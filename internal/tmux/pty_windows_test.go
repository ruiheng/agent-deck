//go:build windows

package tmux

import (
	"context"
	"strings"
	"testing"
)

func TestWindowsAttachCommandStripsPSMUXSession(t *testing.T) {
	t.Setenv("PSMUX_SESSION", "leader")
	s := &Session{}
	cmd := s.windowsAttachCommand(context.Background(), "attach-session", "-t", "demo")

	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "PSMUX_SESSION=") {
			t.Fatalf("PSMUX_SESSION should be stripped from attach env, got %q", kv)
		}
	}
}

func TestWindowsAttachMissingSession(t *testing.T) {
	s := &Session{Name: "agentdeck_missing_session_zzz"}
	err := s.Attach(context.Background())
	if err == nil {
		t.Fatal("Attach() = nil, want error")
	}
	if strings.Contains(err.Error(), "Access is denied") {
		t.Skipf("tmux unavailable in test environment: %v", err)
	}
}

func TestWindowsAttachWindowMissingSession(t *testing.T) {
	s := &Session{Name: "agentdeck_missing_window_zzz"}
	err := s.AttachWindow(context.Background(), 0)
	if err == nil {
		t.Fatal("AttachWindow() = nil, want error")
	}
	if strings.Contains(err.Error(), "Access is denied") {
		t.Skipf("tmux unavailable in test environment: %v", err)
	}
}

func TestWindowsAttachReadOnlyMissingSession(t *testing.T) {
	s := &Session{Name: "agentdeck_missing_readonly_zzz"}
	err := s.AttachReadOnly(context.Background())
	if err == nil {
		t.Fatal("AttachReadOnly() = nil, want error")
	}
	if strings.Contains(err.Error(), "Access is denied") {
		t.Skipf("tmux unavailable in test environment: %v", err)
	}
}

func TestWindowsAttachExitCodeIsSuccess(t *testing.T) {
	if !windowsAttachExitCodeIsSuccess(0) {
		t.Fatal("exit 0 should succeed")
	}
	if !windowsAttachExitCodeIsSuccess(1) {
		t.Fatal("exit 1 should succeed")
	}
	if windowsAttachExitCodeIsSuccess(2) {
		t.Fatal("unexpected non-zero/non-one exit code should fail")
	}
}

func TestWindowsDetachKeyName(t *testing.T) {
	tests := []struct {
		name       string
		detachByte byte
		want       string
	}{
		{name: "ctrl-q", detachByte: 17, want: "C-q"},
		{name: "ctrl-a", detachByte: 1, want: "C-a"},
		{name: "ctrl-z", detachByte: 26, want: "C-z"},
		{name: "invalid defaults", detachByte: 0, want: "C-q"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowsDetachKeyName(tt.detachByte); got != tt.want {
				t.Fatalf("windowsDetachKeyName(%d) = %q, want %q", tt.detachByte, got, tt.want)
			}
		})
	}
}
