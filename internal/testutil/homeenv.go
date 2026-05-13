package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsolateHome redirects home-directory lookups to a temporary directory for a
// whole test package. It also enables Agent Deck's Windows test-home override,
// because os.UserHomeDir ignores HOME on Windows and reads USERPROFILE instead.
func IsolateHome(prefix string) func() {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		panic(fmt.Sprintf("mktemp test home: %v", err))
	}

	orig := map[string]struct {
		value string
		ok    bool
	}{}
	for _, key := range []string{"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "AGENTDECK_TEST_USE_HOME", "CODEX_HOME"} {
		value, ok := os.LookupEnv(key)
		orig[key] = struct {
			value string
			ok    bool
		}{value: value, ok: ok}
	}

	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("USERPROFILE", dir)
	if vol := filepath.VolumeName(dir); vol != "" {
		_ = os.Setenv("HOMEDRIVE", vol)
		if rest := strings.TrimPrefix(dir, vol); rest != "" {
			_ = os.Setenv("HOMEPATH", rest)
		}
	}
	_ = os.Setenv("AGENTDECK_TEST_USE_HOME", "1")
	_ = os.Setenv("CODEX_HOME", filepath.Join(dir, ".codex"))

	return func() {
		for key, old := range orig {
			restoreEnv(key, old.value, old.ok)
		}
		_ = os.RemoveAll(dir)
	}
}
