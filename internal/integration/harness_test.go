package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// nonAlphanumRE matches characters that are not alphanumeric or dash.
// Uses dashes because tmux sanitizeName keeps only [a-zA-Z0-9-].
var nonAlphanumRE = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// TmuxHarness manages tmux sessions for integration tests with automatic cleanup.
// Sessions are created with a test-unique prefix and torn down via t.Cleanup.
type TmuxHarness struct {
	t        *testing.T
	sessions []*session.Instance
	prefix   string
}

// NewTmuxHarness creates a harness that auto-cleans tmux sessions when the test ends.
// Skips the test if no tmux server is available.
func NewTmuxHarness(t *testing.T) *TmuxHarness {
	t.Helper()
	skipIfNoTmuxServer(t)

	h := &TmuxHarness{
		t:      t,
		prefix: fmt.Sprintf("inttest-%s-", sanitizeName(t.Name())),
	}
	t.Cleanup(h.cleanup)
	return h
}

// CreateSession creates a session.Instance with the harness prefix prepended to the title.
// The session is tracked for automatic cleanup.
func (h *TmuxHarness) CreateSession(title, projectPath string) *session.Instance {
	h.t.Helper()
	inst := session.NewInstance(h.prefix+title, projectPath)
	h.sessions = append(h.sessions, inst)
	return inst
}

func testWorkDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return os.TempDir()
	}
	return "/tmp"
}

func longRunningCommand() string {
	if runtime.GOOS == "windows" {
		return `powershell.exe -NoLogo -NoProfile -Command "Start-Sleep -Seconds 60"`
	}
	return "sleep 60"
}

func outputThenSleepCommand(marker string) string {
	if runtime.GOOS == "windows" {
		return `powershell.exe -NoLogo -NoProfile -Command "Write-Output '` + powerShellSingleQuote(marker) + `'; Start-Sleep -Seconds 60"`
	}
	return "echo " + shellSafeWord(marker) + " && sleep 60"
}

func echoServerCommand() string {
	if runtime.GOOS == "windows" {
		return `powershell.exe -NoLogo -NoProfile -Command "while (($line = [Console]::In.ReadLine()) -ne $null) { [Console]::Out.WriteLine($line) }"`
	}
	return "cat"
}

func shellEchoCommand(marker string) string {
	if runtime.GOOS == "windows" {
		return `echo ` + marker
	}
	return "echo " + shellSafeWord(marker)
}

func chunkedMarkerCommand(marker string) string {
	if runtime.GOOS != "windows" {
		var lines []string
		lines = append(lines, "CHUNK-START")
		for i := 0; i < 55; i++ {
			lines = append(lines, fmt.Sprintf("LINE-%03d-%s", i, strings.Repeat("Z", 70)))
		}
		lines = append(lines, marker)
		return strings.Join(lines, "\n")
	}

	padding := strings.Repeat("Z", 4300)
	return `powershell.exe -NoLogo -NoProfile -Command "$x='` + padding + `'; Write-Output '` + powerShellSingleQuote(marker) + `'"`
}

func codexSimulationCommand(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		scriptPath := filepath.Join(dir, "fake-codex.sh")
		script := `#!/bin/sh
sleep 3
printf "codex> "
while IFS= read -r line; do
  echo "received: $line"
  printf "codex> "
done
`
		requireNoError(t, writeExecutable(scriptPath, []byte(script)))
		return scriptPath
	}

	scriptPath := filepath.Join(dir, "fake-codex.ps1")
	script := `Start-Sleep -Seconds 3
[Console]::Out.Write("codex> ")
while (($line = [Console]::In.ReadLine()) -ne $null) {
  [Console]::Out.WriteLine("received: $line")
  [Console]::Out.Write("codex> ")
}
`
	requireNoError(t, writeExecutable(scriptPath, []byte(script)))
	return `powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "` + strings.ReplaceAll(scriptPath, `"`, `""`) + `"`
}

func writeExecutable(path string, data []byte) error {
	return os.WriteFile(path, data, 0o755)
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func powerShellSingleQuote(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}

func shellSafeWord(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// CreateSessionWithTool creates a session.Instance with a specific tool and the harness prefix.
func (h *TmuxHarness) CreateSessionWithTool(title, projectPath, tool string) *session.Instance {
	h.t.Helper()
	inst := session.NewInstanceWithTool(h.prefix+title, projectPath, tool)
	h.sessions = append(h.sessions, inst)
	return inst
}

// SessionCount returns the number of sessions tracked by this harness.
func (h *TmuxHarness) SessionCount() int {
	return len(h.sessions)
}

// cleanup kills all tracked sessions in reverse order. Best-effort: errors are ignored.
func (h *TmuxHarness) cleanup() {
	for i := len(h.sessions) - 1; i >= 0; i-- {
		_ = h.sessions[i].Kill()
	}
}

// sanitizeName replaces slashes and non-alphanumeric characters with dashes
// for safe tmux session names. Matches tmux's own sanitization behavior.
func sanitizeName(name string) string {
	return nonAlphanumRE.ReplaceAllString(name, "-")
}
