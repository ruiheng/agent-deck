package sessionbackend

import (
	"context"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/tmux"
)

// WindowsBackend is the Windows runtime/backend slot behind the session seam.
// In this phase it reuses the existing tmux/psmux-backed session object while
// giving the rest of the codebase a Windows-specific backend code path.
type WindowsBackend struct {
	session *tmux.Session
}

func NewWindowsBackend(session *tmux.Session) *WindowsBackend {
	if session == nil {
		return nil
	}
	return &WindowsBackend{session: session}
}

func (b *WindowsBackend) Name() string { return b.session.Name }

func (b *WindowsBackend) DisplayName() string { return b.session.DisplayName }

func (b *WindowsBackend) SetDisplayName(name string) { b.session.DisplayName = name }

func (b *WindowsBackend) WorkDir() string { return b.session.WorkDir }

func (b *WindowsBackend) SetWorkDir(path string) { b.session.WorkDir = path }

func (b *WindowsBackend) Exists() bool { return b.session.Exists() }

func (b *WindowsBackend) Attach(ctx context.Context, detachByte ...byte) error {
	return b.session.Attach(ctx, detachByte...)
}

func (b *WindowsBackend) AttachWindow(ctx context.Context, windowIndex int, detachByte ...byte) error {
	return b.session.AttachWindow(ctx, windowIndex, detachByte...)
}

func (b *WindowsBackend) SetEnvironment(key, value string) error {
	return b.session.SetEnvironment(key, value)
}

func (b *WindowsBackend) GetEnvironment(key string) (string, error) {
	return b.session.GetEnvironment(key)
}

func (b *WindowsBackend) InvalidateEnvCache() { b.session.InvalidateEnvCache() }

func (b *WindowsBackend) Start(command string) error { return b.session.Start(command) }

func (b *WindowsBackend) Kill() error { return b.session.Kill() }

func (b *WindowsBackend) RespawnPane(command string) error { return b.session.RespawnPane(command) }

func (b *WindowsBackend) GetWindowActivity() (int64, error) { return b.session.GetWindowActivity() }

func (b *WindowsBackend) GetCachedWindowActivity() int64 { return b.session.GetCachedWindowActivity() }

func (b *WindowsBackend) CapturePane() (string, error) { return b.session.CapturePane() }

func (b *WindowsBackend) CapturePaneFresh() (string, error) { return b.session.CapturePaneFresh() }

func (b *WindowsBackend) CaptureFullHistory() (string, error) { return b.session.CaptureFullHistory() }

func (b *WindowsBackend) CaptureWindowFullHistory(windowIndex int) (string, error) {
	return b.session.CaptureWindowFullHistory(windowIndex)
}

func (b *WindowsBackend) HasUpdated() (bool, error) { return b.session.HasUpdated() }

func (b *WindowsBackend) DetectTool() string { return b.session.DetectTool() }

func (b *WindowsBackend) GetStatus() (string, error) { return b.session.GetStatus() }

func (b *WindowsBackend) Acknowledge() { b.session.Acknowledge() }

func (b *WindowsBackend) ResetAcknowledged() { b.session.ResetAcknowledged() }

func (b *WindowsBackend) IsAcknowledged() bool { return b.session.IsAcknowledged() }

func (b *WindowsBackend) GetLastActivityTime() time.Time { return b.session.GetLastActivityTime() }

func (b *WindowsBackend) GetWaitingSince() time.Time { return b.session.GetWaitingSince() }

func (b *WindowsBackend) SendKeys(keys string) error { return b.session.SendKeys(keys) }

func (b *WindowsBackend) SendEnter() error { return b.session.SendEnter() }

func (b *WindowsBackend) SendKeysAndEnter(keys string) error { return b.session.SendKeysAndEnter(keys) }

func (b *WindowsBackend) SendKeysChunked(content string) error {
	return b.session.SendKeysChunked(content)
}

func (b *WindowsBackend) SendCtrlC() error { return b.session.SendCtrlC() }

func (b *WindowsBackend) SendCtrlU() error { return b.session.SendCtrlU() }

func (b *WindowsBackend) SendCommand(command string) error { return b.session.SendCommand(command) }

func (b *WindowsBackend) GetWorkDir() string { return b.session.GetWorkDir() }

func (b *WindowsBackend) EnsureConfigured() { b.session.EnsureConfigured() }

func (b *WindowsBackend) IsConfigured() bool { return b.session.IsConfigured() }
