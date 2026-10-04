package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib" // also registers the "pgx" database/sql driver
)

// ProfileJ2Postgres is the explicit store profile of the PostgreSQL J2 transactional adapter
// (SP09-TX1 r1 A1). It is selected only by OpenStoreProfile, never inferred from a DSN, and it
// never falls back to the memory or SQLite store.
const ProfileJ2Postgres = "j2-postgres"

// ErrIdentityUnsupported is returned by the PostgreSQL store when a value cannot be stored as
// exactly the same bytes it was given (for example a string that JSON encoding would rewrite).
// The write is refused; nothing is normalized or narrowed (TX1 C-2).
var ErrIdentityUnsupported = errors.New("value cannot be stored byte-exactly")

// ErrLifecycleUnsupported is returned by PostgresStore.UpsertRunLifecycle. The versionless,
// last-writer-wins lifecycle upsert gives no fence evidence, so the J2 store refuses it
// (TX1-05). Lifecycle writes go through CompareAndSetRunLifecycle.
var ErrLifecycleUnsupported = errors.New("versionless run lifecycle upsert is unsupported by the J2 store")

// ErrLifecycleVersionConflict is returned by CompareAndSetRunLifecycle when the stored version
// is not the expected one. Nothing is written.
var ErrLifecycleVersionConflict = errors.New("run lifecycle version conflict")

// ErrCommitOutcomeUnknown is returned when COMMIT returned but the server warned while it was
// in flight, e.g. a cancelled synchronous-replication wait. The transaction may be committed
// locally without the required standby acknowledgement; callers must not treat it as success
// and must re-read before retrying (TX1 C-1). It is not retried automatically.
var ErrCommitOutcomeUnknown = errors.New("postgres commit outcome unknown")

// ErrRestoreActivationHold is returned by every PostgresStore mutation when the store's
// activation does not belong to the database it runs on (restore, promoted standby) or is not
// active. Nothing is written; reads keep working (TX1-04/TX1-07 (8)).
var ErrRestoreActivationHold = errors.New("store activation hold: restore/failover activation evidence required")

// postgresSchemaVersion is the schema this binary writes. A database stamped with a newer
// version is refused before any mutation.
const postgresSchemaVersion = 1

// maxSerializationRetries bounds the whole-transaction retries on serialization failure or
// deadlock; the context deadline bounds them too.
const maxSerializationRetries = 16

// PostgresStore is the J2 store: every authoritative mutation is one SERIALIZABLE transaction
// on one PostgreSQL primary, with unique constraints as the last line of defence. Every
// caller-supplied string is stored as bytea, so identities keep their exact bytes (NUL and
// invalid UTF-8 included) and no collation, case folding or trimming applies.
type PostgresStore struct {
	db *sql.DB
	// watches maps a *pgconn.PgConn whose COMMIT is in flight to its *commitWatch.
	watches sync.Map
}

// NewPostgresStore opens the store at dsn and applies its schema. An empty DSN is an error.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres store: DSN is required for the j2-postgres profile")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres DSN: %w", err)
	}
	s := &PostgresStore{}
	cfg.OnNotice = s.onNotice
	db := stdlib.OpenDB(*cfg)
	s.db = db
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres store unavailable: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate postgres store: %w", err)
	}
	return s, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

// pgIdentityQuery reads the physical identity a store activation is bound to: the cluster's
// system identifier, the current WAL timeline (it changes on promotion and point-in-time
// restore) and the database OID (it changes on a logical restore into a new database). It
// fails on a standby, which is not writable anyway.
const pgIdentityQuery = `SELECT
	(SELECT system_identifier::text FROM pg_control_system()),
	('x' || substr(pg_walfile_name(pg_current_wal_lsn()), 1, 8))::bit(32)::bigint,
	(SELECT oid::bigint FROM pg_database WHERE datname = current_database())`

type pgIdentity struct {
	systemID   string
	timeline   int64
	databaseID int64
}

func readPGIdentity(ctx context.Context, tx *sql.Tx) (pgIdentity, error) {
	var id pgIdentity
	if err := tx.QueryRowContext(ctx, pgIdentityQuery).Scan(&id.systemID, &id.timeline, &id.databaseID); err != nil {
		return pgIdentity{}, fmt.Errorf("read store identity: %w", err)
	}
	return id, nil
}

