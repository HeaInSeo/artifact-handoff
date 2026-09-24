package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection serialises writes. WAL mode lets external readers
	// (e.g. sqlite3 CLI) proceed concurrently without blocking the writer.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := sqliteApplyPragmas(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply pragmas: %w", err)
	}
	if err := sqliteMigrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func sqliteApplyPragmas(db *sql.DB) error {
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=OFF",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// sqliteSchemaVersion is the store schema this binary writes. F4 Mode B (RunID
// identity) is version 2. A database stamped with a higher version was written by a
// newer binary whose identity rules this one does not know; opening it would risk
// silently mis-keying rows, so the store refuses (fail closed).
const sqliteSchemaVersion = 2

func sqliteMigrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS artifacts (
			key                 TEXT PRIMARY KEY,
			run_id              TEXT NOT NULL DEFAULT '',
			sample_run_id       TEXT NOT NULL,
			producer_node_id    TEXT NOT NULL,
			producer_attempt_id TEXT NOT NULL,
			output_name         TEXT NOT NULL,
			artifact_id         TEXT NOT NULL DEFAULT '',
			digest              TEXT NOT NULL DEFAULT '',
			logical_uri         TEXT NOT NULL DEFAULT '',
			node_name           TEXT NOT NULL DEFAULT '',
			uri                 TEXT NOT NULL DEFAULT '',
			locations_json      TEXT NOT NULL DEFAULT '[]',
			size_bytes          INTEGER NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS artifact_sources (
			source_id             TEXT PRIMARY KEY,
			artifact_id           TEXT NOT NULL,
			backend_id            TEXT NOT NULL,
			digest                TEXT NOT NULL DEFAULT '',
			state                 TEXT NOT NULL,
			location_fingerprint  TEXT NOT NULL DEFAULT '',
			location_json         TEXT NOT NULL,
			created_at            TEXT NOT NULL,
			updated_at            TEXT NOT NULL,
			last_verified_at      TEXT NOT NULL DEFAULT '',
			last_error            TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS node_terminals (
			key            TEXT PRIMARY KEY,
			run_id         TEXT NOT NULL DEFAULT '',
			sample_run_id  TEXT NOT NULL,
			node_id        TEXT NOT NULL,
			attempt_id     TEXT NOT NULL,
			terminal_state TEXT NOT NULL,
			recorded_at    TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sample_run_lifecycles (
			sample_run_id           TEXT PRIMARY KEY,
			finalized               INTEGER NOT NULL DEFAULT 0,
			finalized_at            TEXT,
			retention_policy_source TEXT NOT NULL DEFAULT '',
			retention_duration_ns   INTEGER NOT NULL DEFAULT 0,
			retention_until         TEXT,
			gc_eligible             INTEGER NOT NULL DEFAULT 0,
			gc_eligible_at          TEXT,
			gc_blocked_reason       TEXT NOT NULL DEFAULT '',
			terminal_node_count     INTEGER NOT NULL DEFAULT 0,
			succeeded_node_count    INTEGER NOT NULL DEFAULT 0,
			failed_node_count       INTEGER NOT NULL DEFAULT 0,
			canceled_node_count     INTEGER NOT NULL DEFAULT 0,
			retained_artifact_count INTEGER NOT NULL DEFAULT 0,
			retained_artifact_bytes INTEGER NOT NULL DEFAULT 0
		)`,
		// F4 Mode B: the Run-keyed lifecycle. The pre-F4 sample_run_lifecycles table
		// is left as-is and never read (legacy-unresolved, see migrateRunIdentity).
		`CREATE TABLE IF NOT EXISTS run_lifecycles (
			run_id                  TEXT PRIMARY KEY,
			sample_run_id           TEXT NOT NULL DEFAULT '',
			finalized               INTEGER NOT NULL DEFAULT 0,
			finalized_at            TEXT,
			retention_policy_source TEXT NOT NULL DEFAULT '',
			retention_duration_ns   INTEGER NOT NULL DEFAULT 0,
			retention_until         TEXT,
			gc_eligible             INTEGER NOT NULL DEFAULT 0,
			gc_eligible_at          TEXT,
			gc_blocked_reason       TEXT NOT NULL DEFAULT '',
			terminal_node_count     INTEGER NOT NULL DEFAULT 0,
			succeeded_node_count    INTEGER NOT NULL DEFAULT 0,
			failed_node_count       INTEGER NOT NULL DEFAULT 0,
			canceled_node_count     INTEGER NOT NULL DEFAULT 0,
			retained_artifact_count INTEGER NOT NULL DEFAULT 0,
			retained_artifact_bytes INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS ah_schema_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_artifact_id ON artifacts(artifact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_sample_run_id ON artifacts(sample_run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artifact_sources_artifact_id ON artifact_sources(artifact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_node_terminals_sample_run_id ON node_terminals(sample_run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_run_lifecycles_sample_run_id ON run_lifecycles(sample_run_id)`,
	}
	for _, stmt := range ddl {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := migrateRunIdentity(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	// ALTER TABLE runs outside the transaction because a duplicate-column error
	// is expected on already-migrated databases and must be handled per-statement.
	for _, stmt := range []string{
		`ALTER TABLE artifacts ADD COLUMN logical_uri TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE artifacts ADD COLUMN locations_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE artifact_sources ADD COLUMN last_verified_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE artifact_sources ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil && !isDuplicateColumn(err) {
			return err
		}
	}
	return nil
}

// migrateRunIdentity applies the F4 Mode B schema step inside the caller's
// migration transaction, so a crash leaves either the pre-F4 schema or the complete
// version-2 schema with its legacy disposition recorded — never a half-migrated store:
//
//   - refuse a database stamped with a newer schema version;
//   - add run_id to artifacts / node_terminals (additive; existing rows get an empty run_id);
//   - index run_id;
//   - record the legacy disposition once: every pre-F4 row (empty run_id) is
//     legacy-unresolved. It is NOT backfilled from sample_run_id (a SampleRunID is
//     not proof of which Run produced the row), NOT deleted, never returned by a
//     Run-keyed lookup and never GC-evaluated.
func migrateRunIdentity(tx *sql.Tx) error {
	var current string
	err := tx.QueryRow(`SELECT value FROM ah_schema_meta WHERE key = 'schema_version'`).Scan(&current)
	switch {
	case err == sql.ErrNoRows:
		current = ""
	case err != nil:
		return fmt.Errorf("read schema version: %w", err)
	}
	if current != "" {
		var v int
		if _, perr := fmt.Sscanf(current, "%d", &v); perr != nil {
			return fmt.Errorf("unreadable schema version %q; refusing to open", current)
		}
		if v > sqliteSchemaVersion {
			return fmt.Errorf("store schema version %d is newer than this binary's %d; refusing to open (downgrade)", v, sqliteSchemaVersion)
		}
	}
	for _, table := range []string{"artifacts", "node_terminals"} {
		if err := addColumnIfMissingTx(tx, table, "run_id", `TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_artifacts_run_id ON artifacts(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_node_terminals_run_id ON node_terminals(run_id)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if current != "" {
		return nil // disposition already recorded by the upgrade that stamped the version
	}
	counts := map[string]string{
		"legacy_unresolved_artifacts":             `SELECT COUNT(*) FROM artifacts WHERE run_id = ''`,
		"legacy_unresolved_node_terminals":        `SELECT COUNT(*) FROM node_terminals WHERE run_id = ''`,
		"legacy_unresolved_sample_run_lifecycles": `SELECT COUNT(*) FROM sample_run_lifecycles`,
	}
	for key, query := range counts {
		var n int64
		if err := tx.QueryRow(query).Scan(&n); err != nil {
			return fmt.Errorf("count %s: %w", key, err)
		}
		if _, err := tx.Exec(`INSERT INTO ah_schema_meta (key, value) VALUES (?, ?)`, key, fmt.Sprintf("%d", n)); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO ah_schema_meta (key, value) VALUES ('schema_version', ?)`, fmt.Sprintf("%d", sqliteSchemaVersion))
	return err
}

func addColumnIfMissingTx(tx *sql.Tx, table, column, decl string) error {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	found := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	// table/column/decl are package constants from migrateRunIdentity, never input.
	_, err = tx.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl)) //nolint:gosec // constant identifiers
	return err
}

// LegacyDisposition reports the F4 Mode B migration record: the schema version and
// how many pre-F4 rows were left legacy-unresolved (key → count).
func (s *SQLiteStore) LegacyDisposition(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM ah_schema_meta`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate column name:")
}

func (s *SQLiteStore) PutArtifact(ctx context.Context, a domain.Artifact) error {
	if strings.TrimSpace(a.RunID) == "" {
		return errRunIDRequired("put artifact")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existingDigest string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM artifacts WHERE key = ?`, a.Key()).Scan(&existingDigest)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && existingDigest != "" {
		if a.Digest == "" {
			return fmt.Errorf("artifact %s already has digest %s; refusing to clear", a.Key(), existingDigest)
		}
		if existingDigest != a.Digest {
			return fmt.Errorf("artifact %s: digest conflict: existing %s, new %s", a.Key(), existingDigest, a.Digest)
		}
		return tx.Commit() // same digest, idempotent
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO artifacts (key, run_id, sample_run_id, producer_node_id, producer_attempt_id,
			output_name, artifact_id, digest, logical_uri, node_name, uri, locations_json, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			artifact_id = excluded.artifact_id,
			digest      = excluded.digest,
			logical_uri = excluded.logical_uri,
			node_name   = excluded.node_name,
			uri         = excluded.uri,
			locations_json = excluded.locations_json,
			size_bytes  = excluded.size_bytes,
			created_at  = excluded.created_at`,
		a.Key(),
		a.RunID, a.SampleRunID, a.ProducerNodeID, a.ProducerAttemptID, a.OutputName,
		a.ArtifactID, a.Digest, a.LogicalURI, a.NodeName, a.URI, marshalLocations(a.Locations), a.SizeBytes,
		timeToStr(a.CreatedAt),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// artifactColumns is the artifact projection every read uses. Every read also
// requires run_id <> ” so a legacy-unresolved (pre-F4) row is never returned.
const artifactColumns = `run_id, sample_run_id, producer_node_id, producer_attempt_id, output_name,
	       artifact_id, digest, logical_uri, node_name, uri, locations_json, size_bytes, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row rowScanner) (domain.Artifact, error) {
	var a domain.Artifact
	var createdAt string
	var locationsJSON string
	if err := row.Scan(&a.RunID, &a.SampleRunID, &a.ProducerNodeID, &a.ProducerAttemptID, &a.OutputName,
		&a.ArtifactID, &a.Digest, &a.LogicalURI, &a.NodeName, &a.URI, &locationsJSON, &a.SizeBytes, &createdAt); err != nil {
		return domain.Artifact{}, err
	}
	if err := json.Unmarshal([]byte(locationsJSON), &a.Locations); err != nil {
		return domain.Artifact{}, fmt.Errorf("unmarshal locations_json: %w", err)
	}
	a.CreatedAt, _ = parseTimeStr(createdAt)
	return a, nil
}

func (s *SQLiteStore) getArtifactWhere(ctx context.Context, where string, arg string) (domain.Artifact, bool, error) {
	// where is one of this file's constant predicates; the value is bound.
	a, err := scanArtifact(s.db.QueryRowContext(ctx,
		`SELECT `+artifactColumns+` FROM artifacts WHERE `+where+` AND run_id <> ''`, arg)) //nolint:gosec // constant predicate
	if err == sql.ErrNoRows {
		return domain.Artifact{}, false, nil
	}
	if err != nil {
		return domain.Artifact{}, false, err
	}
	return a, true, nil
}

func (s *SQLiteStore) listArtifactsWhere(ctx context.Context, where string, arg string) ([]domain.Artifact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+artifactColumns+` FROM artifacts WHERE `+where+` AND run_id <> ''`, arg) //nolint:gosec // constant predicate
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetArtifact(ctx context.Context, runID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error) {
	return s.getArtifactWhere(ctx, `key = ?`, ids.ArtifactKey{
		RunID:             runID,
		ProducerNodeID:    producerNodeID,
		ProducerAttemptID: attemptID,
		OutputName:        outputName,
	}.String())
}

func (s *SQLiteStore) GetArtifactByID(ctx context.Context, artifactID string) (domain.Artifact, bool, error) {
	return s.getArtifactWhere(ctx, `artifact_id = ?`, artifactID)
}

func (s *SQLiteStore) ListArtifactsByRun(ctx context.Context, runID string) ([]domain.Artifact, error) {
	return s.listArtifactsWhere(ctx, `run_id = ?`, runID)
}

func (s *SQLiteStore) ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	return s.listArtifactsWhere(ctx, `sample_run_id = ?`, sampleRunID)
}

func marshalLocations(locations []domain.Location) string {
	if len(locations) == 0 {
		return "[]"
	}
	data, err := json.Marshal(locations)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func marshalLocation(location domain.Location) string {
	data, err := json.Marshal(location)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *SQLiteStore) PutArtifactSources(ctx context.Context, artifactID string, sources []domain.ArtifactSource) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, source := range sources {
		if source.ArtifactID == "" {
			source.ArtifactID = artifactID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO artifact_sources (
				source_id, artifact_id, backend_id, digest, state,
				location_fingerprint, location_json, created_at, updated_at, last_verified_at, last_error
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_id) DO UPDATE SET
				state = excluded.state,
				location_json = excluded.location_json,
				updated_at = excluded.updated_at,
				last_verified_at = excluded.last_verified_at,
				last_error = excluded.last_error`,
			source.SourceID,
			source.ArtifactID,
			source.BackendID,
			source.Digest,
			string(source.State),
			source.LocationFingerprint,
			marshalLocation(source.Location),
			timeToStr(source.CreatedAt),
			timeToStr(source.UpdatedAt),
			timeToStr(source.LastVerifiedAt),
			source.LastError,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListArtifactSources(ctx context.Context, artifactID string) ([]domain.ArtifactSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, artifact_id, backend_id, digest, state,
		       location_fingerprint, location_json, created_at, updated_at, last_verified_at, last_error
		FROM artifact_sources
		WHERE artifact_id = ?
		ORDER BY source_id ASC`, artifactID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ArtifactSource
	for rows.Next() {
		var source domain.ArtifactSource
		var state string
		var locationJSON string
		var createdAt, updatedAt, lastVerifiedAt string
		if err := rows.Scan(
			&source.SourceID, &source.ArtifactID, &source.BackendID, &source.Digest,
			&state, &source.LocationFingerprint, &locationJSON, &createdAt, &updatedAt, &lastVerifiedAt, &source.LastError,
		); err != nil {
			return nil, err
		}
		source.State = domain.SourceState(state)
		source.CreatedAt, _ = parseTimeStr(createdAt)
		source.UpdatedAt, _ = parseTimeStr(updatedAt)
		source.LastVerifiedAt, _ = parseTimeStr(lastVerifiedAt)
		if err := json.Unmarshal([]byte(locationJSON), &source.Location); err != nil {
			return nil, fmt.Errorf("unmarshal location_json: %w", err)
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetArtifactSource(ctx context.Context, sourceID string) (domain.ArtifactSource, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT source_id, artifact_id, backend_id, digest, state,
		       location_fingerprint, location_json, created_at, updated_at, last_verified_at, last_error
		FROM artifact_sources
		WHERE source_id = ?`, sourceID)
	var source domain.ArtifactSource
	var state string
	var locationJSON string
	var createdAt, updatedAt, lastVerifiedAt string
	err := row.Scan(
		&source.SourceID, &source.ArtifactID, &source.BackendID, &source.Digest,
		&state, &source.LocationFingerprint, &locationJSON, &createdAt, &updatedAt, &lastVerifiedAt, &source.LastError,
	)
	if err == sql.ErrNoRows {
		return domain.ArtifactSource{}, false, nil
	}
	if err != nil {
		return domain.ArtifactSource{}, false, err
	}
	source.State = domain.SourceState(state)
	source.CreatedAt, _ = parseTimeStr(createdAt)
	source.UpdatedAt, _ = parseTimeStr(updatedAt)
	source.LastVerifiedAt, _ = parseTimeStr(lastVerifiedAt)
	if err := json.Unmarshal([]byte(locationJSON), &source.Location); err != nil {
		return domain.ArtifactSource{}, false, fmt.Errorf("unmarshal location_json: %w", err)
	}
	return source, true, nil
}

func (s *SQLiteStore) RecordNodeTerminal(ctx context.Context, r domain.NodeTerminalRecord) error {
	if strings.TrimSpace(r.RunID) == "" {
		return errRunIDRequired("record node terminal")
	}
	key := ids.NodeAttemptKey{
		RunID:     r.RunID,
		NodeID:    r.NodeID,
		AttemptID: r.AttemptID,
	}.String()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existingState string
	err = tx.QueryRowContext(ctx, `SELECT terminal_state FROM node_terminals WHERE key = ?`, key).Scan(&existingState)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		if existingState == r.TerminalState {
			return tx.Commit() // same state, idempotent
		}
		return fmt.Errorf("node %s/%s attempt %s: terminal state conflict: already %s, rejecting %s",
			r.RunID, r.NodeID, r.AttemptID, existingState, r.TerminalState)
	}

	// sample_run_id is a legacy NOT NULL column; Run-keyed terminals do not carry
	// Sample metadata, so it is written empty.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO node_terminals (key, run_id, sample_run_id, node_id, attempt_id, terminal_state, recorded_at)
		VALUES (?, ?, '', ?, ?, ?, ?)`,
		key,
		r.RunID, r.NodeID, r.AttemptID, r.TerminalState,
		timeToStr(r.RecordedAt),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetNodeTerminal(ctx context.Context, runID, nodeID, attemptID string) (domain.NodeTerminalRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, node_id, attempt_id, terminal_state, recorded_at
		FROM node_terminals WHERE key = ? AND run_id <> ''`,
		ids.NodeAttemptKey{
			RunID:     runID,
			NodeID:    nodeID,
			AttemptID: attemptID,
		}.String(),
	)
	var r domain.NodeTerminalRecord
	var recordedAt string
	err := row.Scan(&r.RunID, &r.NodeID, &r.AttemptID, &r.TerminalState, &recordedAt)
	if err == sql.ErrNoRows {
		return domain.NodeTerminalRecord{}, false, nil
	}
	if err != nil {
		return domain.NodeTerminalRecord{}, false, err
	}
	r.RecordedAt, _ = parseTimeStr(recordedAt)
	return r, true, nil
}

func (s *SQLiteStore) ListNodeTerminalsByRun(ctx context.Context, runID string) ([]domain.NodeTerminalRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT run_id, node_id, attempt_id, terminal_state, recorded_at
		FROM node_terminals WHERE run_id = ? AND run_id <> ''`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.NodeTerminalRecord
	for rows.Next() {
		var r domain.NodeTerminalRecord
		var recordedAt string
		if err := rows.Scan(&r.RunID, &r.NodeID, &r.AttemptID, &r.TerminalState, &recordedAt); err != nil {
			return nil, err
		}
		r.RecordedAt, _ = parseTimeStr(recordedAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpsertRunLifecycle(ctx context.Context, lc domain.RunLifecycle) error {
	if strings.TrimSpace(lc.RunID) == "" {
		return errRunIDRequired("upsert run lifecycle")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO run_lifecycles (
			run_id, sample_run_id, finalized, finalized_at,
			retention_policy_source, retention_duration_ns, retention_until,
			gc_eligible, gc_eligible_at, gc_blocked_reason,
			terminal_node_count, succeeded_node_count, failed_node_count,
			canceled_node_count, retained_artifact_count, retained_artifact_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET
			sample_run_id           = excluded.sample_run_id,
			finalized               = excluded.finalized,
			finalized_at            = excluded.finalized_at,
			retention_policy_source = excluded.retention_policy_source,
			retention_duration_ns   = excluded.retention_duration_ns,
			retention_until         = excluded.retention_until,
			gc_eligible             = excluded.gc_eligible,
			gc_eligible_at          = excluded.gc_eligible_at,
			gc_blocked_reason       = excluded.gc_blocked_reason,
			terminal_node_count     = excluded.terminal_node_count,
			succeeded_node_count    = excluded.succeeded_node_count,
			failed_node_count       = excluded.failed_node_count,
			canceled_node_count     = excluded.canceled_node_count,
			retained_artifact_count = excluded.retained_artifact_count,
			retained_artifact_bytes = excluded.retained_artifact_bytes`,
		lc.RunID, lc.SampleRunID,
		boolToInt(lc.Finalized), nullTimeToStr(lc.FinalizedAt),
		lc.RetentionPolicySource, int64(lc.RetentionDuration), nullTimeToStr(lc.RetentionUntil),
		boolToInt(lc.GCEligible), nullTimeToStr(lc.GCEligibleAt), lc.GCBlockedReason,
		lc.TerminalNodeCount, lc.SucceededNodeCount, lc.FailedNodeCount,
		lc.CanceledNodeCount, lc.RetainedArtifactCount, lc.RetainedArtifactBytes,
	)
	return err
}

const runLifecycleColumns = `run_id, sample_run_id, finalized, finalized_at,
	       retention_policy_source, retention_duration_ns, retention_until,
	       gc_eligible, gc_eligible_at, gc_blocked_reason,
	       terminal_node_count, succeeded_node_count, failed_node_count,
	       canceled_node_count, retained_artifact_count, retained_artifact_bytes`

func scanRunLifecycle(row rowScanner) (domain.RunLifecycle, error) {
	var lc domain.RunLifecycle
	var finalized, gcEligible int
	var finalizedAt, retentionUntil, gcEligibleAt sql.NullString
	var retentionDurationNs int64
	if err := row.Scan(
		&lc.RunID, &lc.SampleRunID,
		&finalized, &finalizedAt,
		&lc.RetentionPolicySource, &retentionDurationNs, &retentionUntil,
		&gcEligible, &gcEligibleAt, &lc.GCBlockedReason,
		&lc.TerminalNodeCount, &lc.SucceededNodeCount, &lc.FailedNodeCount,
		&lc.CanceledNodeCount, &lc.RetainedArtifactCount, &lc.RetainedArtifactBytes,
	); err != nil {
		return domain.RunLifecycle{}, err
	}
	lc.Finalized = finalized != 0
	lc.GCEligible = gcEligible != 0
	lc.RetentionDuration = time.Duration(retentionDurationNs)
	lc.FinalizedAt = nullStrToTime(finalizedAt)
	lc.RetentionUntil = nullStrToTime(retentionUntil)
	lc.GCEligibleAt = nullStrToTime(gcEligibleAt)
	return lc, nil
}

func (s *SQLiteStore) GetRunLifecycle(ctx context.Context, runID string) (domain.RunLifecycle, bool, error) {
	lc, err := scanRunLifecycle(s.db.QueryRowContext(ctx,
		`SELECT `+runLifecycleColumns+` FROM run_lifecycles WHERE run_id = ?`, runID))
	if err == sql.ErrNoRows {
		return domain.RunLifecycle{}, false, nil
	}
	if err != nil {
		return domain.RunLifecycle{}, false, err
	}
	return lc, true, nil
}

func (s *SQLiteStore) ListRunLifecyclesBySample(ctx context.Context, sampleRunID string) ([]domain.RunLifecycle, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runLifecycleColumns+` FROM run_lifecycles WHERE sample_run_id = ? ORDER BY run_id ASC`, sampleRunID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.RunLifecycle
	for rows.Next() {
		lc, err := scanRunLifecycle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}

func timeToStr(t time.Time) string             { return t.UTC().Format(time.RFC3339Nano) }
func parseTimeStr(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullTimeToStr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{Valid: true, String: timeToStr(*t)}
}

func nullStrToTime(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t, err := parseTimeStr(s.String)
	if err != nil {
		return nil
	}
	return &t
}
