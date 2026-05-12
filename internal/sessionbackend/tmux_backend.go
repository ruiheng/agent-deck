package sessionbackend

import (
	"context"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/tmux"
)

// TmuxBackend is a thin adapter over the existing tmux session runtime.
type TmuxBackend struct {
	session *tmux.Session
}

func NewTmuxBackend(session *tmux.Session) *TmuxBackend {
	if session == nil {
		return nil
	}
	return &TmuxBackend{session: session}
}

func NewBackend(session *tmux.Session) SessionBackend {
	if session == nil {
		return nil
	}
	return NewTmuxBackend(session)
}

func (b *TmuxBackend) Name() string { return b.session.Name }

func (b *TmuxBackend) DisplayName() string { return b.session.DisplayName }

func (b *TmuxBackend) SetDisplayName(name string) { b.session.DisplayName = name }

func (b *TmuxBackend) WorkDir() string { return b.session.WorkDir }

func (b *TmuxBackend) SetWorkDir(path string) { b.session.WorkDir = path }

func (b *TmuxBackend) Exists() bool { return b.session.Exists() }

func (b *TmuxBackend) Attach(ctx context.Context, detachByte ...byte) error {
	return b.session.Attach(ctx, detachByte...)
}

func (b *TmuxBackend) AttachWindow(ctx context.Context, windowIndex int, detachByte ...byte) error {
	return b.session.AttachWindow(ctx, windowIndex, detachByte...)
}

func (b *TmuxBackend) SetEnvironment(key, value string) error {
	return b.session.SetEnvironment(key, value)
}

func (b *TmuxBackend) GetEnvironment(key string) (string, error) {
	return b.session.GetEnvironment(key)
}

func (b *TmuxBackend) InvalidateEnvCache() { b.session.InvalidateEnvCache() }

func (b *TmuxBackend) Start(command string) error { return b.session.Start(command) }

func (b *TmuxBackend) Kill() error { return b.session.Kill() }

func (b *TmuxBackend) RespawnPane(command string) error { return b.session.RespawnPane(command) }

func (b *TmuxBackend) GetWindowActivity() (int64, error) { return b.session.GetWindowActivity() }

func (b *TmuxBackend) GetCachedWindowActivity() int64 { return b.session.GetCachedWindowActivity() }

func (b *TmuxBackend) CapturePane() (string, error) { return b.session.CapturePane() }

func (b *TmuxBackend) CapturePaneFresh() (string, error) { return b.session.CapturePaneFresh() }

func (b *TmuxBackend) CaptureFullHistory() (string, error) { return b.session.CaptureFullHistory() }

func (b *TmuxBackend) CaptureWindowFullHistory(windowIndex int) (string, error) {
	return b.session.CaptureWindowFullHistory(windowIndex)
}

func (b *TmuxBackend) HasUpdated() (bool, error) { return b.session.HasUpdated() }

func (b *TmuxBackend) DetectTool() string { return b.session.DetectTool() }

func (b *TmuxBackend) GetStatus() (string, error) { return b.session.GetStatus() }

func (b *TmuxBackend) Acknowledge() { b.session.Acknowledge() }

func (b *TmuxBackend) ResetAcknowledged() { b.session.ResetAcknowledged() }

func (b *TmuxBackend) IsAcknowledged() bool { return b.session.IsAcknowledged() }

func (b *TmuxBackend) GetLastActivityTime() time.Time { return b.session.GetLastActivityTime() }

func (b *TmuxBackend) GetWaitingSince() time.Time { return b.session.GetWaitingSince() }

func (b *TmuxBackend) SendKeys(keys string) error { return b.session.SendKeys(keys) }

func (b *TmuxBackend) SendEnter() error { return b.session.SendEnter() }

func (b *TmuxBackend) SendKeysAndEnter(keys string) error { return b.session.SendKeysAndEnter(keys) }

func (b *TmuxBackend) SendKeysChunked(content string) error {
	return b.session.SendKeysChunked(content)
}

func (b *TmuxBackend) SendCtrlC() error { return b.session.SendCtrlC() }

func (b *TmuxBackend) SendCtrlU() error { return b.session.SendCtrlU() }

func (b *TmuxBackend) SendCommand(command string) error { return b.session.SendCommand(command) }

func (b *TmuxBackend) GetWorkDir() string { return b.session.GetWorkDir() }

func (b *TmuxBackend) EnsureConfigured() { b.session.EnsureConfigured() }

func (b *TmuxBackend) IsConfigured() bool { return b.session.IsConfigured() }