// activate records the store activation on first open. A brand-new store (every authoritative
// table empty) is a clean bootstrap and becomes epoch 1, active, bound to this database's
// identity. A store that already holds data but has no activation record was not created by
// this binary's bootstrap (e.g. a partial restore), so it is recorded as held. An existing
// record is never rewritten here: a restored record keeps its old identity and is refused by
// requireActive. There is no automatic re-activation (TX1-04 RESTORE-ACTIVATION HOLD).
func activate(ctx context.Context, tx *sql.Tx) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM ah_store_activation)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var hasData bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM ah_artifacts)
		OR EXISTS (SELECT 1 FROM ah_artifact_sources) OR EXISTS (SELECT 1 FROM ah_node_terminals)
		OR EXISTS (SELECT 1 FROM ah_run_lifecycles)`).Scan(&hasData); err != nil {
		return err
	}
	state := storeStateActive
	if hasData {
		state = storeStateRestoreHold
	}
	id, err := readPGIdentity(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ah_store_activation
		(singleton, epoch, state, system_identifier, timeline_id, database_oid) VALUES (true, 1, $1, $2, $3, $4)`,
		state, id.systemID, id.timeline, id.databaseID)
	return err
}

const (
	storeStateActive      = "active"
	storeStateRestoreHold = "restore-hold"
)

// requireActive runs inside every mutation transaction. The store is writable only when its
// activation record is active and bound to the database it is running on. A copy of the store
// on another cluster, timeline or database — a restore or a promoted standby — is refused with
// ErrRestoreActivationHold until activation is re-established with high-water and
// old-primary fencing evidence, which this slice deliberately does not implement. Raising the
// epoch alone does not lift the hold.
func requireActive(ctx context.Context, tx *sql.Tx) error {
	var state, systemID string
	var epoch, timeline, databaseID int64
	err := tx.QueryRowContext(ctx, `SELECT epoch, state, system_identifier, timeline_id, database_oid
		FROM ah_store_activation WHERE singleton`).Scan(&epoch, &state, &systemID, &timeline, &databaseID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: no activation record", ErrRestoreActivationHold)
	}
	if err != nil {
		return fmt.Errorf("read store activation: %w", err)
	}
	if state != storeStateActive {
		return fmt.Errorf("%w: epoch %d state %q", ErrRestoreActivationHold, epoch, state)
	}
	cur, err := readPGIdentity(ctx, tx)
	if err != nil {
		return err
	}
	if cur != (pgIdentity{systemID: systemID, timeline: timeline, databaseID: databaseID}) {
		return fmt.Errorf("%w: epoch %d was activated on system %s timeline %d database %d, now on system %s timeline %d database %d",
			ErrRestoreActivationHold, epoch, systemID, timeline, databaseID, cur.systemID, cur.timeline, cur.databaseID)
	}
	return nil
}

