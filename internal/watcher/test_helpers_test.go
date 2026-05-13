package watcher

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/testutil"
)

func setWatcherTestHome(t *testing.T) string {
	t.Helper()
	cleanup := testutil.IsolateHome("agentdeck-watcher-home-")
	t.Cleanup(cleanup)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return home
}

func requireWatcherSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrInvalid) ||
			strings.Contains(err.Error(), "privilege")) {
			t.Skipf("Windows symlink privilege unavailable: %v", err)
		}
		t.Fatalf("Symlink: %v", err)
	}
}
