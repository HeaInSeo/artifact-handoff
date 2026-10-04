package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// pgTestDSNEnv names a disposable PostgreSQL 18.6 database (TX1-CONFORMANCE profile). The
// PostgreSQL tests skip without it and are reported NOT VERIFIED, never PASS.
const pgTestDSNEnv = "AH_TEST_POSTGRES_DSN"

// pgSchemaDSN creates a fresh schema and returns a DSN whose search_path points at it, so each
// test (and each helper process it spawns) gets its own empty store.
func pgSchemaDSN(t *testing.T) string {
	t.Helper()
	base := os.Getenv(pgTestDSNEnv)
	if base == "" {
		t.Skipf("%s not set: PostgreSQL adapter test NOT VERIFIED", pgTestDSNEnv)
	}
	db, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer func() { _ = db.Close() }()
	schema := fmt.Sprintf("ah_t_%d", time.Now().UnixNano())
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		admin, err := sql.Open("pgx", base)
		if err == nil {
			_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
			_ = admin.Close()
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", pgTestDSNEnv, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func openPG(t *testing.T, dsn string) *inventory.PostgresStore {
	t.Helper()
	s, err := inventory.NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPostgres_RoundTripAndReopen(t *testing.T) {
	dsn := pgSchemaDSN(t)
	ctx := context.Background()
	s := openPG(t, dsn)
	a := domain.Artifact{RunID: "run-1", SampleRunID: "sample-1", ProducerNodeID: "node", ProducerAttemptID: "a1",
		OutputName: "out", ArtifactID: "art-1", Digest: "sha256:1", URI: "", SizeBytes: 7, CreatedAt: time.Unix(5, 0).UTC()}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("same-digest replay must converge: %v", err)
	}
	b := a
	b.Digest = "sha256:2"
	if err := s.PutArtifact(ctx, b); err == nil || !strings.Contains(err.Error(), "digest conflict") {
		t.Fatalf("digest conflict: got %v", err)
	}
	term := domain.NodeTerminalRecord{RunID: "run-1", NodeID: "node", AttemptID: "a1", TerminalState: "Succeeded", RecordedAt: time.Unix(6, 0).UTC()}
	if err := s.RecordNodeTerminal(ctx, term); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	_ = s.Close()

	reopened := openPG(t, dsn)
	got, ok, err := reopened.GetArtifact(ctx, "run-1", "node", "a1", "out")
	if err != nil || !ok || got.Digest != "sha256:1" || got.SizeBytes != 7 || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("reopen artifact: %+v ok=%v err=%v", got, ok, err)
	}
	rec, ok, err := reopened.GetNodeTerminal(ctx, "run-1", "node", "a1")
	if err != nil || !ok || rec.TerminalState != "Succeeded" {
		t.Fatalf("reopen terminal: %+v ok=%v err=%v", rec, ok, err)
	}
}

