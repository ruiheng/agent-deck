package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAgyCommand_FreshAndBoundResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENTDECK_PROFILE", "agy-test")
	ClearUserConfigCache()
	t.Cleanup(ClearUserConfigCache)

	for _, tool := range []string{"antigravity", "agy"} {
		t.Run(tool, func(t *testing.T) {
			inst := NewInstanceWithTool("agy-fresh", t.TempDir(), tool)
			fresh := inst.buildAgyCommand("agy")
			if strings.Contains(fresh, "--conversation") {
				t.Fatalf("fresh agy command must not emit --conversation: %q", fresh)
			}
			if strings.Contains(fresh, " -c") || strings.Contains(fresh, "--continue") {
				t.Fatalf("agy command must never emit -c/--continue: %q", fresh)
			}
			if !inst.launchesFresh.Load() {
				t.Fatal("expected fresh agy launch to set launchesFresh=true")
			}

			inst.AgySessionID = "conv-uuid-1234"
			resumed := inst.buildAgyCommand("agy")
			if !strings.Contains(resumed, "--conversation") || !strings.Contains(resumed, "conv-uuid-1234") {
				t.Fatalf("bound agy command must resume with --conversation conv-uuid-1234, got: %q", resumed)
			}
			if inst.launchesFresh.Load() {
				t.Fatal("expected bound resume launch to set launchesFresh=false")
			}
			if strings.Contains(resumed, " -c") || strings.Contains(resumed, "--continue") {
				t.Fatalf("bound agy command must never emit -c/--continue: %q", resumed)
			}

			// Custom --conversation flag in baseCommand must not be duplicated.
			explicit := inst.buildAgyCommand("agy --conversation override-999")
			if count := strings.Count(explicit, "--conversation"); count != 1 {
				t.Fatalf("expected single --conversation flag when user already specified one, got %d in %q", count, explicit)
			}
		})
	}
}

func TestBuildAgyCommand_LegacyGenericSessionIDFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ClearUserConfigCache()
	t.Cleanup(ClearUserConfigCache)

	inst := NewInstanceWithTool("agy-legacy", t.TempDir(), "antigravity")
	inst.GenericSessionID = "conv-legacy-567"
	if got := inst.EffectiveAgySessionID(); got != "conv-legacy-567" {
		t.Fatalf("EffectiveAgySessionID() = %q, want conv-legacy-567", got)
	}
	cmd := inst.buildAgyCommand("agy")
	if !strings.Contains(cmd, "--conversation") || !strings.Contains(cmd, "conv-legacy-567") {
		t.Fatalf("expected legacy GenericSessionID to emit --conversation conv-legacy-567, got: %q", cmd)
	}

	// Explicit AgySessionID takes precedence over GenericSessionID.
	inst.AgySessionID = "conv-primary-890"
	if got := inst.EffectiveAgySessionID(); got != "conv-primary-890" {
		t.Fatalf("EffectiveAgySessionID() = %q, want conv-primary-890", got)
	}

	// Fresh-start clearing wipes both primary and legacy fallback.
	inst.clearSessionBindingForFreshStart()
	if got := inst.EffectiveAgySessionID(); got != "" {
		t.Fatalf("after clearSessionBindingForFreshStart EffectiveAgySessionID() = %q, want empty", got)
	}
}

func TestBuildAgyCommandWithMessage_PromptAndOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ClearUserConfigCache()
	t.Cleanup(ClearUserConfigCache)

	inst := NewInstanceWithTool("agy-msg", t.TempDir(), "antigravity")
	inst.AgySessionID = "conv-msg-1"
	if err := inst.SetAgyOptions(&AgyOptions{
		Model:         "gemini-2.5-pro",
		Effort:        "high",
		Mode:          "auto",
		DangerousMode: true,
	}); err != nil {
		t.Fatalf("SetAgyOptions: %v", err)
	}

	cmd, embedded := inst.buildAgyCommandWithMessage("agy", "fix tests")
	if !embedded {
		t.Fatalf("expected bare agy launcher to embed prompt, cmd=%q", cmd)
	}
	for _, want := range []string{
		"--model", "gemini-2.5-pro",
		"--effort", "high",
		"--mode", "auto",
		"--dangerously-skip-permissions",
		"--conversation", "conv-msg-1",
		"--prompt-interactive", "'fix tests'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q: %s", want, cmd)
		}
	}

	// Shell pipeline must not embed --prompt-interactive.
	pipeCmd, pipeEmbedded := inst.buildAgyCommandWithMessage("agy 2>&1 | tee ~/agy.log", "fix tests")
	if pipeEmbedded || strings.Contains(pipeCmd, "--prompt-interactive") {
		t.Fatalf("shell pipeline must not embed --prompt-interactive: embedded=%v cmd=%q", pipeEmbedded, pipeCmd)
	}
}

