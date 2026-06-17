package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HomeIsolationMarkerEnv is set during HOME+XDG isolation. Runtime guards (and
// the pathsafety guard test) read this to confirm a test context is sandboxed.
const HomeIsolationMarkerEnv = "AGENT_DECK_TEST_HOME_ISOLATED"

// IsolateHome makes it safe for tests to resolve and write agent-deck runtime
// paths (~/.agent-deck/config.json, profiles/<p>/state.db, worker-scratch,
// logs, hooks) without ever touching the developer's real home directory.
//
// The optional prefix keeps compatibility with older package tests that named
// their temp home; no argument uses the upstream default.
//
// It sets:
//   - HOME and USERPROFILE -> <tempdir>
//   - HOMEDRIVE/HOMEPATH   -> split Windows home path when applicable
//   - XDG_* base dirs      -> cleared, so they resolve under HOME
//   - AGENTDECK_PROFILE    -> _test
//   - AGENTDECK_TEST_USE_HOME -> 1, so Windows helpers honor HOME
//   - CODEX_HOME           -> <tempdir>/.codex
//   - AGENT_DECK_TEST_HOME_ISOLATED -> 1
//
// Returns a cleanup function that removes the temp dir and restores the
// original env so the parent process is not permanently altered.
func IsolateHome(prefix ...string) func() {
	type snap struct {
		key string
		val string
		had bool
	}

	keys := []string{
		"HOME",
		"USERPROFILE",
		"HOMEDRIVE",
		"HOMEPATH",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_CACHE_HOME",
		"XDG_STATE_HOME",
		"AGENTDECK_PROFILE",
		"AGENTDECK_TEST_USE_HOME",
		"CODEX_HOME",
		HomeIsolationMarkerEnv,
	}

	snaps := make([]snap, 0, len(keys))
	for _, k := range keys {
		v, had := os.LookupEnv(k)
		snaps = append(snaps, snap{key: k, val: v, had: had})
	}

	tempPrefix := "ad-home-"
	if len(prefix) > 0 && prefix[0] != "" {
		tempPrefix = prefix[0]
	}
	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		// We must never fall back to the real HOME. A PID-keyed path under the
		// OS temp dir is still safely off the real home.
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("agent-deck-test-home-fallback-%d", os.Getpid()))
		_ = os.MkdirAll(dir, 0o700)
	}

	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("USERPROFILE", dir)
	if vol := filepath.VolumeName(dir); vol != "" {
		_ = os.Setenv("HOMEDRIVE", vol)
		if rest := strings.TrimPrefix(dir, vol); rest != "" {
			_ = os.Setenv("HOMEPATH", rest)
		}
	}
	// Clear (do NOT pin) the XDG base dirs so they fall back to $HOME/*.
	_ = os.Unsetenv("XDG_CONFIG_HOME")
	_ = os.Unsetenv("XDG_DATA_HOME")
	_ = os.Unsetenv("XDG_CACHE_HOME")
	_ = os.Unsetenv("XDG_STATE_HOME")
	_ = os.Setenv("AGENTDECK_PROFILE", "_test")
	_ = os.Setenv("AGENTDECK_TEST_USE_HOME", "1")
	_ = os.Setenv("CODEX_HOME", filepath.Join(dir, ".codex"))
	_ = os.Setenv(HomeIsolationMarkerEnv, "1")

	return func() {
		for _, s := range snaps {
			restoreEnv(s.key, s.val, s.had)
		}
		_ = os.RemoveAll(dir)
	}
}
