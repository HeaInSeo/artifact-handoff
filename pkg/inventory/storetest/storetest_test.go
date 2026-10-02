package storetest_test

import (
	"testing"

	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory/storetest"
)

// MemoryStore has no durable state and no pre-F4 rows: Reopen and SeedLegacy stay
// nil, and the Reopen and Legacy cases report NOT IMPLEMENTED (skip).
func TestMemoryStoreContract(t *testing.T) {
	storetest.Run(t, storetest.Harness{
		New: func(*testing.T) inventory.Store { return inventory.NewMemoryStore() },
	})
}

// SQLiteStore runs the same contract, with Reopen closing the store and opening a
// new SQLiteStore on the same database file, and legacy (pre-F4) rows seeded with
// raw SQL on that file.
func TestSQLiteStoreContract(t *testing.T) {
	storetest.Run(t, storetest.SQLiteHarness())
}
