package session

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	t.Setenv("AGENTDECK_TEST_USE_HOME", "1")
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

func isWindowsSymlinkPrivilegeError(err error) bool {
	if err == nil || runtime.GOOS != "windows" {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "a required privilege is not held by the client") ||
		strings.Contains(msg, "privilege")
}

func skipIfWindowsSymlinkPrivilegeError(t *testing.T, err error) {
	t.Helper()
	if isWindowsSymlinkPrivilegeError(err) {
		t.Skipf("Windows symlink privilege unavailable: %v", err)
	}
}

func requireTestSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	err := os.Symlink(oldname, newname)
	skipIfWindowsSymlinkPrivilegeError(t, err)
	if err != nil {
		t.Fatalf("symlink %s -> %s: %v", newname, oldname, err)
	}
}

func commandContainsEnvAssignment(command, key, value string) bool {
	candidates := []string{
		shellSetEnvCommandForPowerShell(key, value, true),
		shellSetEnvCommandForPowerShell(key, value, false),
		"export " + key + "=" + value + ";",
		key + "=" + value,
	}
	for _, candidate := range candidates {
		if strings.Contains(command, candidate) {
			return true
		}
	}
	return false
}

func testNativePath(t *testing.T, elems ...string) string {
	t.Helper()
	parts := append([]string{t.TempDir()}, elems...)
	return filepath.Join(parts...)
}

func setTestHomeEnvForTest(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	vol := filepath.VolumeName(home)
	if vol == "" {
		return
	}
	t.Setenv("HOMEDRIVE", vol)
	rest := home[len(vol):]
	if rest == "" {
		rest = string(os.PathSeparator)
	}
	t.Setenv("HOMEPATH", rest)
}

func testLongRunningCommand(seconds int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`powershell.exe -NoLogo -NoProfile -Command "Start-Sleep -Seconds %d"`, seconds)
	}
	return fmt.Sprintf("sleep %d", seconds)
}
