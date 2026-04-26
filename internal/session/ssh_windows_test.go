//go:build windows

package session

import (
	"strings"
	"testing"
)

func TestSSHRunnerSSHBaseArgs_WindowsCoreShape(t *testing.T) {
	runner := &SSHRunner{Host: "user@example.com"}
	args := runner.sshBaseArgs("agent-deck list --json")
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "ControlMaster=auto") || strings.Contains(joined, "ControlPersist=600") || strings.Contains(joined, "ControlPath=") {
		t.Fatalf("sshBaseArgs() should not use ControlMaster on Windows v2 core scope, got %q", joined)
	}
	if !strings.Contains(joined, "ConnectTimeout=10") {
		t.Fatalf("sshBaseArgs() missing ConnectTimeout, got %q", joined)
	}
	if !strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("sshBaseArgs() missing BatchMode, got %q", joined)
	}
	if args[len(args)-2] != runner.Host {
		t.Fatalf("sshBaseArgs() host position = %q, want %q", args[len(args)-2], runner.Host)
	}
	if args[len(args)-1] != "agent-deck list --json" {
		t.Fatalf("sshBaseArgs() remote command = %q", args[len(args)-1])
	}
}

func TestSSHRunnerSSHBaseArgs_WindowsHostPort(t *testing.T) {
	runner := &SSHRunner{Host: "user@example.com:2222"}
	args := runner.sshBaseArgs("agent-deck list --json")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-p 2222") {
		t.Fatalf("sshBaseArgs() should include -p for host:port, got %q", joined)
	}
	for _, arg := range args {
		if arg == "user@example.com:2222" {
			t.Fatalf("sshBaseArgs() should not pass user@host:port as destination token, got %#v", args)
		}
	}
}

func TestSSHRunnerSSHBaseArgs_WindowsBareIPv6(t *testing.T) {
	runner := &SSHRunner{Host: "user@2001:db8::1"}
	args := runner.sshBaseArgs("agent-deck list --json")
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "-p 1") {
		t.Fatalf("sshBaseArgs() must not treat bare IPv6 suffix as a port, got %q", joined)
	}
	if args[len(args)-2] != "user@2001:db8::1" {
		t.Fatalf("sshBaseArgs() destination = %q, want bare IPv6 host", args[len(args)-2])
	}
}

func TestSSHRunnerWindowsAttachArgs(t *testing.T) {
	runner := &SSHRunner{Host: "user@example.com:2222"}
	args := runner.windowsAttachArgs("agent-deck session attach abc123")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-tt",
		"ConnectTimeout=10",
		"-p 2222",
		"user@example.com",
		"agent-deck session attach abc123",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("windowsAttachArgs() = %q, want to contain %q", joined, want)
		}
	}
	for _, arg := range args {
		if arg == "user@example.com:2222" {
			t.Fatalf("windowsAttachArgs() should split host:port, got %#v", args)
		}
	}
}

func TestSplitSSHHostPort(t *testing.T) {
	tests := []struct {
		host     string
		wantDest string
		wantPort string
	}{
		{host: "user@example.com", wantDest: "user@example.com", wantPort: ""},
		{host: "user@example.com:2222", wantDest: "user@example.com", wantPort: "2222"},
		{host: "example.com:2022", wantDest: "example.com", wantPort: "2022"},
		{host: "user@example.com:notaport", wantDest: "user@example.com:notaport", wantPort: ""},
		{host: "user@2001:db8::1", wantDest: "user@2001:db8::1", wantPort: ""},
		{host: "user@[2001:db8::1]", wantDest: "user@[2001:db8::1]", wantPort: ""},
		{host: "user@[2001:db8::1]:2222", wantDest: "user@[2001:db8::1]", wantPort: "2222"},
	}

	for _, tt := range tests {
		gotDest, gotPort := splitSSHHostPort(tt.host)
		if gotDest != tt.wantDest || gotPort != tt.wantPort {
			t.Fatalf("splitSSHHostPort(%q) = (%q, %q), want (%q, %q)", tt.host, gotDest, gotPort, tt.wantDest, tt.wantPort)
		}
	}
}

func TestSSHRunnerEnsureSSHAvailable_MissingBinary(t *testing.T) {
	runner := &SSHRunner{}
	err := runner.ensureSSHAvailable("__agent_deck_missing_ssh__")
	if err == nil {
		t.Fatal("ensureSSHAvailable() returned nil for missing binary")
	}
}
