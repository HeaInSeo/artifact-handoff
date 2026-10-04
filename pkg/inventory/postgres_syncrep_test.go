package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// TestPostgres_SyncRepCancelIsNotSuccess covers TX1 C-1 on a single primary: with a
// synchronous standby name that never connects, COMMIT waits for replication. Cancelling that
// wait makes the server reply "COMMIT" with a WARNING; the store must report
// ErrCommitOutcomeUnknown instead of success. It changes the server's
// synchronous_standby_names (ALTER SYSTEM, superuser) for its duration, so it must not run
// against a shared server; without the privilege it skips as NOT VERIFIED.
func TestPostgres_SyncRepCancelIsNotSuccess(t *testing.T) {
	dsn := pgSchemaDSN(t)
	admin, err := sql.Open("pgx", os.Getenv(pgTestDSNEnv))
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := admin.ExecContext(ctx, `ALTER SYSTEM SET synchronous_standby_names = 'ah_absent_standby'`); err != nil {
		t.Skipf("ALTER SYSTEM not permitted (%v): sync-commit cancellation test NOT VERIFIED", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`ALTER SYSTEM RESET synchronous_standby_names`)
		_, _ = admin.Exec(`SELECT pg_reload_conf()`)
	})
	if _, err := admin.ExecContext(ctx, `SELECT pg_reload_conf()`); err != nil {
		t.Fatalf("reload conf: %v", err)
	}

	s := openPG(t, dsn) // schema migration ran before sync replication is in effect
	waitFor(ctx, t, "synchronous_standby_names to apply", func() bool {
		var v string
		return admin.QueryRowContext(ctx, `SHOW synchronous_standby_names`).Scan(&v) == nil && v == "ah_absent_standby"
	})

	a := domain.Artifact{RunID: "run-sync", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o",
		ArtifactID: "art-sync", Digest: "sha256:sync"}
	errc := make(chan error, 1)
	go func() { errc <- s.PutArtifact(ctx, a) }()

	var pid int
	waitFor(ctx, t, "a backend waiting on SyncRep", func() bool {
		return admin.QueryRowContext(ctx,
			`SELECT pid FROM pg_stat_activity WHERE wait_event = 'SyncRep' LIMIT 1`).Scan(&pid) == nil
	})
	if _, err := admin.ExecContext(ctx, `SELECT pg_cancel_backend($1)`, pid); err != nil {
		t.Fatalf("cancel sync wait: %v", err)
	}
	err = <-errc
	if !errors.Is(err, inventory.ErrCommitOutcomeUnknown) {
		t.Fatalf("cancelled sync-commit wait must be ErrCommitOutcomeUnknown, got %v", err)
	}
	// The outcome really is uncertain from the caller's view: the row is committed locally.
	// The caller re-reads instead of trusting the failed write either way.
	_, ok, err := s.GetArtifact(ctx, "run-sync", "n", "a", "o")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	t.Logf("re-read after unknown outcome: present=%v (locally committed, not replicated)", ok)
}

func waitFor(ctx context.Context, t *testing.T, what string, cond func() bool) {
	t.Helper()
	for !cond() {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
