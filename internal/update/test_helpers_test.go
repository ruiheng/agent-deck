package update

import (
	"path/filepath"
	"strings"
	"testing"
)

func setUpdateTestHome(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	if vol := filepath.VolumeName(dir); vol != "" {
		t.Setenv("HOMEDRIVE", vol)
		if rest := strings.TrimPrefix(dir, vol); rest != "" {
			t.Setenv("HOMEPATH", rest)
		}
	}
	t.Setenv("AGENTDECK_TEST_USE_HOME", "1")
	t.Setenv("CODEX_HOME", filepath.Join(dir, ".codex"))
	return dir
}
