package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestTUIInput_ArrowKeysNavigate launches the real agent-deck binary in a tmux
// session and verifies that arrow key escape sequences are interpreted (cursor
// moves) rather than displayed as raw text.
//
// This is a regression test for #539 where tea.WithInput(CSIuReader) stripped
// the *os.File interface from stdin, preventing Bubble Tea from setting raw
// terminal mode. The result was arrow keys appearing as "^[[A" text.
func TestTUIInput_ArrowKeysNavigate(t *testing.T) {
	skipIfNoTmuxServer(t)
	if runtime.GOOS == "windows" {
		testWindowsPsmuxInputDelivery(t, "Down", "down")
		return
	}

	// Build the binary from current source so we test the working tree.
	binPath := testBinaryPath(t)
	build := exec.Command("go", "build", "-o", binPath, "./cmd/agent-deck/")
	build.Dir = findRepoRoot(t)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build agent-deck: %v\n%s", err, out)
	}

	sess := "inttest-tui-input-" + sanitizeName(t.Name())
	t.Cleanup(func() {
		_ = tmuxCommand("kill-session", "-t", sess).Run()
	})

	// Launch agent-deck in a tmux session with test profile (no real sessions).
	home := t.TempDir()
	args := append([]string{"new-session", "-d", "-s", sess, "-x", "120", "-y", "40"}, tuiLaunchCommand(binPath, "-p", "_test_tui_input")...)
	cmd := tuiTmuxCommand(t, home, args...)
	err := cmd.Run()
	if err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}

	// Wait for the TUI to render.
	waitForPane(t, sess, "Agent Deck", 5*time.Second)

	// Send arrow keys.
	_ = tmuxCommand("send-keys", "-t", sess, "Down").Run()
	time.Sleep(300 * time.Millisecond)
	_ = tmuxCommand("send-keys", "-t", sess, "Down").Run()
	time.Sleep(300 * time.Millisecond)
	_ = tmuxCommand("send-keys", "-t", sess, "Up").Run()
	time.Sleep(300 * time.Millisecond)

	// Capture the pane and check for raw escape sequences.
	content := capturePane(t, sess)

	// Raw escape sequences look like ^[[A, ^[[B, ESC[A, \x1b[A etc.
	// If terminal is in raw mode (correct), these are consumed by Bubble Tea.
	// If terminal is in cooked mode (broken), they appear as visible text.
	rawPatterns := []string{"^[[A", "^[[B", "^[[C", "^[[D", "\x1b[A", "\x1b[B"}
	for _, pat := range rawPatterns {
		if strings.Contains(content, pat) {
			t.Errorf("raw escape sequence %q found in pane output — arrow keys are not being interpreted.\n"+
				"This usually means Bubble Tea cannot set raw terminal mode on stdin.\n"+
				"Pane content:\n%s", pat, content)
		}
	}
}

// TestTUIInput_JKNavigation verifies that j/k keys work for navigation
// (not displayed as literal characters in unexpected places).
func TestTUIInput_JKNavigation(t *testing.T) {
	skipIfNoTmuxServer(t)
	if runtime.GOOS == "windows" {
		testWindowsPsmuxInputDelivery(t, "j", "j")
		testWindowsPsmuxInputDelivery(t, "k", "k")
		return
	}

	binPath := testBinaryPath(t)
	build := exec.Command("go", "build", "-o", binPath, "./cmd/agent-deck/")
	build.Dir = findRepoRoot(t)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build agent-deck: %v\n%s", err, out)
	}

	sess := "inttest-tui-jk-" + sanitizeName(t.Name())
	t.Cleanup(func() {
		_ = tmuxCommand("kill-session", "-t", sess).Run()
	})

	home := t.TempDir()
	args := append([]string{"new-session", "-d", "-s", sess, "-x", "120", "-y", "40"}, tuiLaunchCommand(binPath, "-p", "_test_tui_jk")...)
	cmd := tuiTmuxCommand(t, home, args...)
	err := cmd.Run()
	if err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}

	waitForPane(t, sess, "Agent Deck", 5*time.Second)

	// Send j and k (vim-style navigation).
	_ = tmuxCommand("send-keys", "-t", sess, "j").Run()
	time.Sleep(300 * time.Millisecond)
	_ = tmuxCommand("send-keys", "-t", sess, "k").Run()
	time.Sleep(300 * time.Millisecond)

	// In a working TUI, j/k are consumed as navigation commands.
	// If the TUI isn't in raw mode, they'd appear as typed text.
	// Check the status bar area (bottom lines) for stray characters.
	content := capturePane(t, sess)
	lines := strings.Split(content, "\n")

	// Check last 3 lines for stray j/k characters that indicate
	// keys are being echoed instead of interpreted.
	for i := len(lines) - 3; i < len(lines) && i >= 0; i++ {
		line := strings.TrimSpace(lines[i])
		// A line that is just "j", "k", "jk", etc. means keys are echoed.
		if line == "j" || line == "k" || line == "jk" || line == "kj" {
			t.Errorf("stray key characters %q found on bottom line %d — keys are being echoed, not interpreted.\n"+
				"Pane content:\n%s", line, i, content)
		}
	}
}

func testWindowsPsmuxInputDelivery(t *testing.T, key string, wantKey string) {
	t.Helper()

	home := t.TempDir()
	logPath := filepath.Join(home, "input.log")
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to resolve test executable: %v", err)
	}
	sess := "inttest-tui-input-" + sanitizeName(t.Name()) + "-" + strings.ToLower(key)
	t.Cleanup(func() {
		_ = tmuxCommand("kill-session", "-t", sess).Run()
	})

	args := []string{"new-session", "-d", "-s", sess, "-x", "80", "-y", "20", "cmd.exe"}
	cmd := tuiTmuxCommand(t, home, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create psmux session: %v\n%s", err, out)
	}

	waitForPane(t, sess, ">", 5*time.Second)
	sendLiteralKeys(t, sess, windowsTUIInputProbeCommand(exePath, logPath))
	sendKey(t, sess, "Enter")
	waitForPane(t, sess, "AGENTDECK_TUI_PROBE_READY", 5*time.Second)

	sendKey(t, sess, key)
	waitForInputLog(t, logPath, "key="+wantKey, 5*time.Second)
}

