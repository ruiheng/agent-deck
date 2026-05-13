package ui

import (
	"path/filepath"
	"strings"
	"testing"
)

func setUITestHome(t *testing.T, home string) {
	t.Helper()

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if vol := filepath.VolumeName(home); vol != "" {
		t.Setenv("HOMEDRIVE", vol)
		if rest := strings.TrimPrefix(home, vol); rest != "" {
			t.Setenv("HOMEPATH", rest)
		}
	}
}

func cleanupHomeStorage(t *testing.T, home *Home) {
	t.Helper()
	if home != nil && home.storage != nil {
		t.Cleanup(func() { _ = home.storage.Close() })
	}
}
