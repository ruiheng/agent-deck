package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/statedb"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *statedb.StateDB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := statedb.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNewStorageWatcher(t *testing.T) {
	db := newTestDB(t)
	watcher, err := NewStorageWatcher(db)
	require.NoError(t, err)
	require.NotNil(t, watcher)
	defer watcher.Close()
}

func TestStorageWatcher_DetectsChanges(t *testing.T) {
	db := newTestDB(t)
	watcher, err := NewStorageWatcher(db)
	require.NoError(t, err)
	defer watcher.Close()

	watcher.Start()

	// Simulate an external change (another instance touching the metadata)
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, db.Touch())

	// Should receive reload signal within the poll interval
	select {
	case <-watcher.ReloadChannel():
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Expected reload signal but got timeout")
	}
}

func TestStorageWatcher_FinishSaveIgnoresOwnChanges(t *testing.T) {
	db := newTestDB(t)
	watcher, err := NewStorageWatcher(db)
	require.NoError(t, err)
	defer watcher.Close()

	watcher.Start()

	// Touch metadata (this simulates TUI's own save via storage.SaveWithGroups)
	time.Sleep(10 * time.Millisecond)
	watcher.BeginSave()
	ts, err := db.TouchNow()
	require.NoError(t, err)
	watcher.FinishSave(ts)

	// Should NOT receive reload signal for the exact timestamp produced by our save.
	select {
	case <-watcher.ReloadChannel():
		t.Fatal("Should not receive reload signal for TUI's own save")
	case <-time.After(2 * pollInterval):
		// Success: no reload signal received
	}
}

func TestStorageWatcher_ExternalChangesStillDetected(t *testing.T) {
	db := newTestDB(t)
	watcher, err := NewStorageWatcher(db)
	require.NoError(t, err)
	defer watcher.Close()

	watcher.Start()

	// Record our own save timestamp.
	watcher.BeginSave()
	ts, err := db.TouchNow()
	require.NoError(t, err)
	watcher.FinishSave(ts)

	// A later external change must not be swallowed just because it happens
	// immediately after this TUI's own save.
	require.NoError(t, db.Touch())

	select {
	case <-watcher.ReloadChannel():
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Expected reload signal for external change but got timeout")
	}
}

func TestStorageWatcher_SaveInProgressDoesNotSwallowLaterExternalChange(t *testing.T) {
	db := newTestDB(t)
	watcher, err := NewStorageWatcher(db)
	require.NoError(t, err)
	defer watcher.Close()

	watcher.BeginSave()
	ownTS, err := db.TouchNow()
	require.NoError(t, err)

	// A poll in the tiny window after TouchNow but before FinishSave must not
	// classify our own write as external.
	watcher.checkAndNotify()
	select {
	case <-watcher.ReloadChannel():
		t.Fatal("Should not receive reload while own save is in progress")
	default:
	}

	watcher.FinishSave(ownTS)
	watcher.checkAndNotify()
	select {
	case <-watcher.ReloadChannel():
		t.Fatal("Should not receive reload for the completed own save")
	default:
	}

	require.NoError(t, db.Touch())
	watcher.checkAndNotify()
	select {
	case <-watcher.ReloadChannel():
		// Success
	default:
		t.Fatal("Expected reload signal for later external change")
	}
}

// TestStorageWatcher_CrossProfileIsolation verifies that separate SQLite databases
// for different profiles are naturally isolated (each has its own metadata).
func TestStorageWatcher_CrossProfileIsolation(t *testing.T) {
	db1 := newTestDB(t)
	db2 := newTestDB(t)

	// Create watcher for db1 only
	watcher1, err := NewStorageWatcher(db1)
	require.NoError(t, err)
	defer watcher1.Close()
	watcher1.Start()

	// Touch db2's metadata (simulating another profile saving)
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, db2.Touch())

	// Watcher1 should NOT fire (it's watching a different database)
	select {
	case <-watcher1.ReloadChannel():
		t.Fatal("CRITICAL BUG: Watcher1 fired when db2 was modified!")
	case <-time.After(3 * time.Second):
		// Success: isolated correctly
	}

	// Watcher1 SHOULD fire when its own database changes
	require.NoError(t, db1.Touch())

	select {
	case <-watcher1.ReloadChannel():
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Watcher1 should have detected change to its own database")
	}
}

func TestStorageWatcher_NilDB(t *testing.T) {
	watcher, err := NewStorageWatcher(nil)
	require.NoError(t, err)
	require.Nil(t, watcher)
}
