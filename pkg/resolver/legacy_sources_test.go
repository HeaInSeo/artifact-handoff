package resolver

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

const legacyArtifactID = "R1/producer-a/attempt-1/dataset"

// seedPreF4SourcesDB builds a pre-F4 (SampleRunID-keyed) store file holding one legacy
// artifact and one READY legacy source row for it, as an older binary would leave them.
// The legacy SampleRunID "R1" deliberately looks like a RunID.
func seedPreF4SourcesDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy-sources.db")
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
		`INSERT INTO artifacts (key, sample_run_id, producer_node_id, producer_attempt_id, output_name, artifact_id, digest, created_at)
			VALUES ('` + legacyArtifactID + `', 'R1', 'producer-a', 'attempt-1', 'dataset',
			        '` + legacyArtifactID + `', 'sha256:legacy', '2026-01-01T00:00:00Z')`,
		`INSERT INTO artifact_sources (source_id, artifact_id, backend_id, digest, state, location_json, created_at, updated_at)
			VALUES ('src-legacy', '` + legacyArtifactID + `', 'legacy-http', 'sha256:legacy', 'ready',
			        '{"http":{"uri":"http://artifact-source.local/legacy"}}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	return path
}

// F1 (G1): a legacy-unresolved source row is quarantined like its artifact — the source
// APIs neither return its location nor change its state, while sources of a live Run
// keep working. The legacy row is neither deleted nor backfilled.
func TestF1_LegacySourcesAreQuarantined(t *testing.T) {
	ctx := context.Background()
	path := seedPreF4SourcesDB(t)
	store, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	svc := newTestService(t, store)

	// A new Run R1 with the same producer/attempt/output registers next to the legacy row.
	if _, err := svc.RegisterArtifact(ctx, r10Artifact("R1", "sha256:live")); err != nil {
		t.Fatalf("register live R1: %v", err)
	}
	live, ok, err := svc.GetArtifact(ctx, "R1", "producer-a", "attempt-1", "dataset")
	if err != nil || !ok {
		t.Fatalf("get live R1: ok=%v err=%v", ok, err)
	}

	if list, err := svc.ListSources(ctx, legacyArtifactID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListSources(legacy) = %+v, err %v; want ErrNotFound (legacy location returned)", list, err)
	}
	if got, err := svc.UpdateSourceState(ctx, "src-legacy", domain.SourceStateDeleted); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSourceState(legacy) = %+v, err %v; want ErrNotFound (legacy source mutated)", got, err)
	}
	if _, _, _, err := svc.VerifySource(ctx, "src-legacy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("VerifySource(legacy) err = %v, want ErrNotFound", err)
	}
	if _, ok, _ := store.GetArtifactSource(ctx, "src-legacy"); ok {
		t.Fatal("store returned the legacy source by its source ID")
	}
	if list, _ := store.ListArtifactSources(ctx, legacyArtifactID); len(list) != 0 {
		t.Fatalf("store listed legacy sources: %+v", list)
	}

	// The live Run's sources are unaffected.
	sources, err := svc.ListSources(ctx, live.ArtifactID)
	if err != nil || len(sources) == 0 {
		t.Fatalf("ListSources(live) = %+v, err %v; want live sources", sources, err)
	}
	for _, s := range sources {
		if s.SourceID == "src-legacy" || s.ArtifactID != live.ArtifactID {
			t.Fatalf("live listing leaked a foreign source: %+v", s)
		}
	}
	updated, err := svc.UpdateSourceState(ctx, sources[0].SourceID, domain.SourceStateUnreachable)
	if err != nil || updated.State != domain.SourceStateUnreachable {
		t.Fatalf("UpdateSourceState(live) = %+v, err %v", updated, err)
	}
	_ = store.Close()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_sources WHERE source_id = 'src-legacy'
		AND artifact_id = ? AND state = 'ready'`, legacyArtifactID).Scan(&n); err != nil {
		t.Fatalf("raw legacy source: %v", err)
	}
	if n != 1 {
		t.Fatalf("legacy source rows untouched = %d, want 1 (no delete, no backfill, no state change)", n)
	}
}

// n1Source is an AddSource request that reuses sourceID for a live artifact.
func n1Source(sourceID, uri string) domain.ArtifactSource {
	return domain.ArtifactSource{
		SourceID:  sourceID,
		BackendID: "lab-http-cache",
		Location:  domain.Location{HTTP: &domain.HTTPSource{URI: uri}},
	}
}