// inTx runs a mutation: one SERIALIZABLE transaction that first checks the store activation.
func (s *PostgresStore) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return s.retryTx(ctx, func(tx *sql.Tx) error {
		if err := requireActive(ctx, tx); err != nil {
			return err
		}
		return fn(tx)
	})
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	return s.retryTx(ctx, func(tx *sql.Tx) error {
		// Serialize concurrent migrations of independent processes.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7238194021)`); err != nil {
			return err
		}
		var hasMeta, hasStoreTables bool
		if err := tx.QueryRowContext(ctx, `SELECT to_regclass('ah_schema_meta') IS NOT NULL, (
			to_regclass('ah_artifacts') IS NOT NULL OR to_regclass('ah_artifact_sources') IS NOT NULL OR
			to_regclass('ah_node_terminals') IS NOT NULL OR to_regclass('ah_run_lifecycles') IS NOT NULL OR
			to_regclass('ah_store_activation') IS NOT NULL)`).Scan(&hasMeta, &hasStoreTables); err != nil {
			return fmt.Errorf("inspect schema: %w", err)
		}
		// The stamp is the schema_version row, not the metadata table: this binary creates the
		// tables and the row in one transaction, so a store without the row was not created by it.
		var current string
		stamped := false
		if hasMeta {
			err := tx.QueryRowContext(ctx, `SELECT value FROM ah_schema_meta WHERE key = 'schema_version'`).Scan(&current)
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return fmt.Errorf("read schema version: %w", err)
			default:
				stamped = true
			}
		}
		// Store tables without this store's schema stamp were not created by this binary;
		// adopting them could silently reinterpret foreign rows (TX1-07 (5)).
		if hasStoreTables && !stamped {
			return errors.New("store tables exist without a schema stamp (foreign schema); refusing to open")
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ah_schema_meta (
			key text PRIMARY KEY, value text NOT NULL)`); err != nil {
			return err
		}
		if stamped {
			var v int
			if _, perr := fmt.Sscanf(current, "%d", &v); perr != nil {
				return fmt.Errorf("unreadable schema version %q; refusing to open", current)
			}
			if v > postgresSchemaVersion {
				return fmt.Errorf("store schema version %d is newer than this binary's %d; refusing to open (downgrade)",
					v, postgresSchemaVersion)
			}
		}
		for _, stmt := range []string{
			`CREATE TABLE IF NOT EXISTS ah_artifacts (
				key                 bytea PRIMARY KEY,
				run_id              bytea NOT NULL,
				sample_run_id       bytea NOT NULL,
				producer_node_id    bytea NOT NULL,
				producer_attempt_id bytea NOT NULL,
				output_name         bytea NOT NULL,
				artifact_id         bytea NOT NULL,
				digest              bytea NOT NULL,
				logical_uri         bytea NOT NULL,
				node_name           bytea NOT NULL,
				uri                 bytea NOT NULL,
				locations_json      bytea NOT NULL,
				size_bytes          bigint NOT NULL,
				created_at          text NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS ah_artifacts_run_id ON ah_artifacts(run_id)`,
			`CREATE INDEX IF NOT EXISTS ah_artifacts_sample_run_id ON ah_artifacts(sample_run_id)`,
			`CREATE INDEX IF NOT EXISTS ah_artifacts_artifact_id ON ah_artifacts(artifact_id)`,
			`CREATE TABLE IF NOT EXISTS ah_artifact_sources (
				source_id            bytea PRIMARY KEY,
				artifact_id          bytea NOT NULL,
				backend_id           bytea NOT NULL,
				digest               bytea NOT NULL,
				state                bytea NOT NULL,
				location_fingerprint bytea NOT NULL,
				location_json        bytea NOT NULL,
				created_at           text NOT NULL,
				updated_at           text NOT NULL,
				last_verified_at     text NOT NULL,
				last_error           bytea NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS ah_artifact_sources_artifact_id ON ah_artifact_sources(artifact_id)`,
			`CREATE TABLE IF NOT EXISTS ah_node_terminals (
				key            bytea PRIMARY KEY,
				run_id         bytea NOT NULL,
				node_id        bytea NOT NULL,
				attempt_id     bytea NOT NULL,
				terminal_state bytea NOT NULL,
				recorded_at    text NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS ah_node_terminals_run_id ON ah_node_terminals(run_id)`,
			`CREATE TABLE IF NOT EXISTS ah_run_lifecycles (
				run_id         bytea PRIMARY KEY,
				sample_run_id  bytea NOT NULL,
				version        bigint NOT NULL CHECK (version > 0),
				lifecycle_json bytea NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS ah_run_lifecycles_sample_run_id ON ah_run_lifecycles(sample_run_id)`,
			`CREATE TABLE IF NOT EXISTS ah_store_activation (
				singleton         boolean PRIMARY KEY CHECK (singleton),
				epoch             bigint NOT NULL CHECK (epoch > 0),
				state             text NOT NULL,
				system_identifier text NOT NULL,
				timeline_id       bigint NOT NULL,
				database_oid      bigint NOT NULL)`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ah_schema_meta (key, value) VALUES ('schema_version', $1)
			ON CONFLICT (key) DO NOTHING`, fmt.Sprintf("%d", postgresSchemaVersion)); err != nil {
			return err
		}
		return activate(ctx, tx)
	})
}

// retryTx runs fn in one SERIALIZABLE transaction. On a serialization failure or deadlock the
// whole transaction is retried with the same frozen input, bounded by maxSerializationRetries
// and ctx. Any other error, including a failed COMMIT, is returned as is: an uncertain commit
// is never turned into success.
func (s *PostgresStore) retryTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	var err error
	for range maxSerializationRetries {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		err = s.tryTx(ctx, fn)
		if !isRetryableSerialization(err) {
			return err
		}
	}
	return fmt.Errorf("postgres store: giving up after %d serialization retries: %w", maxSerializationRetries, err)
}

