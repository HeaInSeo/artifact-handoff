package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	// SQLite supports only one concurrent writer; serialise via single connection.
	db.SetMaxOpenConns(1)
	if err := sqliteMigrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func sqliteMigrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS artifacts (
			key                 TEXT PRIMARY KEY,
			sample_run_id       TEXT NOT NULL,
			producer_node_id    TEXT NOT NULL,
			producer_attempt_id TEXT NOT NULL,
			output_name         TEXT NOT NULL,
			artifact_id         TEXT NOT NULL DEFAULT '',
			digest              TEXT NOT NULL DEFAULT '',
			node_name           TEXT NOT NULL DEFAULT '',
			uri                 TEXT NOT NULL DEFAULT '',
			size_bytes          INTEGER NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS node_terminals (
			key            TEXT PRIMARY KEY,
			sample_run_id  TEXT NOT NULL,
			node_id        TEXT NOT NULL,
			attempt_id     TEXT NOT NULL,
			terminal_state TEXT NOT NULL,
			recorded_at    TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sample_run_lifecycles (
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
		);
	`)
	return err
}

func (s *SQLiteStore) PutArtifact(ctx context.Context, a domain.Artifact) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO artifacts (key, sample_run_id, producer_node_id, producer_attempt_id,
			output_name, artifact_id, digest, node_name, uri, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			artifact_id = excluded.artifact_id,
			digest      = excluded.digest,
			node_name   = excluded.node_name,
			uri         = excluded.uri,
			size_bytes  = excluded.size_bytes,
			created_at  = excluded.created_at`,
		a.Key(),
		a.SampleRunID, a.ProducerNodeID, a.ProducerAttemptID, a.OutputName,
		a.ArtifactID, a.Digest, a.NodeName, a.URI, a.SizeBytes,
		timeToStr(a.CreatedAt),
	)
	return err
}

