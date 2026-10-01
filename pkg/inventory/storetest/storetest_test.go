package storetest_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory/storetest"
)

// MemoryStore has no durable state: Reopen stays nil and the Reopen case reports
// NOT IMPLEMENTED (skip).
func TestMemoryStoreContract(t *testing.T) {
	storetest.Run(t, storetest.Harness{
		New: func(*testing.T) inventory.Store { return inventory.NewMemoryStore() },
	})
}

// SQLiteStore runs the same contract, with Reopen closing the store and opening a
// new SQLiteStore on the same database file.
func TestSQLiteStoreContract(t *testing.T) {
	var mu sync.Mutex
	paths := map[inventory.Store]string{}
	open := func(t *testing.T, path string) inventory.Store {
		t.Helper()
		s, err := inventory.NewSQLiteStore(path)
		if err != nil {
			t.Fatalf("NewSQLiteStore(%s): %v", path, err)
		}
		t.Cleanup(func() { _ = s.Close() })
		mu.Lock()
		paths[s] = path
		mu.Unlock()
		return s
	}
	storetest.Run(t, storetest.Harness{
		New: func(t *testing.T) inventory.Store { return open(t, filepath.Join(t.TempDir(), "inventory.db")) },
		Reopen: func(t *testing.T, s inventory.Store) inventory.Store {
			mu.Lock()
			path := paths[s]
			mu.Unlock()
			if err := s.(*inventory.SQLiteStore).Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			return open(t, path)
		},
	})
}