// N1: AddSource through a live Run's artifact cannot rewrite a legacy source row by
// reusing its source ID. The call fails as AlreadyExists (not a nil error with a zero
// source) and the legacy row keeps its owner, state and location.
func TestN1_AddSourceCannotOverwriteLegacySource(t *testing.T) {
	ctx := context.Background()
	path := seedPreF4SourcesDB(t)
	store, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	svc := newTestService(t, store)
	if _, err := svc.RegisterArtifact(ctx, r10Artifact("R1", "sha256:live")); err != nil {
		t.Fatalf("register live R1: %v", err)
	}
	live, ok, err := svc.GetArtifact(ctx, "R1", "producer-a", "attempt-1", "dataset")
	if err != nil || !ok {
		t.Fatalf("get live R1: ok=%v err=%v", ok, err)
	}

	got, err := svc.AddSource(ctx, live.ArtifactID, n1Source("src-legacy", "http://artifact-source.local/hijack"))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("AddSource(reused legacy source id) = %+v, err %v; want ErrAlreadyExists", got, err)
	}
	if list, err := svc.ListSources(ctx, live.ArtifactID); err != nil {
		t.Fatalf("ListSources(live): %v", err)
	} else {
		for _, s := range list {
			if s.SourceID == "src-legacy" {
				t.Fatalf("live artifact acquired the legacy source: %+v", s)
			}
		}
	}
	_ = store.Close()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_sources WHERE source_id = 'src-legacy'
		AND artifact_id = ? AND state = 'ready' AND location_json LIKE '%/legacy"%'`, legacyArtifactID).Scan(&n); err != nil {
		t.Fatalf("raw legacy source: %v", err)
	}
	if n != 1 {
		t.Fatalf("legacy source rows untouched = %d, want 1 (owner/state/location rewritten)", n)
	}
}

// N1: a source ID owned by Run R2's artifact cannot be rewritten through Run R1's
// artifact, on either store.
func TestN1_AddSourceCannotOverwriteOtherRunSource(t *testing.T) {
	stores := map[string]func(t *testing.T) inventory.Store{
		"memory": func(*testing.T) inventory.Store { return inventory.NewMemoryStore() },
		"sqlite": func(t *testing.T) inventory.Store {
			s, err := inventory.NewSQLiteStore(filepath.Join(t.TempDir(), "n1.db"))
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			return s
		},
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc := newTestService(t, open(t))
			arts := map[string]domain.Artifact{}
			for _, run := range []string{"R1", "R2"} {
				if _, err := svc.RegisterArtifact(ctx, r10Artifact(run, "sha256:"+run)); err != nil {
					t.Fatalf("register %s: %v", run, err)
				}
				a, ok, err := svc.GetArtifact(ctx, run, "producer-a", "attempt-1", "dataset")
				if err != nil || !ok {
					t.Fatalf("get %s: ok=%v err=%v", run, ok, err)
				}
				arts[run] = a
			}
			r2src, err := svc.AddSource(ctx, arts["R2"].ArtifactID, n1Source("src-shared", "http://artifact-source.local/r2"))
			if err != nil {
				t.Fatalf("add R2 source: %v", err)
			}

			got, err := svc.AddSource(ctx, arts["R1"].ArtifactID, n1Source("src-shared", "http://artifact-source.local/r1"))
			if !errors.Is(err, ErrAlreadyExists) {
				t.Fatalf("AddSource(R1, R2's source id) = %+v, err %v; want ErrAlreadyExists", got, err)
			}
			after, err := svc.ListSources(ctx, arts["R2"].ArtifactID)
			if err != nil {
				t.Fatalf("ListSources(R2): %v", err)
			}
			found := false
			for _, s := range after {
				if s.SourceID != "src-shared" {
					continue
				}
				found = true
				if s.ArtifactID != arts["R2"].ArtifactID || s.State != r2src.State ||
					s.Location.HTTP == nil || s.Location.HTTP.URI != "http://artifact-source.local/r2" {
					t.Fatalf("R2 source rewritten through R1: %+v", s)
				}
			}
			if !found {
				t.Fatal("R2 source disappeared")
			}

			// Re-adding a source through its own artifact still updates it.
			if _, err := svc.AddSource(ctx, arts["R2"].ArtifactID, n1Source("src-shared", "http://artifact-source.local/r2")); err != nil {
				t.Fatalf("idempotent re-add through the owner: %v", err)
			}
		})
	}
}

// F1: sources follow Run identity — mutating R1's source never changes R2's sources of
// the same Sample, and R1 becoming GC-eligible leaves R2's sources listed.
func TestF1_SourcesAreRunScoped(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, inventory.NewMemoryStore())
	arts := map[string]domain.Artifact{}
	for _, run := range []string{"R1", "R2"} {
		if _, err := svc.RegisterArtifact(ctx, r10Artifact(run, "sha256:"+run)); err != nil {
			t.Fatalf("register %s: %v", run, err)
		}
		a, ok, err := svc.GetArtifact(ctx, run, "producer-a", "attempt-1", "dataset")
		if err != nil || !ok {
			t.Fatalf("get %s: ok=%v err=%v", run, ok, err)
		}
		arts[run] = a
		if err := svc.NotifyNodeTerminal(ctx, run, "producer-a", "attempt-1", "Succeeded"); err != nil {
			t.Fatalf("notify %s: %v", run, err)
		}
	}
	r1, err := svc.ListSources(ctx, arts["R1"].ArtifactID)
	if err != nil || len(r1) == 0 {
		t.Fatalf("R1 sources = %+v, err %v", r1, err)
	}
	for _, s := range r1 {
		if _, err := svc.UpdateSourceState(ctx, s.SourceID, domain.SourceStateDeleted); err != nil {
			t.Fatalf("delete R1 source %s: %v", s.SourceID, err)
		}
	}
	if err := svc.FinalizeRun(ctx, "R1", r10Sample); err != nil {
		t.Fatalf("finalize R1: %v", err)
	}
	if err := svc.EvaluateRunGC(ctx, "R1"); err != nil {
		t.Fatalf("evaluate R1 GC: %v", err)
	}
	r2, err := svc.ListSources(ctx, arts["R2"].ArtifactID)
	if err != nil || len(r2) == 0 {
		t.Fatalf("R2 sources after R1 delete/GC = %+v, err %v; want untouched", r2, err)
	}
	for _, s := range r2 {
		if s.ArtifactID != arts["R2"].ArtifactID || s.State != domain.SourceStateReady {
			t.Fatalf("R2 source changed by R1: %+v", s)
		}
	}
}
