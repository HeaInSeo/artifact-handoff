package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
)

func (s *PostgresStore) PutArtifactSources(ctx context.Context, artifactID string, sources []domain.ArtifactSource) error {
	type row struct {
		src          domain.ArtifactSource
		locationJSON []byte
	}
	rows := make([]row, 0, len(sources))
	for _, source := range sources {
		if source.ArtifactID == "" {
			source.ArtifactID = artifactID
		}
		locationJSON, err := exactJSON(source.Location, "artifact source location")
		if err != nil {
			return err
		}
		rows = append(rows, row{src: source, locationJSON: locationJSON})
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, r := range rows {
			src := r.src
			// The conflict update applies only to a row owned by the same artifact. A source ID
			// owned by another artifact changes no row, and the whole transaction is rolled back.
			res, err := tx.ExecContext(ctx, `
				INSERT INTO ah_artifact_sources (
					source_id, artifact_id, backend_id, digest, state,
					location_fingerprint, location_json, created_at, updated_at, last_verified_at, last_error
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				ON CONFLICT (source_id) DO UPDATE SET
					state            = excluded.state,
					location_json    = excluded.location_json,
					updated_at       = excluded.updated_at,
					last_verified_at = excluded.last_verified_at,
					last_error       = excluded.last_error
				WHERE ah_artifact_sources.artifact_id = excluded.artifact_id`,
				[]byte(src.SourceID), []byte(src.ArtifactID), []byte(src.BackendID), []byte(src.Digest),
				[]byte(string(src.State)), []byte(src.LocationFingerprint), r.locationJSON,
				timeToStr(src.CreatedAt), timeToStr(src.UpdatedAt), timeToStr(src.LastVerifiedAt), []byte(src.LastError))
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("put artifact source %q for artifact %q: %w", src.SourceID, src.ArtifactID, ErrSourceOwnershipConflict)
			}
		}
		return nil
	})
}

// pgLiveSourceFilter returns a source only while its artifact is a live Run-keyed row.
const pgLiveSourceFilter = `EXISTS (SELECT 1 FROM ah_artifacts a
	WHERE a.artifact_id = ah_artifact_sources.artifact_id AND a.run_id <> ''::bytea)`

const pgSourceColumns = `source_id, artifact_id, backend_id, digest, state,
	location_fingerprint, location_json, created_at, updated_at, last_verified_at, last_error`

func scanPGSource(row rowScanner) (domain.ArtifactSource, error) {
	var sourceID, artifactID, backendID, digest, state, fingerprint, locationJSON, lastError []byte
	var createdAt, updatedAt, lastVerifiedAt string
	if err := row.Scan(&sourceID, &artifactID, &backendID, &digest, &state, &fingerprint, &locationJSON,
		&createdAt, &updatedAt, &lastVerifiedAt, &lastError); err != nil {
		return domain.ArtifactSource{}, err
	}
	source := domain.ArtifactSource{
		SourceID: string(sourceID), ArtifactID: string(artifactID), BackendID: string(backendID),
		Digest: string(digest), State: domain.SourceState(state), LocationFingerprint: string(fingerprint),
		LastError: string(lastError),
	}
	source.CreatedAt, _ = parseTimeStr(createdAt)
	source.UpdatedAt, _ = parseTimeStr(updatedAt)
	source.LastVerifiedAt, _ = parseTimeStr(lastVerifiedAt)
	if err := json.Unmarshal(locationJSON, &source.Location); err != nil {
		return domain.ArtifactSource{}, fmt.Errorf("unmarshal location_json: %w", err)
	}
	return source, nil
}