// TX1-07 (2): the first writer commits and its ACK is lost (it goes away). A retry of the same
// request through another store instance converges on the committed rows: no second row, no
// new identity, and a different digest/state is still a conflict.
func TestPostgres_AckLossRetryConverges(t *testing.T) {
	dsn := pgSchemaDSN(t)
	ctx := context.Background()
	a := domain.Artifact{RunID: "run-ack", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o", ArtifactID: "art-ack", Digest: "sha256:ack"}
	term := domain.NodeTerminalRecord{RunID: "run-ack", NodeID: "n", AttemptID: "a", TerminalState: "Succeeded"}
	src := domain.ArtifactSource{SourceID: "src-ack", BackendID: "node-local-default", State: domain.SourceStateReady}

	first := openPG(t, dsn)
	if err := first.PutArtifact(ctx, a); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := first.RecordNodeTerminal(ctx, term); err != nil {
		t.Fatalf("first terminal: %v", err)
	}
	if err := first.PutArtifactSources(ctx, "art-ack", []domain.ArtifactSource{src}); err != nil {
		t.Fatalf("first sources: %v", err)
	}
	_ = first.Close() // the caller never saw the ACKs

	retry := openPG(t, dsn)
	if err := retry.PutArtifact(ctx, a); err != nil {
		t.Fatalf("retried put: %v", err)
	}
	if err := retry.RecordNodeTerminal(ctx, term); err != nil {
		t.Fatalf("retried terminal: %v", err)
	}
	if err := retry.PutArtifactSources(ctx, "art-ack", []domain.ArtifactSource{src}); err != nil {
		t.Fatalf("retried sources: %v", err)
	}
	if list, err := retry.ListArtifactsByRun(ctx, "run-ack"); err != nil || len(list) != 1 {
		t.Fatalf("artifacts after retry = %d err=%v, want 1", len(list), err)
	}
	if list, err := retry.ListNodeTerminalsByRun(ctx, "run-ack"); err != nil || len(list) != 1 {
		t.Fatalf("terminals after retry = %d err=%v, want 1", len(list), err)
	}
	if list, err := retry.ListArtifactSources(ctx, "art-ack"); err != nil || len(list) != 1 {
		t.Fatalf("sources after retry = %d err=%v, want 1", len(list), err)
	}
	other := term
	other.TerminalState = "Failed"
	if err := retry.RecordNodeTerminal(ctx, other); err == nil || !strings.Contains(err.Error(), "terminal state conflict") {
		t.Fatalf("different terminal state after ACK loss: got %v", err)
	}
}

// C-2: identities keep their exact bytes. NUL and invalid UTF-8 are distinct identities, not
// rejected by the column type and not normalized.
func TestPostgres_IdentityBytesExact(t *testing.T) {
	s := openPG(t, pgSchemaDSN(t))
	ctx := context.Background()
	for i, run := range []string{"run\x00nul", "run\xffbad", "Run-Case", "run-case", " run-case"} {
		a := domain.Artifact{RunID: run, ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o",
			ArtifactID: fmt.Sprintf("art-%d", i), Digest: fmt.Sprintf("sha256:%d", i)}
		if err := s.PutArtifact(ctx, a); err != nil {
			t.Fatalf("put %q: %v", run, err)
		}
	}
	for i, run := range []string{"run\x00nul", "run\xffbad", "Run-Case", "run-case", " run-case"} {
		list, err := s.ListArtifactsByRun(ctx, run)
		if err != nil || len(list) != 1 || list[0].RunID != run || list[0].Digest != fmt.Sprintf("sha256:%d", i) {
			t.Fatalf("run %q: %+v err=%v", run, list, err)
		}
	}
}

// C-2: a value JSON would rewrite (invalid UTF-8 inside a location) is refused, not narrowed.
func TestPostgres_NonExactLocationRefused(t *testing.T) {
	s := openPG(t, pgSchemaDSN(t))
	ctx := context.Background()
	a := domain.Artifact{RunID: "run", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o", ArtifactID: "art", Digest: "sha256:x",
		Locations: []domain.Location{{HTTP: &domain.HTTPSource{URI: "http://h/\xff"}}}}
	if err := s.PutArtifact(ctx, a); !errors.Is(err, inventory.ErrIdentityUnsupported) {
		t.Fatalf("want ErrIdentityUnsupported, got %v", err)
	}
	if _, ok, _ := s.GetArtifact(ctx, "run", "n", "a", "o"); ok {
		t.Fatal("refused artifact was stored")
	}
}

func TestPostgres_SourceOwnershipConflictWritesNothing(t *testing.T) {
	s := openPG(t, pgSchemaDSN(t))
	ctx := context.Background()
	for _, id := range []string{"art-a", "art-b"} {
		if err := s.PutArtifact(ctx, domain.Artifact{RunID: "run", ProducerNodeID: id, ProducerAttemptID: "a", OutputName: "o", ArtifactID: id, Digest: "sha256:" + id}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	src := domain.ArtifactSource{SourceID: "src-1", BackendID: "node-local-default", State: domain.SourceStateReady}
	if err := s.PutArtifactSources(ctx, "art-a", []domain.ArtifactSource{src}); err != nil {
		t.Fatalf("own source: %v", err)
	}
	other := domain.ArtifactSource{SourceID: "src-2", BackendID: "node-local-default", State: domain.SourceStateReady}
	err := s.PutArtifactSources(ctx, "art-b", []domain.ArtifactSource{other, src})
	if !errors.Is(err, inventory.ErrSourceOwnershipConflict) {
		t.Fatalf("want ErrSourceOwnershipConflict, got %v", err)
	}
	if _, ok, _ := s.GetArtifactSource(ctx, "src-2"); ok {
		t.Fatal("conflicting call wrote part of its batch")
	}
	got, ok, err := s.GetArtifactSource(ctx, "src-1")
	if err != nil || !ok || got.ArtifactID != "art-a" {
		t.Fatalf("source changed owner: %+v ok=%v err=%v", got, ok, err)
	}
}

// TX1-05 / TX1-07: the versionless upsert is unsupported; the CAS seam rejects a stale version.
// R1/R2 of the same Sample keep separate lifecycles.
func TestPostgres_LifecycleCASAndUnsupportedUpsert(t *testing.T) {
	s := openPG(t, pgSchemaDSN(t))
	ctx := context.Background()
	if err := s.UpsertRunLifecycle(ctx, domain.RunLifecycle{RunID: "r1"}); !errors.Is(err, inventory.ErrLifecycleUnsupported) {
		t.Fatalf("versionless upsert: want ErrLifecycleUnsupported, got %v", err)
	}
	v1, err := s.CompareAndSetRunLifecycle(ctx, domain.RunLifecycle{RunID: "r1", SampleRunID: "s"}, 0)
	if err != nil || v1 != 1 {
		t.Fatalf("create r1: v=%d err=%v", v1, err)
	}
	if _, err := s.CompareAndSetRunLifecycle(ctx, domain.RunLifecycle{RunID: "r2", SampleRunID: "s", Finalized: true}, 0); err != nil {
		t.Fatalf("create r2: %v", err)
	}
	v2, err := s.CompareAndSetRunLifecycle(ctx, domain.RunLifecycle{RunID: "r1", SampleRunID: "s", Finalized: true}, v1)
	if err != nil || v2 != 2 {
		t.Fatalf("advance r1: v=%d err=%v", v2, err)
	}
	if _, err := s.CompareAndSetRunLifecycle(ctx, domain.RunLifecycle{RunID: "r1", SampleRunID: "s", GCEligible: true}, v1); !errors.Is(err, inventory.ErrLifecycleVersionConflict) {
		t.Fatalf("stale version: want ErrLifecycleVersionConflict, got %v", err)
	}
	lc, v, ok, err := s.GetRunLifecycleVersion(ctx, "r1")
	if err != nil || !ok || v != 2 || !lc.Finalized || lc.GCEligible {
		t.Fatalf("r1 after stale write: %+v v=%d ok=%v err=%v", lc, v, ok, err)
	}
	list, err := s.ListRunLifecyclesBySample(ctx, "s")
	if err != nil || len(list) != 2 {
		t.Fatalf("sample lifecycles = %d err=%v, want 2 separate Runs", len(list), err)
	}
}

// TX1-07 (5): a database stamped by a newer binary is refused before any mutation.
func TestPostgres_NewerSchemaRefused(t *testing.T) {
	dsn := pgSchemaDSN(t)
	s := openPG(t, dsn)
	_ = s.Close()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE ah_schema_meta SET value = '99' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("stamp newer schema: %v", err)
	}
	if _, err := inventory.NewPostgresStore(context.Background(), dsn); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("newer schema: want refusal, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ah_artifacts`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("mutation after refusal: n=%d err=%v", n, err)
	}
}

// TX1-06 / TX1-07 (7) / C-4: the J2 profile never falls back to memory or SQLite.
func TestOpenStoreProfile_NoFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for name, dsn := range map[string]string{
		"empty":       "",
		"memory":      "memory",
		"sqlite":      "sqlite:/tmp/ah-should-not-open.db",
		"unreachable": "postgres://ah@127.0.0.1:1/ah?connect_timeout=1",
	} {
		if s, _, err := inventory.OpenStoreProfile(ctx, inventory.ProfileJ2Postgres, dsn); err == nil {
			t.Fatalf("%s: j2-postgres opened %T, want error", name, s)
		}
	}
	if _, _, err := inventory.OpenStore("postgres://ah@127.0.0.1/ah"); err == nil {
		t.Fatal("OpenStore inferred the J2 store from a postgres DSN")
	}
	if _, _, err := inventory.OpenStoreProfile(ctx, "j2-unknown", "memory"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}