func TestAgyTranscriptPath_ExactBindingOnlyNoMtimeLeak(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGY_DATA_DIR", dataDir)

	projectDir := t.TempDir()
	convA := "11111111-1111-1111-1111-111111111111"
	logsDirA := filepath.Join(dataDir, "brain", convA, ".system_generated", "logs")
	if err := os.MkdirAll(logsDirA, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptA := filepath.Join(logsDirA, "transcript.jsonl")
	content := `{"type":"USER_INPUT","content":"hello"}` + "\n" +
		`{"type":"PLANNER_RESPONSE","content":"","thinking":"still thinking"}` + "\n" +
		`{"type":"PLANNER_RESPONSE","content":"final agy answer"}` + "\n"
	if err := os.WriteFile(transcriptA, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Sibling session in the SAME workspace with no bound ID must NOT fall back to mtime.
	unbound := NewInstanceWithTool("agy-unbound", projectDir, "antigravity")
	if got := AgyTranscriptPath(unbound); got != "" {
		t.Fatalf("unbound AgyTranscriptPath() = %q, want empty (no mtime cross-session leak)", got)
	}
	if _, err := unbound.GetLastResponse(); err == nil {
		t.Fatal("unbound GetLastResponse() succeeded unexpectedly; must not read sibling transcript")
	}

	// Bound session reads exact conversation transcript_full.jsonl or transcript.jsonl fallback.
	bound := NewInstanceWithTool("agy-bound", projectDir, "antigravity")
	bound.AgySessionID = convA
	if got := AgyTranscriptPath(bound); got != transcriptA {
		t.Fatalf("AgyTranscriptPath(bound) = %q, want %q", got, transcriptA)
	}
	resp, err := bound.GetLastResponse()
	if err != nil {
		t.Fatalf("bound GetLastResponse: %v", err)
	}
	if resp.Content != "final agy answer" || resp.Tool != "antigravity" || resp.SessionID != convA {
		t.Fatalf("unexpected last response: %+v", resp)
	}

	// When transcript_full.jsonl exists alongside transcript.jsonl, prefer transcript_full.jsonl.
	fullPath := filepath.Join(logsDirA, "transcript_full.jsonl")
	fullContent := `{"type":"PLANNER_RESPONSE","content":"full untruncated answer"}` + "\n"
	if err := os.WriteFile(fullPath, []byte(fullContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := AgyTranscriptPath(bound); got != fullPath {
		t.Fatalf("AgyTranscriptPath(bound) = %q, want %q", got, fullPath)
	}
	respFull, err := bound.GetLastResponse()
	if err != nil {
		t.Fatalf("bound GetLastResponse (full): %v", err)
	}
	if respFull.Content != "full untruncated answer" {
		t.Fatalf("GetLastResponse content = %q, want %q", respFull.Content, "full untruncated answer")
	}
}

func TestExitToShell_AgyFallsBackToShellOnExit(t *testing.T) {
	exitToShellTestEnv(t)

	dir := t.TempDir()
	inst := NewInstanceWithTool("e2s-agy", dir, "antigravity")
	inst.ExitToShell = boolPtr(true)

	wrapped := inst.wrapExitToShell(inst.buildAgyCommand("agy"))
	if !strings.HasSuffix(wrapped, exitToShellSuffix) {
		t.Fatalf("agy command must end with %q when exit_to_shell is on, got:\n%s", exitToShellSuffix, wrapped)
	}

	out := runPaneCommand(t, wrapped, dir, exitToShellFakeEnv(t, "agy"))
	if !strings.Contains(out, "AGENT_RAN") {
		t.Fatalf("agy did not run, output:\n%s", out)
	}
	if !strings.Contains(out, "SHELL_FALLBACK -i") {
		t.Fatalf("pane did not fall back to an interactive shell after agy exited, output:\n%s", out)
	}
}

func TestBuildAgyCommand_SessionModesAndExtendedOptions(t *testing.T) {
	for _, section := range []string{"antigravity", "agy"} {
		t.Run(section, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("AGENTDECK_PROFILE", "agy-opts-test")
			ClearUserConfigCache()
			t.Cleanup(ClearUserConfigCache)

			cfgDir := filepath.Join(home, ".agent-deck")
			if err := os.MkdirAll(cfgDir, 0o700); err != nil {
				t.Fatal(err)
			}
			cfgTOML := "\n[" + section + "]\n" + `
default_model = "gemini-2.5-pro"
effort = "high"
mode = "plan"
dangerous_mode = true
sandbox = true
agent = "architect"
extra_args = ["--remote-control", "--project", "my proj"]
`
			if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfgTOML), 0o600); err != nil {
				t.Fatal(err)
			}
			ClearUserConfigCache()

			userCfg, err := LoadUserConfig()
			if err != nil {
				t.Fatalf("LoadUserConfig: %v", err)
			}
			defaults := NewAgyOptions(userCfg)
			if !defaults.Sandbox || defaults.Agent != "architect" || defaults.Mode != "plan" || !defaults.DangerousMode {
				t.Fatalf("NewAgyOptions defaults mismatch: %+v", defaults)
			}
			if len(defaults.ExtraArgs) != 3 || defaults.ExtraArgs[2] != "my proj" {
				t.Fatalf("NewAgyOptions ExtraArgs = %v, want 3 entries", defaults.ExtraArgs)
			}

			// Verify extended flags + quoted ExtraArgs + AdditionalPaths + ParentProjectPath.
			inst := NewInstanceWithTool("agy-ext", t.TempDir(), "antigravity")
			inst.ParentProjectPath = "/parent/dir one"
			inst.AdditionalPaths = []string{"/extra/repo two"}
			if err := inst.SetAgyOptions(&AgyOptions{
				SessionMode:   "new",
				Mode:          "accept-edits",
				DangerousMode: true,
				Sandbox:       true,
				Agent:         "reviewer",
				ExtraArgs:     []string{"--remote-control", "--project", "my proj"},
			}); err != nil {
				t.Fatalf("SetAgyOptions: %v", err)
			}

			cmd := inst.buildAgyCommand("agy")
			for _, want := range []string{
				"--model gemini-2.5-pro",
				"--effort high",
				"--mode accept-edits",
				"--agent reviewer",
				"--dangerously-skip-permissions",
				"--sandbox",
				"--add-dir '/parent/dir one'",
				"--add-dir '/extra/repo two'",
				"--remote-control",
				"--project 'my proj'",
			} {
				if !strings.Contains(cmd, want) {
					t.Errorf("command missing %q, got:\n%s", want, cmd)
				}
			}

			// Continue mode emits --continue while unbound and flips launchesFresh=false.
			instContinue := NewInstanceWithTool("agy-cont", t.TempDir(), "antigravity")
			if err := instContinue.SetAgyOptions(&AgyOptions{SessionMode: "continue"}); err != nil {
				t.Fatalf("SetAgyOptions continue: %v", err)
			}
			contCmd := instContinue.buildAgyCommand("agy")
			if !strings.Contains(contCmd, " --continue") {
				t.Fatalf("continue mode expected --continue when unbound, got: %q", contCmd)
			}
			if instContinue.launchesFresh.Load() {
				t.Fatal("continue mode should set launchesFresh=false")
			}

			// Once bound, verified --conversation ID takes precedence and --continue is omitted.
			instContinue.AgySessionID = "conv-bound-123"
			reboundCmd := instContinue.buildAgyCommand("agy")
			if !strings.Contains(reboundCmd, "--conversation conv-bound-123") || strings.Contains(reboundCmd, "--continue") {
				t.Fatalf("bound continue mode should switch to --conversation conv-bound-123 without --continue, got: %q", reboundCmd)
			}
		})
	}
}

func TestIsValidAgyConversationID(t *testing.T) {
	valid := []string{"aafbb74d-9089-4066-9102-f9df37ff2809", "conv_123.abc"}
	for _, id := range valid {
		if !IsValidAgyConversationID(id) {
			t.Errorf("IsValidAgyConversationID(%q) = false, want true", id)
		}
	}
	invalid := []string{"", "   ", "bad id", "bad;rm", "quote'id", strings.Repeat("a", 129)}
	for _, id := range invalid {
		if IsValidAgyConversationID(id) {
			t.Errorf("IsValidAgyConversationID(%q) = true, want false", id)
		}
	}
}
