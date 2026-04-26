package session

import (
	"os"
	"path/filepath"
	"testing"
)

// isolatedHomeDir creates a fresh temp HOME with the agent-deck and Claude
// directories pre-created, then clears cached config state for the test.
func isolatedHomeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, sub := range []string{".agent-deck", ".agent-deck/hooks", ".claude/projects"} {
		if err := os.MkdirAll(filepath.Join(home, sub), 0o755); err != nil {
			t.Fatalf("isolatedHomeDir mkdir %s: %v", sub, err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	vol := filepath.VolumeName(home)
	if vol != "" {
		t.Setenv("HOMEDRIVE", vol)
		rest := home[len(vol):]
		if rest == "" {
			rest = string(os.PathSeparator)
		}
		t.Setenv("HOMEPATH", rest)
	}
	ClearUserConfigCache()
	t.Cleanup(func() { ClearUserConfigCache() })
	return home
}