// tryTx runs one attempt on one pinned connection so that a WARNING the server sends while
// COMMIT is in flight can be attributed to this transaction. PostgreSQL answers a cancelled
// synchronous-replication wait with "COMMIT" plus a WARNING ("canceling wait for synchronous
// replication ... committed locally, but might not have been replicated"); that is not a
// durable acknowledgement, so it is reported as ErrCommitOutcomeUnknown (TX1 C-1). Any WARNING
// during COMMIT is treated the same way, which does not depend on the server's lc_messages.
func (s *PostgresStore) tryTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	var pg *pgconn.PgConn
	if err := conn.Raw(func(driverConn any) error {
		c, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("postgres store: unexpected driver connection %T", driverConn)
		}
		pg = c.Conn().PgConn()
		return nil
	}); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	w := s.watch(pg)
	defer s.unwatch(pg)
	if err := tx.Commit(); err != nil {
		return err
	}
	if n := w.warning(); n != nil {
		return fmt.Errorf("%w: server warning during COMMIT: %s (%s)", ErrCommitOutcomeUnknown, n.Message, n.Detail)
	}
	return nil
}

// commitWatch records the first WARNING notice a connection receives while it is watched.
type commitWatch struct {
	mu   sync.Mutex
	seen *pgconn.Notice
}

func (w *commitWatch) note(n *pgconn.Notice) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seen == nil {
		w.seen = n
	}
}

func (w *commitWatch) warning() *pgconn.Notice {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen
}

func (s *PostgresStore) watch(pg *pgconn.PgConn) *commitWatch {
	w := &commitWatch{}
	s.watches.Store(pg, w)
	return w
}

func (s *PostgresStore) unwatch(pg *pgconn.PgConn) { s.watches.Delete(pg) }

// onNotice is installed on every pooled connection. Notices outside a watched COMMIT are
// ignored.
func (s *PostgresStore) onNotice(pg *pgconn.PgConn, n *pgconn.Notice) {
	if !strings.EqualFold(n.SeverityUnlocalized, "WARNING") && !strings.EqualFold(n.Severity, "WARNING") {
		return
	}
	if w, ok := s.watches.Load(pg); ok {
		w.(*commitWatch).note(n)
	}
}

func isRetryableSerialization(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

// exactJSON marshals v and refuses it when decoding the result does not give v back, so JSON
// encoding never silently rewrites a value (e.g. invalid UTF-8 → U+FFFD).
func exactJSON(v any, what string) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", what, err)
	}
	back := reflect.New(reflect.TypeOf(v))
	if err := json.Unmarshal(data, back.Interface()); err != nil {
		return nil, fmt.Errorf("re-read %s: %w", what, err)
	}
	if !reflect.DeepEqual(back.Elem().Interface(), v) {
		return nil, fmt.Errorf("%s: %w", what, ErrIdentityUnsupported)
	}
	return data, nil
}

