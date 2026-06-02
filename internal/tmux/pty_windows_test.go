//go:build windows

package tmux

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestWindowsAttachCommandPreservesSocketName(t *testing.T) {
	s := &Session{SocketName: "agentdeck-test"}
	cmd := s.windowsAttachCommand(context.Background(), "attach-session", "-t", "demo")
	want := []string{"tmux", "-L", "agentdeck-test", "attach-session", "-t", "demo"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("Windows native attach must preserve socket isolation\n got:  %v\n want: %v", cmd.Args, want)
	}
}

func TestWindowsControlCommandDoesNotInheritConsole(t *testing.T) {
	t.Setenv("PSMUX_SESSION", "leader")
	s := &Session{SocketName: "agentdeck-test"}
	cmd := s.windowsControlCommand(context.Background(), "unbind-key", "-n", "-T", "root", "C-q")

	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatal("non-interactive Windows tmux commands must not inherit console handles")
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "PSMUX_SESSION=") {
			t.Fatalf("PSMUX_SESSION should be stripped from control env, got %q", kv)
		}
	}
	want := []string{"tmux", "-L", "agentdeck-test", "unbind-key", "-n", "-T", "root", "C-q"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("Windows control command must preserve socket isolation\n got:  %v\n want: %v", cmd.Args, want)
	}
}

func TestWindowsDetachKeyBindingUsesRootTable(t *testing.T) {
	s := &Session{SocketName: "agentdeck-test"}
	key := windowsDetachKeyName(17)

	bindCmd := s.windowsControlCommand(context.Background(), "bind-key", "-n", "-T", "root", key, "detach-client")
	wantBind := []string{"tmux", "-L", "agentdeck-test", "bind-key", "-n", "-T", "root", "C-q", "detach-client"}
	if !reflect.DeepEqual(bindCmd.Args, wantBind) {
		t.Fatalf("Windows detach bind must target root table\n got:  %v\n want: %v", bindCmd.Args, wantBind)
	}

	unbindCmd := s.windowsControlCommand(context.Background(), "unbind-key", "-n", "-T", "root", key)
	wantUnbind := []string{"tmux", "-L", "agentdeck-test", "unbind-key", "-n", "-T", "root", "C-q"}
	if !reflect.DeepEqual(unbindCmd.Args, wantUnbind) {
		t.Fatalf("Windows detach unbind must target root table\n got:  %v\n want: %v", unbindCmd.Args, wantUnbind)
	}
}

func TestWindowsAttachMissingSession(t *testing.T) {
	t.Skip("psmux missing-target process exit is not stable enough for an integration assertion; exit classification is covered below")
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
	t.Skip("psmux missing-target process exit is not stable enough for an integration assertion; exit classification is covered below")
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
	if !windowsAttachExitCodeIsSuccess(0, 0) {
		t.Fatal("exit 0 should succeed")
	}
	if !windowsAttachExitCodeIsSuccess(1, windowsAttachMinInteractiveDuration) {
		t.Fatal("exit 1 after interactive duration should succeed")
	}
	if windowsAttachExitCodeIsSuccess(1, windowsAttachMinInteractiveDuration-time.Millisecond) {
		t.Fatal("quick exit 1 should fail without evidence of an interactive attach")
	}
	if windowsAttachExitCodeIsSuccess(2, windowsAttachMinInteractiveDuration) {
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
