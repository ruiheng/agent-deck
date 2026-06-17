//go:build windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

// conductorBaseMu serializes conductor base mutations (setup, meta writes, and
// dir migration) WITHIN this process; the sibling advisory lock serializes them
// ACROSS processes.
var conductorBaseMu sync.Mutex

type conductorBaseLock struct {
	file *os.File
}

func (l *conductorBaseLock) release() {
	if l == nil {
		return
	}
	if l.file != nil {
		var overlapped windows.Overlapped
		_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &overlapped)
		_ = l.file.Close()
	}
	conductorBaseMu.Unlock()
}

func acquireConductorBaseLock() (*conductorBaseLock, error) {
	conductorBaseMu.Lock()
	locks, err := resolveLocksDirForSpawnLock()
	if err != nil {
		conductorBaseMu.Unlock()
		return nil, fmt.Errorf("resolve locks dir for conductor base lock: %w", err)
	}
	if err := os.MkdirAll(locks, 0o700); err != nil {
		conductorBaseMu.Unlock()
		return nil, fmt.Errorf("create locks dir for conductor base lock: %w", err)
	}
	lockPath := filepath.Join(locks, "conductor-base.lock")
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		conductorBaseMu.Unlock()
		return nil, fmt.Errorf("open conductor base lock %q: %w", lockPath, err)
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		_ = f.Close()
		conductorBaseMu.Unlock()
		return nil, fmt.Errorf("lock conductor base: %w", err)
	}
	return &conductorBaseLock{file: f}, nil
}