func (s *SQLiteStore) GetArtifact(ctx context.Context, sampleRunID, producerNodeID, attemptID, outputName string) (domain.Artifact, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT sample_run_id, producer_node_id, producer_attempt_id, output_name,
		       artifact_id, digest, node_name, uri, size_bytes, created_at
		FROM artifacts WHERE key = ?`,
		sampleRunID+"/"+producerNodeID+"/"+attemptID+"/"+outputName,
	)
	var a domain.Artifact
	var createdAt string
	err := row.Scan(&a.SampleRunID, &a.ProducerNodeID, &a.ProducerAttemptID, &a.OutputName,
		&a.ArtifactID, &a.Digest, &a.NodeName, &a.URI, &a.SizeBytes, &createdAt)
	if err == sql.ErrNoRows {
		return domain.Artifact{}, false, nil
	}
	if err != nil {
		return domain.Artifact{}, false, err
	}
	a.CreatedAt, _ = parseTimeStr(createdAt)
	return a, true, nil
}

func (s *SQLiteStore) ListArtifactsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sample_run_id, producer_node_id, producer_attempt_id, output_name,
		       artifact_id, digest, node_name, uri, size_bytes, created_at
		FROM artifacts WHERE sample_run_id = ?`, sampleRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Artifact
	for rows.Next() {
		var a domain.Artifact
		var createdAt string
		if err := rows.Scan(&a.SampleRunID, &a.ProducerNodeID, &a.ProducerAttemptID, &a.OutputName,
			&a.ArtifactID, &a.Digest, &a.NodeName, &a.URI, &a.SizeBytes, &createdAt); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = parseTimeStr(createdAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) RecordNodeTerminal(ctx context.Context, r domain.NodeTerminalRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_terminals (key, sample_run_id, node_id, attempt_id, terminal_state, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			terminal_state = excluded.terminal_state,
			recorded_at    = excluded.recorded_at`,
		r.SampleRunID+"/"+r.NodeID+"/"+r.AttemptID,
		r.SampleRunID, r.NodeID, r.AttemptID, r.TerminalState,
		timeToStr(r.RecordedAt),
	)
	return err
}

func (s *SQLiteStore) GetNodeTerminal(ctx context.Context, sampleRunID, nodeID, attemptID string) (domain.NodeTerminalRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT sample_run_id, node_id, attempt_id, terminal_state, recorded_at
		FROM node_terminals WHERE key = ?`,
		sampleRunID+"/"+nodeID+"/"+attemptID,
	)
	var r domain.NodeTerminalRecord
	var recordedAt string
	err := row.Scan(&r.SampleRunID, &r.NodeID, &r.AttemptID, &r.TerminalState, &recordedAt)
	if err == sql.ErrNoRows {
		return domain.NodeTerminalRecord{}, false, nil
	}
	if err != nil {
		return domain.NodeTerminalRecord{}, false, err
	}
	r.RecordedAt, _ = parseTimeStr(recordedAt)
	return r, true, nil
}

func (s *SQLiteStore) ListNodeTerminalsBySampleRun(ctx context.Context, sampleRunID string) ([]domain.NodeTerminalRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sample_run_id, node_id, attempt_id, terminal_state, recorded_at
		FROM node_terminals WHERE sample_run_id = ?`, sampleRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NodeTerminalRecord
	for rows.Next() {
		var r domain.NodeTerminalRecord
		var recordedAt string
		if err := rows.Scan(&r.SampleRunID, &r.NodeID, &r.AttemptID, &r.TerminalState, &recordedAt); err != nil {
			return nil, err
		}
		r.RecordedAt, _ = parseTimeStr(recordedAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpsertSampleRunLifecycle(ctx context.Context, lc domain.SampleRunLifecycle) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sample_run_lifecycles (
			sample_run_id, finalized, finalized_at,
			retention_policy_source, retention_duration_ns, retention_until,
			gc_eligible, gc_eligible_at, gc_blocked_reason,
			terminal_node_count, succeeded_node_count, failed_node_count,
			canceled_node_count, retained_artifact_count, retained_artifact_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(sample_run_id) DO UPDATE SET
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
		lc.SampleRunID,
		boolToInt(lc.Finalized), nullTimeToStr(lc.FinalizedAt),
		lc.RetentionPolicySource, int64(lc.RetentionDuration), nullTimeToStr(lc.RetentionUntil),
		boolToInt(lc.GCEligible), nullTimeToStr(lc.GCEligibleAt), lc.GCBlockedReason,
		lc.TerminalNodeCount, lc.SucceededNodeCount, lc.FailedNodeCount,
		lc.CanceledNodeCount, lc.RetainedArtifactCount, lc.RetainedArtifactBytes,
	)
	return err
}

func (s *SQLiteStore) GetSampleRunLifecycle(ctx context.Context, sampleRunID string) (domain.SampleRunLifecycle, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT sample_run_id, finalized, finalized_at,
		       retention_policy_source, retention_duration_ns, retention_until,
		       gc_eligible, gc_eligible_at, gc_blocked_reason,
		       terminal_node_count, succeeded_node_count, failed_node_count,
		       canceled_node_count, retained_artifact_count, retained_artifact_bytes
		FROM sample_run_lifecycles WHERE sample_run_id = ?`, sampleRunID)

	var lc domain.SampleRunLifecycle
	var finalized, gcEligible int
	var finalizedAt, retentionUntil, gcEligibleAt sql.NullString
	var retentionDurationNs int64

	err := row.Scan(
		&lc.SampleRunID,
		&finalized, &finalizedAt,
		&lc.RetentionPolicySource, &retentionDurationNs, &retentionUntil,
		&gcEligible, &gcEligibleAt, &lc.GCBlockedReason,
		&lc.TerminalNodeCount, &lc.SucceededNodeCount, &lc.FailedNodeCount,
		&lc.CanceledNodeCount, &lc.RetainedArtifactCount, &lc.RetainedArtifactBytes,
	)
	if err == sql.ErrNoRows {
		return domain.SampleRunLifecycle{}, false, nil
	}
	if err != nil {
		return domain.SampleRunLifecycle{}, false, err
	}
	lc.Finalized = finalized != 0
	lc.GCEligible = gcEligible != 0
	lc.RetentionDuration = time.Duration(retentionDurationNs)
	lc.FinalizedAt = nullStrToTime(finalizedAt)
	lc.RetentionUntil = nullStrToTime(retentionUntil)
	lc.GCEligibleAt = nullStrToTime(gcEligibleAt)
	return lc, true, nil
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
