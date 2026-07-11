package ui

import (
	"log/slog"
	"sync"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/logging"
	"github.com/asheshgoplani/agent-deck/internal/statedb"
)

var watcherLog = logging.ForComponent(logging.CompStorage)

// StorageWatcher monitors the SQLite database for external changes
// by polling the metadata.last_modified timestamp.
// Replaces the previous fsnotify-based watcher which had reliability issues
// on certain filesystems (9p, NFS, WSL).
type StorageWatcher struct {
	db        *statedb.StateDB
	reloadCh  chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once

	// lastModified tracks the last seen modification timestamp
	lastModified int64
	modMu        sync.RWMutex

	// ignoreModified is the exact metadata timestamp produced by this TUI's
	// latest save. Only that timestamp is ignored; later external writes must
	// still trigger a reload.
	ignoreModified int64
	saveInProgress bool
	saveMu         sync.RWMutex
}

// pollInterval is how often we check for external changes.
const pollInterval = 500 * time.Millisecond

// NewStorageWatcher creates a watcher that polls the SQLite metadata for changes.
func NewStorageWatcher(db *statedb.StateDB) (*StorageWatcher, error) {
	if db == nil {
		return nil, nil
	}

	// Get initial modification timestamp
	lastMod, _ := db.LastModified()

	return &StorageWatcher{
		db:           db,
		lastModified: lastMod,
		reloadCh:     make(chan struct{}, 1), // Buffered to prevent blocking
		closeCh:      make(chan struct{}),
	}, nil
}

// Start begins polling for changes (non-blocking).
func (sw *StorageWatcher) Start() {
	go sw.pollLoop()
}

// pollLoop checks the metadata timestamp periodically.
func (sw *StorageWatcher) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sw.closeCh:
			return
		case <-ticker.C:
			sw.checkAndNotify()
		}
	}
}

// checkAndNotify checks if the metadata timestamp has changed and notifies if so.
func (sw *StorageWatcher) checkAndNotify() {
	ts, err := sw.db.LastModified()
	if err != nil {
		watcherLog.Debug("watcher_poll_failed", slog.String("error", err.Error()))
		return
	}

	sw.saveMu.RLock()
	ignoreModified := sw.ignoreModified
	saveInProgress := sw.saveInProgress
	sw.saveMu.RUnlock()

	if saveInProgress {
		return
	}

	sw.modMu.Lock()
	changed := ts > sw.lastModified
	if changed {
		sw.lastModified = ts
	}
	sw.modMu.Unlock()

	if !changed {
		return
	}

	if ts == ignoreModified {
		watcherLog.Debug("watcher_ignoring_own_save")
		return
	}

	watcherLog.Debug("watcher_db_changed", slog.Int64("timestamp", ts))

	// Non-blocking send (drop if channel full)
	select {
	case sw.reloadCh <- struct{}{}:
	default:
		watcherLog.Debug("watcher_reload_channel_full")
	}
}

// ReloadChannel returns the channel that signals when reload is needed.
func (sw *StorageWatcher) ReloadChannel() <-chan struct{} {
	return sw.reloadCh
}

// BeginSave pauses reload classification while this process is writing. The
// matching FinishSave call records the exact timestamp to ignore.
func (sw *StorageWatcher) BeginSave() {
	sw.saveMu.Lock()
	sw.saveInProgress = true
	sw.saveMu.Unlock()
}

// FinishSave records the metadata timestamp produced by this TUI's own save,
// so the watcher can ignore exactly that write without swallowing later
// external CLI/delegate-task changes.
func (sw *StorageWatcher) FinishSave(ts int64) {
	sw.saveMu.Lock()
	if ts != 0 {
		sw.ignoreModified = ts
	}
	sw.saveInProgress = false
	sw.saveMu.Unlock()
}

// TriggerReload sends a reload signal.
// Used as a manual trigger for reload (e.g., after CLI command changes).
func (sw *StorageWatcher) TriggerReload() {
	// Update lastModified to current to prevent re-triggering
	if ts, err := sw.db.LastModified(); err == nil {
		sw.modMu.Lock()
		sw.lastModified = ts
		sw.modMu.Unlock()
	}
	// Non-blocking send to reload channel
	select {
	case sw.reloadCh <- struct{}{}:
		watcherLog.Debug("watcher_trigger_reload")
	default:
		watcherLog.Debug("watcher_trigger_reload_channel_full")
	}
}

// Warning returns empty string. SQLite polling works on all filesystems.
func (sw *StorageWatcher) Warning() string {
	return ""
}

// Close stops the watcher and releases resources. Safe to call multiple times.
func (sw *StorageWatcher) Close() error {
	sw.closeOnce.Do(func() {
		close(sw.closeCh)
	})
	return nil
}
