package storetest

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/internal/ids"
	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// SQLiteHarness is the SQLiteStore harness, exported to the external test
// package: Reopen closes the store and opens a new SQLiteStore on the same file,
// and legacy rows are written and read with raw SQL on that file.
var SQLiteHarness = func() Harness { return newSQLiteFiles().harness() }

// sqliteFiles maps each open SQLiteStore to its database file.
type sqliteFiles struct {
	mu    sync.Mutex
	paths map[inventory.Store]string
}

func newSQLiteFiles() *sqliteFiles { return &sqliteFiles{paths: map[inventory.Store]string{}} }

// unwrapper is implemented by the broken-store wrappers below.
type unwrapper interface{ unwrap() *inventory.SQLiteStore }

func (f *sqliteFiles) underlying(s inventory.Store) *inventory.SQLiteStore {
	if u, ok := s.(unwrapper); ok {
		return u.unwrap()
	}
	return s.(*inventory.SQLiteStore)
}

func (f *sqliteFiles) open(t *testing.T, path string) *inventory.SQLiteStore {
	t.Helper()
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f.mu.Lock()
	f.paths[s] = path
	f.mu.Unlock()
	return s
}

func (f *sqliteFiles) path(s inventory.Store) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paths[f.underlying(s)]
}

func (f *sqliteFiles) reopen(t *testing.T, s inventory.Store) *inventory.SQLiteStore {
	t.Helper()
	path := f.path(s)
	if err := f.underlying(s).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return f.open(t, path)
}

func (f *sqliteFiles) harness() Harness {
	return Harness{
		New: func(t *testing.T) inventory.Store {
			return f.open(t, filepath.Join(t.TempDir(), "inventory.db"))
		},
		Reopen: func(t *testing.T, s inventory.Store) inventory.Store { return f.reopen(t, s) },
		SeedLegacy: func(t *testing.T, s inventory.Store, rows LegacyRows) {
			seedLegacySQLite(t, f.path(s), rows)
		},
		LegacySnapshot: func(t *testing.T, s inventory.Store) string { return legacySnapshotSQLite(t, f.path(s)) },
	}
}

