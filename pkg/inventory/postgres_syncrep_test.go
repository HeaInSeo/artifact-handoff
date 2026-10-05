package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
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
//
// The setting is server-wide: while it is in effect, commits from other test packages running
// in parallel against the same server (e.g. storetest) wait on SyncRep too, until the cleanup
// resets it. The store's connections therefore carry a unique application_name, and the test
// cancels only its own waiting backend; cancelling another package's backend would leave this
// store's COMMIT waiting and prove nothing.
func TestPostgres_SyncRepCancelIsNotSuccess(t *testing.T) {
	dsn, appName := withApplicationName(t, pgSchemaDSN(t))
	admin, err := sql.Open("pgx", os.Getenv(pgTestDSNEnv))
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s := openPG(t, dsn) // migrate before synchronous replication is in effect

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

	waitFor(ctx, t, "synchronous_standby_names to apply", func() bool {
		var v string
		return admin.QueryRowContext(ctx, `SHOW synchronous_standby_names`).Scan(&v) == nil && v == "ah_absent_standby"
	})

	a := domain.Artifact{RunID: "run-sync", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o",
		ArtifactID: "art-sync", Digest: "sha256:sync"}
	errc := make(chan error, 1)
	go func() { errc <- s.PutArtifact(ctx, a) }()

	var pid int
	waitFor(ctx, t, "this store's backend waiting on SyncRep", func() bool {
		return admin.QueryRowContext(ctx,
			`SELECT pid FROM pg_stat_activity WHERE wait_event = 'SyncRep' AND application_name = $1`,
			appName).Scan(&pid) == nil
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

// withApplicationName returns dsn with a unique application_name, so the test can find the
// store's own backends in pg_stat_activity.
func withApplicationName(t *testing.T, dsn string) (tagged, appName string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	appName = fmt.Sprintf("ah_syncrep_%d", time.Now().UnixNano())
	q := u.Query()
	q.Set("application_name", appName)
	u.RawQuery = q.Encode()
	return u.String(), appName
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
