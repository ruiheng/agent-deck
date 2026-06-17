package web

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestTmuxAttachCommand_NoIgnoreSize: the web's tmux attach must NOT pass
// `-f ignore-size`. Earlier the bridge combined ignore-size with a manual
// `tmux resize-window` call (since reverted) which had the side effect of
// flipping the session option to `window-size=manual`, dragging the window
// for ALL attached clients (Ghostty, iTerm) — the dots-in-window bug.
// With ignore-size removed and resize-window dropped, the web client
// participates in tmux's `window-size=largest` arbitration set at
// Session.Start (internal/tmux/tmux.go), so every client sees content sized
// to the biggest viewer.
func TestTmuxAttachCommand_NoIgnoreSize(t *testing.T) {
	t.Setenv("TMUX", "")

	cmd := tmuxAttachCommand("sess-1", "")

	wantArgs := []string{"tmux", "attach-session", "-t", "sess-1"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", cmd.Args, wantArgs)
	}
}

func TestTmuxAttachCommandUsesSocketFromTMUXEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test.sock,12345,0")

	cmd := tmuxAttachCommand("sess-2", "")

	wantArgs := []string{"tmux", "-S", "/tmp/tmux-test.sock", "attach-session", "-t", "sess-2"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected args with TMUX env: got %v want %v", cmd.Args, wantArgs)
	}

	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "TMUX=") {
			t.Fatalf("TMUX variable should be removed from command env, got %q", env)
		}
	}
}

// TestTmuxAttachCommand_SocketNameOverridesEnv: when the per-session socket
// name is explicit (MenuSession.TmuxSocketName, threaded through from
// Instance at v1.7.50), the legacy $TMUX env path is ignored and the web
// bridge targets the isolated agent-deck socket instead. This is the
// phase-1 guarantee for issue #687 users running `agent-deck web` inside
// their own tmux pane.
func TestTmuxAttachCommand_SocketNameOverridesEnv(t *testing.T) {
	// $TMUX is set to the user's default tmux — must be ignored because the
	// caller supplied an explicit socket name.
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	cmd := tmuxAttachCommand("agentdeck-foo", "agent-deck")

	wantArgs := []string{"tmux", "-L", "agent-deck", "attach-session", "-t", "agentdeck-foo"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("socket name must take precedence over $TMUX env\n got:  %v\n want: %v", cmd.Args, wantArgs)
	}

	// TMUX must be stripped so tmux-in-tmux refuse-to-nest guards don't trip.
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "TMUX=") {
			t.Fatalf("TMUX variable should be removed when socket name is set, got %q", env)
		}
	}
}

// TestResize_RejectsNonsensicalDimensions: the web bridge must reject resize
// requests with dimensions too small to be a real terminal. When xterm.js
// calls fitAddon.fit() on a display:none container, it computes cols≈2 rows≈1
// which, if forwarded to the PTY, shrinks the tmux window via window-size=largest
// and corrupts all session output until a session restart.
func TestResize_RejectsNonsensicalDimensions(t *testing.T) {
	bridge := &tmuxPTYBridge{}

	cases := []struct {
		name string
		cols int
		rows int
	}{
		{"cols=2 rows=1 (hidden container)", 2, 1},
		{"cols=5 rows=2 (still too small)", 5, 2},
		{"cols=9 rows=10 (just below col minimum)", 9, 10},
		{"cols=80 rows=2 (just below row minimum)", 80, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := bridge.Resize(tc.cols, tc.rows)
			if err == nil {
				t.Fatalf("Resize(%d, %d) should reject nonsensical dimensions", tc.cols, tc.rows)
			}
			if !strings.Contains(err.Error(), "too small") {
				t.Fatalf("expected 'too small' error, got: %v", err)
			}
		})
	}
}

func TestResize_AcceptsReasonableDimensions(t *testing.T) {
	bridge := &tmuxPTYBridge{}

	cases := []struct {
		name string
		cols int
		rows int
	}{
		{"minimum acceptable", 10, 3},
		{"typical terminal", 120, 40},
		{"wide monitor", 300, 80},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := bridge.Resize(tc.cols, tc.rows)
			if err == nil {
				return
			}
			if strings.Contains(err.Error(), "too small") {
				t.Fatalf("Resize(%d, %d) should not reject reasonable dimensions", tc.cols, tc.rows)
			}
		})
	}
}

