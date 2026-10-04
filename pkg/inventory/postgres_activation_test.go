package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// rawExec runs SQL in the test schema directly, standing in for restore tooling.
func rawExec(t *testing.T, dsn, stmt string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

func artifactCount(t *testing.T, dsn string) int {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ah_artifacts`).Scan(&n); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	return n
}

func requireHold(t *testing.T, s *inventory.PostgresStore, what string) {
	t.Helper()
	ctx := context.Background()
	err := s.PutArtifact(ctx, domain.Artifact{RunID: "run-new", ProducerNodeID: "n", ProducerAttemptID: "a",
		OutputName: "o", ArtifactID: "art-new", Digest: "sha256:new"})
	if !errors.Is(err, inventory.ErrRestoreActivationHold) {
		t.Fatalf("%s: put must be refused with ErrRestoreActivationHold, got %v", what, err)
	}
	err = s.RecordNodeTerminal(ctx, domain.NodeTerminalRecord{RunID: "run-new", NodeID: "n", AttemptID: "a", TerminalState: "Succeeded"})
	if !errors.Is(err, inventory.ErrRestoreActivationHold) {
		t.Fatalf("%s: terminal must be refused with ErrRestoreActivationHold, got %v", what, err)
	}
}

// TX1-07 (8): a store whose activation belongs to another cluster/timeline/database (a
// restore or promoted standby) refuses every mutation; reads keep working; raising the epoch
// or reopening does not lift the hold.
func TestPostgres_RestoreActivationHold(t *testing.T) {
	dsn := pgSchemaDSN(t)
	ctx := context.Background()
	s := openPG(t, dsn)
	a := domain.Artifact{RunID: "run-1", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o",
		ArtifactID: "art-1", Digest: "sha256:1"}
	if err := s.PutArtifact(ctx, a); err != nil {
		t.Fatalf("clean bootstrap must be writable: %v", err)
	}

	// The record now claims an earlier timeline, as after point-in-time restore or promotion.
	rawExec(t, dsn, `UPDATE ah_store_activation SET timeline_id = timeline_id + 1`)
	requireHold(t, s, "identity mismatch")
	if _, ok, err := s.GetArtifact(ctx, "run-1", "n", "a", "o"); err != nil || !ok {
		t.Fatalf("reads must keep working under hold: ok=%v err=%v", ok, err)
	}
	if n := artifactCount(t, dsn); n != 1 {
		t.Fatalf("hold must write nothing: %d artifacts", n)
	}

	rawExec(t, dsn, `UPDATE ah_store_activation SET epoch = epoch + 1`)
	requireHold(t, s, "epoch raised without evidence")

	_ = s.Close()
	requireHold(t, openPG(t, dsn), "reopen")
	if n := artifactCount(t, dsn); n != 1 {
		t.Fatalf("hold must write nothing: %d artifacts", n)
	}
}

// TX1-07 (5): store tables without this store's schema stamp are a foreign schema: open is
// refused and nothing is created or written.
func TestPostgres_ForeignSchemaRefused(t *testing.T) {
	dsn := pgSchemaDSN(t)
	rawExec(t, dsn, `CREATE TABLE ah_artifacts (key text PRIMARY KEY, note text)`)
	rawExec(t, dsn, `INSERT INTO ah_artifacts VALUES ('k', 'foreign row')`)
	if s, err := inventory.NewPostgresStore(context.Background(), dsn); err == nil {
		_ = s.Close()
		t.Fatal("open over a foreign schema must be refused")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	defer func() { _ = db.Close() }()
	var stamped bool
	if err := db.QueryRow(`SELECT to_regclass('ah_schema_meta') IS NOT NULL`).Scan(&stamped); err != nil || stamped {
		t.Fatalf("refused open must create nothing: stamped=%v err=%v", stamped, err)
	}
	var note string
	if err := db.QueryRow(`SELECT note FROM ah_artifacts WHERE key = 'k'`).Scan(&note); err != nil || note != "foreign row" {
		t.Fatalf("foreign row must be untouched: %q err=%v", note, err)
	}
}

// A store holding data without an activation record (e.g. a partial restore) is not a clean
// bootstrap: it opens held, not active.
func TestPostgres_DataWithoutActivationIsHeld(t *testing.T) {
	dsn := pgSchemaDSN(t)
	s := openPG(t, dsn)
	if err := s.PutArtifact(context.Background(), domain.Artifact{RunID: "run-1", ProducerNodeID: "n",
		ProducerAttemptID: "a", OutputName: "o", ArtifactID: "art-1", Digest: "sha256:1"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	_ = s.Close()
	rawExec(t, dsn, `DELETE FROM ah_store_activation`)
	requireHold(t, openPG(t, dsn), "data without activation")
}