// rawOpen opens a second, raw handle on the store file.
func rawOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("raw open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func rawExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func rawTime(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

// seedLegacySQLite inserts rows with an empty run_id into the migrated store file.
// The terminal key is the one an empty-RunID lookup would compute, so only the
// RunID guard hides it.
func seedLegacySQLite(t *testing.T, path string, rows LegacyRows) {
	t.Helper()
	db := rawOpen(t, path)
	a, src, term := rows.Artifact, rows.Source, rows.Terminal
	rawExec(t, db, `INSERT INTO artifacts (key, run_id, sample_run_id, producer_node_id, producer_attempt_id, output_name,
		artifact_id, digest, created_at) VALUES (?, '', ?, ?, ?, ?, ?, ?, ?)`,
		a.Key(), a.SampleRunID, a.ProducerNodeID, a.ProducerAttemptID, a.OutputName, a.ArtifactID, a.Digest, rawTime(a.CreatedAt))
	rawExec(t, db, `INSERT INTO artifact_sources (source_id, artifact_id, backend_id, digest, state, location_fingerprint,
		location_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, '{}', ?, ?)`,
		src.SourceID, src.ArtifactID, src.BackendID, src.Digest, string(src.State), src.LocationFingerprint,
		rawTime(src.CreatedAt), rawTime(src.UpdatedAt))
	rawExec(t, db, `INSERT INTO node_terminals (key, run_id, sample_run_id, node_id, attempt_id, terminal_state, recorded_at)
		VALUES (?, '', ?, ?, ?, ?, ?)`,
		ids.NodeAttemptKey{RunID: term.RunID, NodeID: term.NodeID, AttemptID: term.AttemptID}.String(),
		sampleRun, term.NodeID, term.AttemptID, term.TerminalState, rawTime(term.RecordedAt))
	lc := rows.Lifecycle
	rawExec(t, db, `INSERT INTO sample_run_lifecycles (sample_run_id, finalized, finalized_at, retention_policy_source,
		retention_duration_ns, retention_until, gc_eligible, gc_eligible_at, gc_blocked_reason, terminal_node_count,
		succeeded_node_count, failed_node_count, canceled_node_count, retained_artifact_count, retained_artifact_bytes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lc.SampleRunID, lc.Finalized, rawTimePtr(lc.FinalizedAt), lc.RetentionPolicySource, int64(lc.RetentionDuration),
		rawTimePtr(lc.RetentionUntil), lc.GCEligible, rawTimePtr(lc.GCEligibleAt), lc.GCBlockedReason, lc.TerminalNodeCount,
		lc.SucceededNodeCount, lc.FailedNodeCount, lc.CanceledNodeCount, lc.RetainedArtifactCount, lc.RetainedArtifactBytes)
}

// rawTimePtr is rawTime for an optional time; nil is stored as NULL.
func rawTimePtr(at *time.Time) any {
	if at == nil {
		return nil
	}
	return rawTime(*at)
}

// legacySnapshotTables selects the legacy rows of each table: every row with no
// RunID, every source not owned by a live artifact, and every pre-F4 Sample
// lifecycle, each in primary-key order.
var legacySnapshotTables = []struct{ table, where, order string }{
	{"artifacts", "run_id = ''", "key"},
	{"artifact_sources", "artifact_id NOT IN (SELECT artifact_id FROM artifacts WHERE run_id <> '')", "source_id"},
	{"node_terminals", "run_id = ''", "key"},
	{"sample_run_lifecycles", "1 = 1", "sample_run_id"},
}

// legacySnapshotSQLite dumps every persisted column of the legacy rows in a
// stable order. Columns are read from the table schema, so a column added later
// is covered too, and each value is rendered with SQLite quote(), which keeps
// NULL, integer and text distinct.
func legacySnapshotSQLite(t *testing.T, path string) string {
	t.Helper()
	db := rawOpen(t, path)
	var lines []string
	for _, tbl := range legacySnapshotTables {
		cols := tableColumns(t, db, tbl.table)
		fields := make([]string, 0, len(cols))
		for _, c := range cols {
			fields = append(fields, "'"+c+"=' || quote("+c+")")
		}
		q := `SELECT '` + tbl.table + `|' || ` + strings.Join(fields, ` || '|' || `) + //nolint:gosec // constant tables/predicates; columns from the schema
			` FROM ` + tbl.table + ` WHERE ` + tbl.where + ` ORDER BY ` + tbl.order
		rows, err := db.Query(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan: %v", err)
			}
			lines = append(lines, line)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close rows: %v", err)
		}
	}
	return strings.Join(lines, "\n")
}

// tableColumns lists table's columns in schema order.
func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan column of %s: %v", table, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil || len(cols) == 0 {
		t.Fatalf("columns of %s: %v (n=%d)", table, err, len(cols))
	}
	return cols
}

// leakByID resolves an artifact ID without the RunID guard.
type leakByID struct {
	*inventory.SQLiteStore
	raw *sql.DB
}

func (s leakByID) unwrap() *inventory.SQLiteStore { return s.SQLiteStore }

func (s leakByID) GetArtifactByID(ctx context.Context, id string) (domain.Artifact, bool, error) {
	if a, ok, err := s.SQLiteStore.GetArtifactByID(ctx, id); ok || err != nil {
		return a, ok, err
	}
	var a domain.Artifact
	err := s.raw.QueryRowContext(ctx, `SELECT artifact_id, sample_run_id FROM artifacts WHERE artifact_id = ?`, id).
		Scan(&a.ArtifactID, &a.SampleRunID)
	if err == sql.ErrNoRows {
		return domain.Artifact{}, false, nil
	}
	return a, err == nil, err
}

// claimingSources re-owns an existing source ID to the caller before upserting.
type claimingSources struct {
	*inventory.SQLiteStore
	raw *sql.DB
}

func (s claimingSources) unwrap() *inventory.SQLiteStore { return s.SQLiteStore }

func (s claimingSources) PutArtifactSources(ctx context.Context, artifactID string, sources []domain.ArtifactSource) error {
	for _, src := range sources {
		if _, err := s.raw.ExecContext(ctx, `UPDATE artifact_sources SET artifact_id = ? WHERE source_id = ?`, artifactID, src.SourceID); err != nil {
			return err
		}
	}
	return s.SQLiteStore.PutArtifactSources(ctx, artifactID, sources)
}

// wrapped returns a harness whose stores are wrap(store, raw handle).
func wrapped(wrap func(*inventory.SQLiteStore, *sql.DB) inventory.Store) Harness {
	f := newSQLiteFiles()
	h := f.harness()
	h.New = func(t *testing.T) inventory.Store {
		path := filepath.Join(t.TempDir(), "inventory.db")
		return wrap(f.open(t, path), rawOpen(t, path))
	}
	h.Reopen = func(t *testing.T, s inventory.Store) inventory.Store {
		path := f.path(s)
		return wrap(f.reopen(t, s), rawOpen(t, path))
	}
	return h
}

// dropLegacyOnReopen deletes legacy rows as part of reopening.
func dropLegacyOnReopen() Harness {
	f := newSQLiteFiles()
	h := f.harness()
	h.Reopen = func(t *testing.T, s inventory.Store) inventory.Store {
		db := rawOpen(t, f.path(s))
		rawExec(t, db, `DELETE FROM artifacts WHERE run_id = ''`)
		rawExec(t, db, `DELETE FROM node_terminals WHERE run_id = ''`)
		return f.reopen(t, s)
	}
	return h
}

// sampleLifecycleFallback attributes the pre-F4 Sample lifecycle to a Run that
// has no Run-keyed lifecycle of its own.
type sampleLifecycleFallback struct {
	*inventory.SQLiteStore
	raw *sql.DB
}

func (s sampleLifecycleFallback) unwrap() *inventory.SQLiteStore { return s.SQLiteStore }

func (s sampleLifecycleFallback) GetRunLifecycle(ctx context.Context, runID string) (domain.RunLifecycle, bool, error) {
	if lc, ok, err := s.SQLiteStore.GetRunLifecycle(ctx, runID); ok || err != nil {
		return lc, ok, err
	}
	lc := domain.RunLifecycle{RunID: runID}
	err := s.raw.QueryRowContext(ctx, `SELECT l.sample_run_id, l.finalized, l.gc_eligible FROM sample_run_lifecycles l
		JOIN artifacts a ON a.sample_run_id = l.sample_run_id WHERE a.run_id = ? LIMIT 1`, runID).
		Scan(&lc.SampleRunID, &lc.Finalized, &lc.GCEligible)
	if err == sql.ErrNoRows {
		return domain.RunLifecycle{}, false, nil
	}
	return lc, err == nil, err
}

// mutateLegacyOnReopen runs stmt against the legacy rows as part of reopening,
// as a migration that rewrites a column would.
func mutateLegacyOnReopen(stmt string) Harness {
	f := newSQLiteFiles()
	h := f.harness()
	h.Reopen = func(t *testing.T, s inventory.Store) inventory.Store {
		rawExec(t, rawOpen(t, f.path(s)), stmt)
		return f.reopen(t, s)
	}
	return h
}

// Every legacy case must reject the broken store it targets, so a check that
// silently passes is caught.
func TestLegacyCasesRejectBrokenStores(t *testing.T) {
	cases := []struct {
		mutant  string
		harness Harness
		rejects []string
	}{
		{
			mutant: "artifact ID lookup without RunID guard",
			harness: wrapped(func(s *inventory.SQLiteStore, raw *sql.DB) inventory.Store {
				return leakByID{SQLiteStore: s, raw: raw}
			}),
			rejects: []string{"Legacy/InvisibleAcrossReopen", "Legacy/RetainedIdempotentReopen", "Legacy/SourceNotClaimableByLive"},
		},
		{
			mutant:  "reopen deletes legacy rows",
			harness: dropLegacyOnReopen(),
			rejects: []string{"Legacy/RetainedIdempotentReopen"},
		},
		{
			mutant: "source upsert re-owns a foreign source",
			harness: wrapped(func(s *inventory.SQLiteStore, raw *sql.DB) inventory.Store {
				return claimingSources{SQLiteStore: s, raw: raw}
			}),
			rejects: []string{"Legacy/SourceNotClaimableByLive"},
		},
		{
			mutant: "legacy Sample lifecycle attributed to a live Run",
			harness: wrapped(func(s *inventory.SQLiteStore, raw *sql.DB) inventory.Store {
				return sampleLifecycleFallback{SQLiteStore: s, raw: raw}
			}),
			rejects: []string{"Legacy/InvisibleAcrossReopen"},
		},
		{
			mutant:  "reopen rewrites legacy artifact size",
			harness: mutateLegacyOnReopen(`UPDATE artifacts SET size_bytes = size_bytes + 1 WHERE run_id = ''`),
			rejects: []string{"Legacy/RetainedIdempotentReopen"},
		},
		{
			mutant:  "reopen rewrites legacy source error",
			harness: mutateLegacyOnReopen(`UPDATE artifact_sources SET last_error = 'migrated' WHERE source_id = '` + legacySourceID + `'`),
			rejects: []string{"Legacy/RetainedIdempotentReopen"},
		},
		{
			mutant:  "reopen rewrites legacy terminal attempt",
			harness: mutateLegacyOnReopen(`UPDATE node_terminals SET attempt_id = 'attempt-x' WHERE run_id = ''`),
			rejects: []string{"Legacy/RetainedIdempotentReopen"},
		},
		{
			mutant:  "reopen nulls legacy lifecycle GC time",
			harness: mutateLegacyOnReopen(`UPDATE sample_run_lifecycles SET gc_eligible_at = NULL`),
			rejects: []string{"Legacy/RetainedIdempotentReopen"},
		},
	}
	for _, tc := range cases {
		for _, name := range tc.rejects {
			t.Run(tc.mutant+"/"+name, func(t *testing.T) {
				for _, c := range legacyCases {
					if c.name == name {
						if err := c.run(t, tc.harness); err == nil {
							t.Fatalf("case %q accepted broken store %q", name, tc.mutant)
						}
						return
					}
				}
				t.Fatalf("no legacy case %q", name)
			})
		}
	}
}
