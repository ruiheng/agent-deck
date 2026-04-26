package sessionbackend

import (
	"context"
	"time"
)

// SessionBackend is the minimal session/runtime seam needed to decouple the
// operator loop from the concrete tmux implementation.
//
// The method set intentionally mirrors the currently-consumed session surface
// so migration can proceed incrementally without a broad rewrite.
type SessionBackend interface {
	Name() string
	DisplayName() string
	SetDisplayName(string)
	WorkDir() string
	SetWorkDir(string)

	Exists() bool
	Attach(ctx context.Context, detachByte ...byte) error
	AttachWindow(ctx context.Context, windowIndex int, detachByte ...byte) error

	SetEnvironment(key, value string) error
	GetEnvironment(key string) (string, error)
	InvalidateEnvCache()

	Start(command string) error
	Kill() error
	RespawnPane(command string) error

	GetWindowActivity() (int64, error)
	GetCachedWindowActivity() int64
	CapturePane() (string, error)
	CapturePaneFresh() (string, error)
	CaptureFullHistory() (string, error)
	CaptureWindowFullHistory(windowIndex int) (string, error)
	HasUpdated() (bool, error)
	DetectTool() string
	GetStatus() (string, error)

	Acknowledge()
	ResetAcknowledged()
	IsAcknowledged() bool
	GetLastActivityTime() time.Time
	GetWaitingSince() time.Time

	SendKeys(keys string) error
	SendEnter() error
	SendKeysAndEnter(keys string) error
	SendKeysChunked(content string) error
	SendCtrlC() error
	SendCtrlU() error
	SendCommand(command string) error
	GetWorkDir() string
	EnsureConfigured()
	IsConfigured() bool
}
