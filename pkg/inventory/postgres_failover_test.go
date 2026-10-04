package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// pgStandbyDSNEnv names a streaming-replication standby of the AH_TEST_POSTGRES_DSN primary.
// The failover test promotes it, so the pair is single-use. Without it the test skips as NOT
// VERIFIED.
const pgStandbyDSNEnv = "AH_TEST_POSTGRES_STANDBY_DSN"

// TestPostgres_PromotedStandbyIsHeld covers the two-member post-failover state (TX1 C-1,
// TX1-04): records committed on the primary survive on the promoted standby and stay
// readable, but the promoted member is a new timeline, so every mutation is refused with
// ErrRestoreActivationHold until activation is re-established with old-primary fencing
// evidence. The old primary is not fenced by the store; that remains an external gate.
func TestPostgres_PromotedStandbyIsHeld(t *testing.T) {
	standbyBase := os.Getenv(pgStandbyDSNEnv)
	if standbyBase == "" {
		t.Skipf("%s not set: two-member failover test NOT VERIFIED", pgStandbyDSNEnv)
	}
	dsn := pgSchemaDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	primary := openPG(t, dsn)
	a := domain.Artifact{RunID: "run-fo", ProducerNodeID: "n", ProducerAttemptID: "a", OutputName: "o",
		ArtifactID: "art-fo", Digest: "sha256:fo"}
	if err := primary.PutArtifact(ctx, a); err != nil {
		t.Fatalf("put on primary: %v", err)
	}

	admin, err := sql.Open("pgx", os.Getenv(pgTestDSNEnv))
	if err != nil {
		t.Fatalf("open primary admin: %v", err)
	}
	defer func() { _ = admin.Close() }()
	standby, err := sql.Open("pgx", standbyBase)
	if err != nil {
		t.Fatalf("open standby admin: %v", err)
	}
	defer func() { _ = standby.Close() }()
	var target string
	if err := admin.QueryRowContext(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&target); err != nil {
		t.Fatalf("primary lsn: %v", err)
	}
	waitFor(ctx, t, "standby replay to reach the primary LSN", func() bool {
		var caught bool
		return standby.QueryRowContext(ctx, `SELECT pg_last_wal_replay_lsn() >= $1::pg_lsn`, target).Scan(&caught) == nil && caught
	})
	var promoted bool
	if err := standby.QueryRowContext(ctx, `SELECT pg_promote(true, 60)`).Scan(&promoted); err != nil || !promoted {
		t.Fatalf("promote standby: promoted=%v err=%v", promoted, err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	su, err := url.Parse(standbyBase)
	if err != nil {
		t.Fatalf("parse standby dsn: %v", err)
	}
	q := su.Query()
	q.Set("search_path", u.Query().Get("search_path"))
	su.RawQuery = q.Encode()
	s := openPG(t, su.String())

	got, ok, err := s.GetArtifact(ctx, "run-fo", "n", "a", "o")
	if err != nil || !ok || got.Digest != a.Digest {
		t.Fatalf("committed record must survive failover: ok=%v digest=%q err=%v", ok, got.Digest, err)
	}
	err = s.PutArtifact(ctx, domain.Artifact{RunID: "run-fo2", ProducerNodeID: "n", ProducerAttemptID: "a",
		OutputName: "o", ArtifactID: "art-fo2", Digest: "sha256:fo2"})
	if !errors.Is(err, inventory.ErrRestoreActivationHold) {
		t.Fatalf("promoted standby must refuse mutation with ErrRestoreActivationHold, got %v", err)
	}
	if err := s.PutArtifact(ctx, a); !errors.Is(err, inventory.ErrRestoreActivationHold) {
		t.Fatalf("even an idempotent replay must be refused on the held member, got %v", err)
	}
}
