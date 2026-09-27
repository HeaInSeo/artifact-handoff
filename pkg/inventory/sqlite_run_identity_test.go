package inventory_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// seedPreF4DB builds a store file with the pre-F4 (SampleRunID-keyed) schema and one
// legacy row per table, exactly as an older binary would have left it.
func seedPreF4DB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, stmt := range []string{
		`CREATE TABLE artifacts (key TEXT PRIMARY KEY, sample_run_id TEXT NOT NULL, producer_node_id TEXT NOT NULL,
			producer_attempt_id TEXT NOT NULL, output_name TEXT NOT NULL, artifact_id TEXT NOT NULL DEFAULT '',
			digest TEXT NOT NULL DEFAULT '', logical_uri TEXT NOT NULL DEFAULT '', node_name TEXT NOT NULL DEFAULT '',
			uri TEXT NOT NULL DEFAULT '', locations_json TEXT NOT NULL DEFAULT '[]', size_bytes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL)`,
		`CREATE TABLE artifact_sources (source_id TEXT PRIMARY KEY, artifact_id TEXT NOT NULL, backend_id TEXT NOT NULL,
			digest TEXT NOT NULL DEFAULT '', state TEXT NOT NULL, location_fingerprint TEXT NOT NULL DEFAULT '',
			location_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			last_verified_at TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE node_terminals (key TEXT PRIMARY KEY, sample_run_id TEXT NOT NULL, node_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL, terminal_state TEXT NOT NULL, recorded_at TEXT NOT NULL)`,
		`CREATE TABLE sample_run_lifecycles (sample_run_id TEXT PRIMARY KEY, finalized INTEGER NOT NULL DEFAULT 0,
			finalized_at TEXT, retention_policy_source TEXT NOT NULL DEFAULT '', retention_duration_ns INTEGER NOT NULL DEFAULT 0,
			retention_until TEXT, gc_eligible INTEGER NOT NULL DEFAULT 0, gc_eligible_at TEXT,
			gc_blocked_reason TEXT NOT NULL DEFAULT '', terminal_node_count INTEGER NOT NULL DEFAULT 0,
			succeeded_node_count INTEGER NOT NULL DEFAULT 0, failed_node_count INTEGER NOT NULL DEFAULT 0,
			canceled_node_count INTEGER NOT NULL DEFAULT 0, retained_artifact_count INTEGER NOT NULL DEFAULT 0,
			retained_artifact_bytes INTEGER NOT NULL DEFAULT 0)`,
		// The legacy row's SampleRunID is deliberately a string that also looks like a
		// RunID ("R1"): the migration must still not attribute it to Run R1.
		`INSERT INTO artifacts (key, sample_run_id, producer_node_id, producer_attempt_id, output_name, artifact_id, digest, created_at)
			VALUES ('R1/producer-a/attempt-1/dataset', 'R1', 'producer-a', 'attempt-1', 'dataset',
			        'R1/producer-a/attempt-1/dataset', 'sha256:legacy', '2026-01-01T00:00:00Z')`,
		`INSERT INTO node_terminals (key, sample_run_id, node_id, attempt_id, terminal_state, recorded_at)
			VALUES ('R1/producer-a/attempt-1', 'R1', 'producer-a', 'attempt-1', 'Succeeded', '2026-01-01T00:00:00Z')`,
		`INSERT INTO sample_run_lifecycles (sample_run_id, finalized, gc_eligible) VALUES ('R1', 1, 1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	return path
}

func rawCount(t *testing.T, path, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// R10-5 / R07-2: an ambiguous pre-F4 row is never attributed to a Run — not by key, not
// by artifact ID, not by run or sample grouping, and its lifecycle never becomes a Run's
// GC state — yet it is neither deleted nor backfilled, and its disposition is recorded.
func TestSQLite_LegacyRowsAreQuarantinedNotAttributed(t *testing.T) {
	ctx := context.Background()
	path := seedPreF4DB(t)
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}

	if _, ok, _ := s.GetArtifact(ctx, "R1", "producer-a", "attempt-1", "dataset"); ok {
		t.Fatal("legacy artifact attributed to Run R1 by key")
	}
	if _, ok, _ := s.GetArtifactByID(ctx, "R1/producer-a/attempt-1/dataset"); ok {
		t.Fatal("legacy artifact returned by its legacy artifact ID")
	}
	if list, _ := s.ListArtifactsByRun(ctx, "R1"); len(list) != 0 {
		t.Fatalf("legacy artifact listed under Run R1: %+v", list)
	}
	if list, _ := s.ListArtifactsBySampleRun(ctx, "R1"); len(list) != 0 {
		t.Fatalf("legacy artifact returned by sample grouping: %+v", list)
	}
	if _, ok, _ := s.GetNodeTerminal(ctx, "R1", "producer-a", "attempt-1"); ok {
		t.Fatal("legacy terminal attributed to Run R1")
	}
	if _, ok, _ := s.GetRunLifecycle(ctx, "R1"); ok {
		t.Fatal("legacy GC-eligible sample lifecycle became Run R1's lifecycle")
	}

	disp, err := s.LegacyDisposition(ctx)
	if err != nil {
		t.Fatalf("LegacyDisposition: %v", err)
	}
	want := map[string]string{
		"schema_version":                          "2",
		"legacy_unresolved_artifacts":             "1",
		"legacy_unresolved_node_terminals":        "1",
		"legacy_unresolved_sample_run_lifecycles": "1",
	}
	for k, v := range want {
		if disp[k] != v {
			t.Errorf("disposition %s = %q, want %q (all: %v)", k, disp[k], v, disp)
		}
	}

	// A new Run R1 registers alongside the quarantined row without colliding.
	if err := s.PutArtifact(ctx, domain.Artifact{RunID: "R1", ProducerNodeID: "producer-a", ProducerAttemptID: "attempt-1",
		OutputName: "dataset", Digest: "sha256:new"}); err != nil {
		t.Fatalf("new run artifact next to legacy row: %v", err)
	}
	got, ok, _ := s.GetArtifact(ctx, "R1", "producer-a", "attempt-1", "dataset")
	if !ok || got.Digest != "sha256:new" {
		t.Fatalf("new run artifact = %+v ok=%v", got, ok)
	}
	_ = s.Close()

	// Nothing was deleted or backfilled.
	if n := rawCount(t, path, `SELECT COUNT(*) FROM artifacts WHERE run_id = '' AND digest = 'sha256:legacy'`); n != 1 {
		t.Fatalf("legacy artifact rows untouched = %d, want 1 (no delete, no backfill)", n)
	}
	if n := rawCount(t, path, `SELECT COUNT(*) FROM sample_run_lifecycles`); n != 1 {
		t.Fatalf("legacy lifecycle rows = %d, want 1", n)
	}
}

// A3: the migration is one transaction and idempotent — reopening a migrated store
// neither re-counts nor re-stamps the disposition.
func TestSQLite_RunIdentityMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := seedPreF4DB(t)
	for i := 0; i < 3; i++ {
		s, err := inventory.NewSQLiteStore(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		if i == 1 {
			// Rows written after the upgrade must not be counted as legacy on reopen.
			if err := s.PutArtifact(ctx, domain.Artifact{RunID: "R9", ProducerNodeID: "p", ProducerAttemptID: "a", OutputName: "o"}); err != nil {
				t.Fatalf("put: %v", err)
			}
		}
		disp, err := s.LegacyDisposition(ctx)
		if err != nil {
			t.Fatalf("disposition #%d: %v", i, err)
		}
		if disp["schema_version"] != "2" || disp["legacy_unresolved_artifacts"] != "1" {
			t.Fatalf("reopen #%d disposition = %v", i, disp)
		}
		_ = s.Close()
	}
}

// A2: a store stamped by a newer schema is refused instead of being mis-keyed.
func TestSQLite_RefusesNewerSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := db.Exec(`UPDATE ah_schema_meta SET value = '3' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("stamp newer: %v", err)
	}
	_ = db.Close()
	if _, err := inventory.NewSQLiteStore(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("opening a newer-schema store: err = %v, want refusal", err)
	}
}

// Store-level fail-closed: nothing is persisted without a RunID.
func TestSQLite_WritesRequireRunID(t *testing.T) {
	ctx := context.Background()
	s, cleanup := openSQLite(t)
	defer cleanup()
	if err := s.PutArtifact(ctx, domain.Artifact{SampleRunID: "S", ProducerNodeID: "p", ProducerAttemptID: "a", OutputName: "o"}); err == nil {
		t.Fatal("artifact without runID persisted")
	}
	if err := s.RecordNodeTerminal(ctx, domain.NodeTerminalRecord{NodeID: "p", AttemptID: "a", TerminalState: "Succeeded"}); err == nil {
		t.Fatal("terminal without runID persisted")
	}
	if err := s.UpsertRunLifecycle(ctx, domain.RunLifecycle{SampleRunID: "S"}); err == nil {
		t.Fatal("lifecycle without runID persisted")
	}
}
