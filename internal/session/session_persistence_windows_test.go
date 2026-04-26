//go:build windows

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistence_WindowsDefaultIsDirect(t *testing.T) {
	home := isolatedHomeDir(t)
	cfg := filepath.Join(home, ".agent-deck", "config.toml")
	if err := os.WriteFile(cfg, []byte(""), 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	ClearUserConfigCache()

	settings := GetTmuxSettings()
	if settings.GetLaunchInUserScope() {
		t.Fatalf("native Windows must not default to Linux user-scope launch")
	}
}

func TestPersistence_WindowsFreshSessionUsesSessionIDNotResume(t *testing.T) {
	_ = isolatedHomeDir(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	inst := NewInstanceWithTool("persist-windows-fresh", `C:\agent-deck\fresh`, "claude")
	inst.ClaudeSessionID = "11111111-1111-4111-8111-111111111111"

	cmdLine := inst.buildClaudeResumeCommand()
	if !strings.Contains(cmdLine, "--session-id "+inst.ClaudeSessionID) {
		t.Fatalf("fresh Windows session should use --session-id %s, got %q", inst.ClaudeSessionID, cmdLine)
	}
	if strings.Contains(cmdLine, "--resume") {
		t.Fatalf("fresh Windows session must not use --resume without JSONL transcript, got %q", cmdLine)
	}
}

func TestPersistence_WindowsTranscriptUsesResume(t *testing.T) {
	home := isolatedHomeDir(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	projectPath := `C:\agent-deck\resume`
	inst := NewInstanceWithTool("persist-windows-resume", projectPath, "claude")
	inst.ClaudeSessionID = "22222222-2222-4222-8222-222222222222"

	projectDir := filepath.Join(home, ".claude", "projects", ConvertToClaudeDirName(projectPath))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	jsonl := filepath.Join(projectDir, inst.ClaudeSessionID+".jsonl")
	if err := os.WriteFile(jsonl, []byte(`{"sessionId":"`+inst.ClaudeSessionID+`","type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	cmdLine := inst.buildClaudeResumeCommand()
	if !strings.Contains(cmdLine, "--resume "+inst.ClaudeSessionID) {
		t.Fatalf("Windows session with JSONL should use --resume %s, got %q", inst.ClaudeSessionID, cmdLine)
	}
}