func (s *PostgresStore) ListArtifactSources(ctx context.Context, artifactID string) ([]domain.ArtifactSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pgSourceColumns+` FROM ah_artifact_sources
		WHERE artifact_id = $1 AND `+pgLiveSourceFilter+` ORDER BY source_id ASC`, []byte(artifactID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ArtifactSource
	for rows.Next() {
		source, err := scanPGSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetArtifactSource(ctx context.Context, sourceID string) (domain.ArtifactSource, bool, error) {
	source, err := scanPGSource(s.db.QueryRowContext(ctx, `SELECT `+pgSourceColumns+` FROM ah_artifact_sources
		WHERE source_id = $1 AND `+pgLiveSourceFilter, []byte(sourceID)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ArtifactSource{}, false, nil
	}
	if err != nil {
		return domain.ArtifactSource{}, false, err
	}
	return source, true, nil
}

func (s *PostgresStore) RecordNodeTerminal(ctx context.Context, r domain.NodeTerminalRecord) error {
	if strings.TrimSpace(r.RunID) == "" {
		return errRunIDRequired("record node terminal")
	}
	key := []byte(ids.NodeAttemptKey{RunID: r.RunID, NodeID: r.NodeID, AttemptID: r.AttemptID}.String())
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var existing []byte
		err := tx.QueryRowContext(ctx, `SELECT terminal_state FROM ah_node_terminals WHERE key = $1`, key).Scan(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if string(existing) == r.TerminalState {
				return nil // same state, idempotent
			}
			return fmt.Errorf("node %s/%s attempt %s: terminal state conflict: already %s, rejecting %s",
				r.RunID, r.NodeID, r.AttemptID, existing, r.TerminalState)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ah_node_terminals (key, run_id, node_id, attempt_id, terminal_state, recorded_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			key, []byte(r.RunID), []byte(r.NodeID), []byte(r.AttemptID), []byte(r.TerminalState), timeToStr(r.RecordedAt))
		return err
	})
}

const pgTerminalColumns = `run_id, node_id, attempt_id, terminal_state, recorded_at`

func scanPGTerminal(row rowScanner) (domain.NodeTerminalRecord, error) {
	var runID, nodeID, attemptID, state []byte
	var recordedAt string
	if err := row.Scan(&runID, &nodeID, &attemptID, &state, &recordedAt); err != nil {
		return domain.NodeTerminalRecord{}, err
	}
	r := domain.NodeTerminalRecord{RunID: string(runID), NodeID: string(nodeID), AttemptID: string(attemptID), TerminalState: string(state)}
	r.RecordedAt, _ = parseTimeStr(recordedAt)
	return r, nil
}

func (s *PostgresStore) GetNodeTerminal(ctx context.Context, runID, nodeID, attemptID string) (domain.NodeTerminalRecord, bool, error) {
	key := []byte(ids.NodeAttemptKey{RunID: runID, NodeID: nodeID, AttemptID: attemptID}.String())
	r, err := scanPGTerminal(s.db.QueryRowContext(ctx,
		`SELECT `+pgTerminalColumns+` FROM ah_node_terminals WHERE key = $1 AND run_id <> ''::bytea`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NodeTerminalRecord{}, false, nil
	}
	if err != nil {
		return domain.NodeTerminalRecord{}, false, err
	}
	return r, true, nil
}

func (s *PostgresStore) ListNodeTerminalsByRun(ctx context.Context, runID string) ([]domain.NodeTerminalRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pgTerminalColumns+` FROM ah_node_terminals
		WHERE run_id = $1 AND run_id <> ''::bytea ORDER BY key`, []byte(runID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.NodeTerminalRecord
	for rows.Next() {
		r, err := scanPGTerminal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertRunLifecycle is unsupported by the J2 store (TX1-05): the versionless upsert cannot
// prove which writer's view it overwrites. Use CompareAndSetRunLifecycle.
func (s *PostgresStore) UpsertRunLifecycle(_ context.Context, lc domain.RunLifecycle) error {
	return fmt.Errorf("upsert run lifecycle %q: %w", lc.RunID, ErrLifecycleUnsupported)
}

// CompareAndSetRunLifecycle writes lc only when the stored lifecycle version equals
// expectedVersion (0 = no lifecycle stored yet) and returns the new version. A stale expected
// version fails with ErrLifecycleVersionConflict and writes nothing. This is the internal CAS
// seam only; no caller uses it yet (caller wiring is a later gate).
func (s *PostgresStore) CompareAndSetRunLifecycle(ctx context.Context, lc domain.RunLifecycle, expectedVersion int64) (int64, error) {
	if strings.TrimSpace(lc.RunID) == "" {
		return 0, errRunIDRequired("compare-and-set run lifecycle")
	}
	data, err := exactJSON(lc, "run lifecycle")
	if err != nil {
		return 0, err
	}
	var newVersion int64
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var current int64
		err := tx.QueryRowContext(ctx, `SELECT version FROM ah_run_lifecycles WHERE run_id = $1`, []byte(lc.RunID)).Scan(&current)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			current = 0
		case err != nil:
			return err
		}
		if current != expectedVersion {
			return fmt.Errorf("run %q: expected version %d, stored %d: %w", lc.RunID, expectedVersion, current, ErrLifecycleVersionConflict)
		}
		newVersion = current + 1
		if current == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO ah_run_lifecycles (run_id, sample_run_id, version, lifecycle_json)
				VALUES ($1, $2, $3, $4)`, []byte(lc.RunID), []byte(lc.SampleRunID), newVersion, data)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE ah_run_lifecycles SET sample_run_id = $2, version = $3, lifecycle_json = $4
				WHERE run_id = $1 AND version = $5`, []byte(lc.RunID), []byte(lc.SampleRunID), newVersion, data, current)
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// GetRunLifecycleVersion returns the lifecycle with its stored version (0, false when absent).
func (s *PostgresStore) GetRunLifecycleVersion(ctx context.Context, runID string) (domain.RunLifecycle, int64, bool, error) {
	var data []byte
	var version int64
	err := s.db.QueryRowContext(ctx, `SELECT version, lifecycle_json FROM ah_run_lifecycles WHERE run_id = $1`,
		[]byte(runID)).Scan(&version, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RunLifecycle{}, 0, false, nil
	}
	if err != nil {
		return domain.RunLifecycle{}, 0, false, err
	}
	var lc domain.RunLifecycle
	if err := json.Unmarshal(data, &lc); err != nil {
		return domain.RunLifecycle{}, 0, false, fmt.Errorf("unmarshal lifecycle_json: %w", err)
	}
	return lc, version, true, nil
}

func (s *PostgresStore) GetRunLifecycle(ctx context.Context, runID string) (domain.RunLifecycle, bool, error) {
	lc, _, ok, err := s.GetRunLifecycleVersion(ctx, runID)
	return lc, ok, err
}

func (s *PostgresStore) ListRunLifecyclesBySample(ctx context.Context, sampleRunID string) ([]domain.RunLifecycle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT lifecycle_json FROM ah_run_lifecycles
		WHERE sample_run_id = $1 ORDER BY run_id ASC`, []byte(sampleRunID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.RunLifecycle
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var lc domain.RunLifecycle
		if err := json.Unmarshal(data, &lc); err != nil {
			return nil, fmt.Errorf("unmarshal lifecycle_json: %w", err)
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}

var _ Store = (*PostgresStore)(nil)