func (s *PostgresStore) PutArtifact(ctx context.Context, a domain.Artifact) error {
	if strings.TrimSpace(a.RunID) == "" {
		return errRunIDRequired("put artifact")
	}
	locations := a.Locations
	if locations == nil {
		locations = []domain.Location{}
	}
	locationsJSON, err := exactJSON(locations, "artifact locations")
	if err != nil {
		return err
	}
	key := []byte(a.Key())
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var existing []byte
		err := tx.QueryRowContext(ctx, `SELECT digest FROM ah_artifacts WHERE key = $1`, key).Scan(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && len(existing) > 0 {
			if a.Digest == "" {
				return fmt.Errorf("artifact %s already has digest %s; refusing to clear", a.Key(), existing)
			}
			if string(existing) != a.Digest {
				return fmt.Errorf("artifact %s: digest conflict: existing %s, new %s", a.Key(), existing, a.Digest)
			}
			return nil // same digest, idempotent
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ah_artifacts (key, run_id, sample_run_id, producer_node_id, producer_attempt_id,
				output_name, artifact_id, digest, logical_uri, node_name, uri, locations_json, size_bytes, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (key) DO UPDATE SET
				artifact_id    = excluded.artifact_id,
				digest         = excluded.digest,
				logical_uri    = excluded.logical_uri,
				node_name      = excluded.node_name,
				uri            = excluded.uri,
				locations_json = excluded.locations_json,
				size_bytes     = excluded.size_bytes,
				created_at     = excluded.created_at`,
			key, []byte(a.RunID), []byte(a.SampleRunID), []byte(a.ProducerNodeID), []byte(a.ProducerAttemptID),
			[]byte(a.OutputName), []byte(a.ArtifactID), []byte(a.Digest), []byte(a.LogicalURI), []byte(a.NodeName),
			[]byte(a.URI), locationsJSON, a.SizeBytes, timeToStr(a.CreatedAt))
		return err
	})
}

const pgArtifactColumns = `run_id, sample_run_id, producer_node_id, producer_attempt_id, output_name,
	artifact_id, digest, logical_uri, node_name, uri, locations_json, size_bytes, created_at`

func scanPGArtifact(row rowScanner) (domain.Artifact, error) {
	var runID, sampleRunID, nodeID, attemptID, output, artifactID, digest, logicalURI, nodeName, uri, locs []byte
	var a domain.Artifact
	var createdAt string
	if err := row.Scan(&runID, &sampleRunID, &nodeID, &attemptID, &output, &artifactID, &digest,
		&logicalURI, &nodeName, &uri, &locs, &a.SizeBytes, &createdAt); err != nil {
		return domain.Artifact{}, err
	}
	a.RunID, a.SampleRunID, a.ProducerNodeID, a.ProducerAttemptID = string(runID), string(sampleRunID), string(nodeID), string(attemptID)
	a.OutputName, a.ArtifactID, a.Digest = string(output), string(artifactID), string(digest)
	a.LogicalURI, a.NodeName, a.URI = string(logicalURI), string(nodeName), string(uri)
	if err := json.Unmarshal(locs, &a.Locations); err != nil {
		return domain.Artifact{}, fmt.Errorf("unmarshal locations_json: %w", err)
	}
	if len(a.Locations) == 0 {
		a.Locations = nil
	}
	a.CreatedAt, _ = parseTimeStr(createdAt)
	return a, nil
}

func (s *PostgresStore) getArtifactWhere(ctx context.Context, where string, arg []byte) (domain.Artifact, bool, error) {
	a, err := scanPGArtifact(s.db.QueryRowContext(ctx,
		`SELECT `+pgArtifactColumns+` FROM ah_artifacts WHERE `+where+` AND run_id <> ''::bytea`, arg)) //nolint:gosec // constant predicate
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Artifact{}, false, nil
	}
	if err != nil {
		return domain.Artifact{}, false, err
	}
	return a, true, nil
}

func (s *PostgresStore) listArtifactsWhere(ctx context.Context, where string, arg []byte) ([]domain.Artifact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+pgArtifactColumns+` FROM ah_artifacts WHERE `+where+` AND run_id <> ''::bytea ORDER BY key`, arg) //nolint:gosec // constant predicate
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Artifact
	for rows.Next() {
		a, err := scanPGArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetArtifact(ctx context.Context, runID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error) {
	return s.getArtifactWhere(ctx, `key = $1`, []byte(ids.ArtifactKey{
		RunID: runID, ProducerNodeID: producerNodeID, ProducerAttemptID: attemptID, OutputName: outputName,
	}.String()))
}

func (s *PostgresStore) GetArtifactByID(ctx context.Context, artifactID string) (domain.Artifact, bool, error) {
	return s.getArtifactWhere(ctx, `artifact_id = $1`, []byte(artifactID))
}

func (s *PostgresStore) ListArtifactsByRun(ctx context.Context, runID string) ([]domain.Artifact, error) {
	return s.listArtifactsWhere(ctx, `run_id = $1`, []byte(runID))
}

func (s *PostgresStore) ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	return s.listArtifactsWhere(ctx, `sample_run_id = $1`, []byte(sampleRunID))
}