func sendLiteralKeys(t *testing.T, sess, keys string) {
	t.Helper()
	if out, err := tmuxCommand("send-keys", "-l", "-t", sess, "--", keys).CombinedOutput(); err != nil {
		t.Fatalf("failed to send literal keys to %q: %v\n%s", sess, err, out)
	}
}

func sendKey(t *testing.T, sess, key string) {
	t.Helper()
	if out, err := tmuxCommand("send-keys", "-t", sess, key).CombinedOutput(); err != nil {
		t.Fatalf("failed to send key %q to %q: %v\n%s", key, sess, err, out)
	}
}

func waitForInputLog(t *testing.T, path string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			data, _ := os.ReadFile(path)
			t.Fatalf("timed out waiting for input %q in %s; got:\n%s", want, path, data)
		case <-ticker.C:
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if strings.Contains(string(data), want) {
				return
			}
		}
	}
}

func runTUIInputProbe() {
	logPath := os.Getenv("AGENTDECK_TUI_INPUT_LOG")
	if logPath == "" {
		fmt.Fprintln(os.Stderr, "AGENTDECK_TUI_INPUT_LOG is required")
		os.Exit(2)
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open input log: %v\n", err)
		os.Exit(2)
	}
	defer file.Close()

	fmt.Println("AGENTDECK_TUI_PROBE_READY")
	model := tuiInputProbeModel{file: file}
	if _, err := tea.NewProgram(model, tea.WithoutRenderer()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run bubbletea input probe: %v\n", err)
		os.Exit(2)
	}
}

type tuiInputProbeModel struct {
	file *os.File
}

func (m tuiInputProbeModel) Init() tea.Cmd {
	return nil
}

func (m tuiInputProbeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		fmt.Fprintf(m.file, "key=%s type=%d runes=%q\n", key.String(), key.Type, string(key.Runes))
		_ = m.file.Sync()
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiInputProbeModel) View() string {
	return ""
}

// waitForPane polls tmux pane content until it contains the expected string.
func waitForPane(t *testing.T, sess, contains string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			content := capturePane(t, sess)
			t.Fatalf("timed out waiting for pane to contain %q.\nPane content:\n%s", contains, content)
		case <-ticker.C:
			content := capturePane(t, sess)
			if strings.Contains(content, contains) {
				return
			}
		}
	}
}

// capturePane returns the current tmux pane content.
func capturePane(t *testing.T, sess string) string {
	t.Helper()
	out, err := tmuxCommand("capture-pane", "-t", sess, "-p", "-S", "-50").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to capture pane: %v\noutput: %s\nsessions:\n%s", err, out, listTmuxSessions())
	}
	return string(out)
}

func tuiTmuxCommand(t *testing.T, home string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := tmuxCommand(args...)
	if runtime.GOOS == "windows" {
		return cmd
	}
	cmd.Env = tuiSubprocessEnv(home)
	return cmd
}

func tmuxCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", args...)
	if runtime.GOOS == "windows" {
		cmd.Env = windowsPsmuxSubprocessEnv()
	}
	return cmd
}

func windowsPsmuxSubprocessEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "TMUX"):
			continue
		case strings.HasPrefix(kv, "PSMUX_SESSION="):
			continue
		}
		env = append(env, kv)
	}
	return env
}

func tuiSubprocessEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "TMUX"):
			continue
		case strings.HasPrefix(kv, "PSMUX_SESSION="):
			continue
		case strings.HasPrefix(kv, "AGENTDECK_"):
			continue
		case strings.HasPrefix(kv, "HOME="):
			continue
		case strings.HasPrefix(kv, "USERPROFILE="):
			continue
		case strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR="):
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"AGENTDECK_TEST_USE_HOME=1",
		"TERM=dumb",
	)
	if vol := filepath.VolumeName(home); vol != "" {
		env = append(env, "HOMEDRIVE="+vol)
		if rest := strings.TrimPrefix(home, vol); rest != "" {
			env = append(env, "HOMEPATH="+rest)
		}
	}
	return env
}

func listTmuxSessions() string {
	out, err := tmuxCommand("list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		return string(out)
	}
	return string(out)
}

// findRepoRoot walks up from the working directory to find the go.mod file.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod)")
		}
		dir = parent
	}
}

func testBinaryPath(t *testing.T) string {
	t.Helper()
	name := "agent-deck-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(t.TempDir(), name)
}

func tuiLaunchCommand(binPath string, args ...string) []string {
	if runtime.GOOS != "windows" {
		return append([]string{binPath}, args...)
	}
	parts := []string{cmdNestedQuote(binPath)}
	for _, arg := range args {
		parts = append(parts, cmdNestedQuote(arg))
	}
	return []string{`cmd.exe /d /s /c "` + strings.Join(parts, " ") + `"`}
}

func windowsTUIInputProbeCommand(exePath, logPath string) string {
	return strings.Join([]string{
		`set "AGENTDECK_TUI_INPUT_HELPER=1"`,
		`set "AGENTDECK_TUI_INPUT_LOG=` + logPath + `"`,
		cmdQuote(exePath),
	}, " && ")
}

func cmdNestedQuote(s string) string {
	return `""` + strings.ReplaceAll(s, `"`, `""`) + `""`
}

func cmdQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
