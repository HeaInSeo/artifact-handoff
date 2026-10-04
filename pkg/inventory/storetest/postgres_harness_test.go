package storetest_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory/storetest"
)

// TestPostgresStoreContract runs the Store contract against the J2 PostgreSQL store, each
// Store in a fresh schema. Lifecycle writes go through the fenced CAS seam because the J2
// store refuses the versionless upsert. Legacy (pre-F4) seeding is not wired: those cases
// report NOT IMPLEMENTED. Without AH_TEST_POSTGRES_DSN the test skips as NOT VERIFIED.
func TestPostgresStoreContract(t *testing.T) {
	base := os.Getenv("AH_TEST_POSTGRES_DSN")
	if base == "" {
		t.Skip("AH_TEST_POSTGRES_DSN not set: PostgreSQL contract NOT VERIFIED")
	}
	dsns := map[inventory.Store]string{}
	storetest.Run(t, storetest.Harness{
		New: func(t *testing.T) inventory.Store {
			dsn := schemaDSN(t, base)
			s := openPostgres(t, dsn)
			dsns[s] = dsn
			return s
		},
		Reopen: func(t *testing.T, s inventory.Store) inventory.Store {
			dsn := dsns[s]
			_ = s.(*inventory.PostgresStore).Close()
			r := openPostgres(t, dsn)
			dsns[r] = dsn
			return r
		},
		SetLifecycle: func(ctx context.Context, s inventory.Store, lc domain.RunLifecycle) error {
			ps := s.(*inventory.PostgresStore)
			_, version, _, err := ps.GetRunLifecycleVersion(ctx, lc.RunID)
			if err != nil {
				return err
			}
			_, err = ps.CompareAndSetRunLifecycle(ctx, lc, version)
			return err
		},
	})
}

func openPostgres(t *testing.T, dsn string) *inventory.PostgresStore {
	t.Helper()
	s, err := inventory.NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func schemaDSN(t *testing.T, base string) string {
	t.Helper()
	db, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer func() { _ = db.Close() }()
	schema := fmt.Sprintf("ah_c_%d", time.Now().UnixNano())
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if admin, err := sql.Open("pgx", base); err == nil {
			_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
			_ = admin.Close()
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
