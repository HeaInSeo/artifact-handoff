package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
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
}

// NewPostgresStore opens the store at dsn and applies its schema. An empty DSN is an error.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres store: DSN is required for the j2-postgres profile")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres store unavailable: %w", err)
	}
	s := &PostgresStore{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate postgres store: %w", err)
	}
	return s, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

func (s *PostgresStore) migrate(ctx context.Context) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		// Serialize concurrent migrations of independent processes.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7238194021)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ah_schema_meta (
			key text PRIMARY KEY, value text NOT NULL)`); err != nil {
			return err
		}
		var current string
		err := tx.QueryRowContext(ctx, `SELECT value FROM ah_schema_meta WHERE key = 'schema_version'`).Scan(&current)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("read schema version: %w", err)
		default:
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
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ah_schema_meta (key, value) VALUES ('schema_version', $1)
			ON CONFLICT (key) DO NOTHING`, fmt.Sprintf("%d", postgresSchemaVersion))
		return err
	})
}

// inTx runs fn in one SERIALIZABLE transaction. On a serialization failure or deadlock the
// whole transaction is retried with the same frozen input, bounded by maxSerializationRetries
// and ctx. Any other error, including a failed COMMIT, is returned as is: an uncertain commit
// is never turned into success.
func (s *PostgresStore) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
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

func (s *PostgresStore) tryTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
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