// TestEnsureTERM exercises the launchd/systemd failure mode directly: a web
// daemon spawned by a supervisor inherits an environment with no TERM, and a
// tmux attach client with an unset/empty TERM aborts with "open terminal
// failed: terminal does not support clear". ensureTERM must guarantee a usable
// TERM without clobbering one the daemon legitimately inherited.
func TestEnsureTERM(t *testing.T) {
	const fallback = "TERM=xterm-256color"

	countTERM := func(env []string) (n int, last string) {
		for _, kv := range env {
			if strings.HasPrefix(kv, "TERM=") {
				n++
				last = kv
			}
		}
		return n, last
	}

	t.Run("unset TERM gets the fallback appended", func(t *testing.T) {
		env := []string{"PATH=/usr/bin", "HOME=/home/x"}
		got := ensureTERM(env)
		n, last := countTERM(got)
		if n != 1 || last != fallback {
			t.Fatalf("want exactly one %q, got n=%d last=%q (env=%v)", fallback, n, last, got)
		}
	})

	t.Run("empty TERM is replaced in place, not duplicated", func(t *testing.T) {
		env := []string{"TERM=", "PATH=/usr/bin"}
		got := ensureTERM(env)
		n, last := countTERM(got)
		if n != 1 {
			t.Fatalf("empty TERM must be replaced, not shadowed: got %d TERM entries (env=%v)", n, got)
		}
		if last != fallback {
			t.Fatalf("want TERM replaced with %q, got %q", fallback, last)
		}
	})

	t.Run("whitespace-only TERM is treated as empty and replaced", func(t *testing.T) {
		env := []string{"TERM=   ", "PATH=/usr/bin"}
		got := ensureTERM(env)
		n, last := countTERM(got)
		if n != 1 || last != fallback {
			t.Fatalf("whitespace TERM must be replaced: got n=%d last=%q (env=%v)", n, last, got)
		}
	})

	t.Run("inherited non-empty TERM is preserved untouched", func(t *testing.T) {
		env := []string{"TERM=screen-256color", "PATH=/usr/bin"}
		got := ensureTERM(env)
		n, last := countTERM(got)
		if n != 1 || last != "TERM=screen-256color" {
			t.Fatalf("inherited TERM must be preserved: got n=%d last=%q", n, last)
		}
	})

	t.Run("nil env is materialized and gets a TERM", func(t *testing.T) {
		got := ensureTERM(nil)
		if got == nil {
			t.Fatal("nil env must be materialized, got nil")
		}
		if n, _ := countTERM(got); n == 0 {
			t.Fatalf("materialized env must contain a TERM, got none (len=%d)", len(got))
		}
	})
}

// TestTmuxAttachCommand_InjectsTERM guards the wiring: the attach command's
// environment must always carry a non-empty TERM regardless of socket path, so
// the bridge renders under a TERM-less supervisor.
func TestTmuxAttachCommand_InjectsTERM(t *testing.T) {
	t.Setenv("TERM", "") // simulate a launchd-spawned daemon with no TERM

	for _, tc := range []struct {
		name       string
		socketName string
		tmuxEnv    string
	}{
		{"default server", "", ""},
		{"socket from TMUX env", "", "/tmp/tmux-test.sock,1,0"},
		{"explicit socket name", "agent-deck", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.tmuxEnv)
			cmd := tmuxAttachCommand("sess", tc.socketName)
			found := false
			for _, kv := range cmd.Env {
				if strings.HasPrefix(kv, "TERM=") && strings.TrimSpace(kv[len("TERM="):]) != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("attach command env must carry a non-empty TERM, got %v", cmd.Env)
			}
		})
	}
}

// TestTmuxAttachCommand_WhitespaceSocketNameFallsBackToEnv: the same
// defensive trim we use elsewhere. A typo like `socket_name = "   "` in
// config must not send the web bridge to a phantom server named "   " —
// treat as empty and use the legacy env path.
func TestTmuxAttachCommand_WhitespaceSocketNameFallsBackToEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test.sock,12345,0")

	cmd := tmuxAttachCommand("sess-3", "   \t")

	wantArgs := []string{"tmux", "-S", "/tmp/tmux-test.sock", "attach-session", "-t", "sess-3"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("whitespace-only socket name must fall through to legacy TMUX env\n got:  %v\n want: %v", cmd.Args, wantArgs)
	}
}

