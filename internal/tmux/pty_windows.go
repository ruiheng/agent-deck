//go:build windows

package tmux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const windowsAttachMinInteractiveDuration = 750 * time.Millisecond

// IndexDetachKey returns the index of a control-key sequence in data, or -1 if
// not found. On Windows we currently only need raw-byte detection for compile-
// time consumers; the native attach path is implemented separately.
func IndexDetachKey(data []byte, detachByte byte) int {
	return bytes.IndexByte(data, detachByte)
}

// IndexCtrlQ returns the index of a Ctrl+Q sequence in data, or -1 if not found.
func IndexCtrlQ(data []byte) int {
	return IndexDetachKey(data, 17)
}

func (s *Session) windowsAttachCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := s.tmuxCmdContext(ctx, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// psmux warns about nested sessions when this env var is inherited from the
	// leader pane. Clearing it allows attaching to another managed session from
	// inside the current psmux session.
	cmd.Env = environWithoutPSMUXSession()
	return cmd
}

func windowsAttachExitCodeIsSuccess(exitCode int, attachDuration time.Duration) bool {
	switch exitCode {
	case 0:
		return true
	case 1:
		// psmux returns 1 for a normal interactive detach on Windows. Keep
		// stdin/stdout/stderr inherited directly from the real console: capturing
		// attach output through Go pipes makes psmux fail with
		// "incorrect function" because it no longer sees console handles.
		//
		// Direct psmux existence probes can false-negative even for live
		// sessions, so elapsed interactive duration is the only local signal
		// we trust to distinguish attach+detach from immediate target errors.
		return attachDuration >= windowsAttachMinInteractiveDuration
	default:
		return false
	}
}

func windowsAttachExitIsSuccess(err error, attachDuration time.Duration) bool {
	if err == nil {
		return true
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	return windowsAttachExitCodeIsSuccess(exitErr.ExitCode(), attachDuration)
}

func windowsDetachKeyName(detachByte byte) string {
	if detachByte >= 1 && detachByte <= 26 {
		return fmt.Sprintf("C-%c", 'a'+detachByte-1)
	}
	return "C-q"
}

func (s *Session) bindWindowsDetachKey(detachByte byte) func() {
	key := windowsDetachKeyName(detachByte)
	if err := s.windowsAttachCommand(context.Background(), "bind-key", "-n", key, "detach-client").Run(); err != nil {
		return func() {}
	}
	return func() {
		_ = s.windowsAttachCommand(context.Background(), "unbind-key", "-n", key).Run()
	}
}

// Attach attaches to the session using the current Windows console.
func (s *Session) Attach(ctx context.Context, detachByte ...byte) error {
	// Do not preflight with has-session on Windows. psmux can report false
	// negatives from both cached and direct existence probes even when the UI
	// has a live session. attach-session itself is the authoritative operation.
	detach := byte(17)
	if len(detachByte) > 0 && detachByte[0] != 0 {
		detach = detachByte[0]
	}
	cleanupDetachKey := s.bindWindowsDetachKey(detach)
	defer cleanupDetachKey()

	args := []string{"attach-session", "-t", s.Name}
	cmd := s.windowsAttachCommand(ctx, args...)
	startedAt := time.Now()
	err := cmd.Run()
	attachDuration := time.Since(startedAt)
	if !windowsAttachExitIsSuccess(err, attachDuration) {
		return fmt.Errorf("attach command failed: %w", err)
	}
	return nil
}

// AttachWindow attaches to a specific window within this session.
func (s *Session) AttachWindow(ctx context.Context, windowIndex int, detachByte ...byte) error {
	target := fmt.Sprintf("%s:%d", s.Name, windowIndex)
	selectArgs := []string{"select-window", "-t", target}
	selectCmd := s.windowsAttachCommand(ctx, selectArgs...)
	if err := selectCmd.Run(); err != nil {
		return fmt.Errorf("failed to select window %s: %w", target, err)
	}
	return s.Attach(ctx, detachByte...)
}

// Resize changes the terminal size of the session.
func (s *Session) Resize(cols, rows int) error {
	return nil
}

// AttachReadOnly attaches to the session in read-only mode.
func (s *Session) AttachReadOnly(ctx context.Context) error {
	cleanupDetachKey := s.bindWindowsDetachKey(17)
	defer cleanupDetachKey()

	args := []string{"attach-session", "-r", "-t", s.Name}
	cmd := s.windowsAttachCommand(ctx, args...)
	startedAt := time.Now()
	err := cmd.Run()
	attachDuration := time.Since(startedAt)
	if !windowsAttachExitIsSuccess(err, attachDuration) {
		return fmt.Errorf("attach read-only command failed: %w", err)
	}
	return nil
}

// StreamOutput streams the session output to the provided writer.
func (s *Session) StreamOutput(ctx context.Context, w io.Writer) error {
	return fmt.Errorf("stream output not implemented on Windows yet")
}