func TestSplitPollingInputMapsCommonTerminalControlBytes(t *testing.T) {
	got := splitPollingInput("echo hi\r\x7f\x03tail\n\x04")
	want := []pollingInputOp{
		{literal: "echo hi"},
		{key: "Enter"},
		{key: "BSpace"},
		{key: "C-c"},
		{literal: "tail"},
		{key: "Enter"},
		{key: "C-d"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitPollingInput mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFormatPollingCaptureForTerminalUsesCRLF(t *testing.T) {
	got := formatPollingCaptureForTerminal("one\ntwo\nthree", 0, 0, false)
	want := "\x1b[H\x1b[2Jone\r\ntwo\r\nthree"
	if got != want {
		t.Fatalf("formatPollingCaptureForTerminal = %q, want %q", got, want)
	}
}

func TestFormatPollingCaptureForTerminalTrimsTrailingBlankRows(t *testing.T) {
	got := formatPollingCaptureForTerminal("prompt> input\n\x1b[0m\n   \n", 0, 0, false)
	want := "\x1b[H\x1b[2Jprompt> input"
	if got != want {
		t.Fatalf("formatPollingCaptureForTerminal = %q, want %q", got, want)
	}
}

func TestFormatPollingCaptureForTerminalPositionsCursor(t *testing.T) {
	got := formatPollingCaptureForTerminal("one\ntwo\n\n\n", 2, 1, true)
	want := "\x1b[H\x1b[2Jone\r\ntwo\x1b[2;3H"
	if got != want {
		t.Fatalf("formatPollingCaptureForTerminal = %q, want %q", got, want)
	}
}

func TestFormatPollingCaptureForTerminalPreservesBlankCursorRow(t *testing.T) {
	got := formatPollingCaptureForTerminal("one\n\n\n", 4, 2, true)
	want := "\x1b[H\x1b[2Jone\r\n\r\n\x1b[3;5H"
	if got != want {
		t.Fatalf("formatPollingCaptureForTerminal = %q, want %q", got, want)
	}
}

func TestPollingBridgeResizeDoesNotRequirePTY(t *testing.T) {
	t.Setenv("TMUX", "")
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		writeFakeTmux(t, filepath.Join(dir, "tmux.cmd"), `@echo off
if "%1"=="resize-window" (
  echo %*> "%FAKE_TMUX_LOG%"
  exit /b 0
)
if "%1"=="capture-pane" (
  echo resized
  exit /b 0
)
if "%1"=="display-message" (
  echo 0,0
  exit /b 0
)
echo unexpected command: %* 1>&2
exit /b 1
`)
	} else {
		writeFakeTmux(t, filepath.Join(dir, "tmux"), `#!/bin/sh
case "$1" in
  resize-window)
    printf '%s\n' "$*" > "$FAKE_TMUX_LOG"
    exit 0
    ;;
  capture-pane)
    printf 'resized\n'
    exit 0
    ;;
  display-message)
    printf '0,0\n'
    exit 0
    ;;
esac
printf 'unexpected command: %s\n' "$*" >&2
exit 1
`)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	logPath := filepath.Join(dir, "tmux.log")
	t.Setenv("FAKE_TMUX_LOG", logPath)

	b := &tmuxPTYBridge{
		tmuxSession: "sess",
		writer:      fakeTerminalWriter{},
		polling:     true,
	}
	if err := b.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake tmux log: %v", err)
	}
	if !strings.Contains(string(got), "resize-window -t sess -x 120 -y 40") {
		t.Fatalf("resize-window not called correctly: %q", string(got))
	}
}

func TestPollingBridgeCaptureUsesAlternateScreenWhenActive(t *testing.T) {
	t.Setenv("TMUX", "")
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		writeFakeTmux(t, filepath.Join(dir, "tmux.cmd"), `@echo off
if "%1"=="display-message" (
  if "%FAKE_ALT%"=="1" (echo 1) else (echo 0)
  exit /b 0
)
if "%1"=="capture-pane" (
  echo %*> "%FAKE_TMUX_LOG%"
  echo alt screen
  exit /b 0
)
echo unexpected command: %* 1>&2
exit /b 1
`)
	} else {
		writeFakeTmux(t, filepath.Join(dir, "tmux"), `#!/bin/sh
case "$1" in
  display-message)
    if [ "$FAKE_ALT" = "1" ]; then
      printf '1\n'
    else
      printf '0\n'
    fi
    exit 0
    ;;
  capture-pane)
    printf '%s\n' "$*" > "$FAKE_TMUX_LOG"
    printf 'alt screen\n'
    exit 0
    ;;
esac
printf 'unexpected command: %s\n' "$*" >&2
exit 1
`)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	logPath := filepath.Join(dir, "tmux.log")
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("FAKE_ALT", "1")

	b := &tmuxPTYBridge{tmuxSession: "sess"}
	output, err := b.capturePaneOutput()
	if err != nil {
		t.Fatalf("capturePaneOutput: %v", err)
	}
	if !strings.Contains(string(output), "alt screen") {
		t.Fatalf("capture output = %q, want alt screen", string(output))
	}
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake tmux log: %v", err)
	}
	if !strings.Contains(string(got), "-a") {
		t.Fatalf("alternate capture did not include -a: %q", string(got))
	}
}

type fakeTerminalWriter struct{}

func (fakeTerminalWriter) WriteJSON(any) error {
	return nil
}

func (fakeTerminalWriter) WriteBinary([]byte) error {
	return nil
}

func writeFakeTmux(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
