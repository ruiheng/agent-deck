package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/asheshgoplani/agent-deck/internal/session"
)

func TestMapCodexNotifyToStatus(t *testing.T) {
	tests := []struct {
		event  string
		expect string
	}{
		{"agent-turn-complete", "waiting"},
		{"agent-turn-start", "running"},
		{"AGENT-TURN-COMPLETE", "waiting"},
		{"turn/completed", "waiting"},
		{"turn/started", "running"},
		{"turn.completed", "waiting"},
		{"turn.started", "running"},
		{"turn.failed", "waiting"},
		{"thread.started", "waiting"},
		{"foo turn start bar", "running"},
		{"foo turn complete bar", "waiting"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			got := mapCodexNotifyToStatus(tt.event)
			if got != tt.expect {
				t.Fatalf("mapCodexNotifyToStatus(%q) = %q, want %q", tt.event, got, tt.expect)
			}
		})
	}
}

func TestHandleCodexNotify_WritesStatus(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("AGENTDECK_INSTANCE_ID", "inst-1")
	t.Setenv("CODEX_SESSION_ID", "")

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agent-deck", "codex-notify"}

	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_, _ = w.WriteString(`{"type":"agent-turn-complete","session_id":"abc-123"}`)
	_ = w.Close()
	os.Stdin = r

	handleCodexNotify()

	hookPath := filepath.Join(getHooksDir(), "inst-1.json")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	var hook hookStatusFile
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	if hook.Status != "waiting" {
		t.Fatalf("hook status = %q, want waiting", hook.Status)
	}
	if hook.SessionID != "abc-123" {
		t.Fatalf("hook session_id = %q, want abc-123", hook.SessionID)
	}
}

func TestHandleCodexNotify_ArgPayload(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("AGENTDECK_INSTANCE_ID", "inst-arg")
	t.Setenv("CODEX_SESSION_ID", "")

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agent-deck", "codex-notify", `{"event":"turn/completed","thread_id":"thr-1"}`}

	handleCodexNotify()

	hookPath := filepath.Join(getHooksDir(), "inst-arg.json")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	var hook hookStatusFile
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	if hook.Status != "waiting" {
		t.Fatalf("hook status = %q, want waiting", hook.Status)
	}
	if hook.SessionID != "thr-1" {
		t.Fatalf("hook session_id = %q, want thr-1", hook.SessionID)
	}
}

func TestHandleCodexNotify_JSONRPCMethodPayload(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("AGENTDECK_INSTANCE_ID", "inst-method")
	t.Setenv("CODEX_SESSION_ID", "")

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agent-deck", "codex-notify", `{"method":"turn/completed","params":{"thread_id":"thr-42"}}`}

	handleCodexNotify()

	hookPath := filepath.Join(getHooksDir(), "inst-method.json")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	var hook hookStatusFile
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	if hook.Status != "waiting" {
		t.Fatalf("hook status = %q, want waiting", hook.Status)
	}
	if hook.SessionID != "thr-42" {
		t.Fatalf("hook session_id = %q, want thr-42", hook.SessionID)
	}
}

func TestHandleCodexNotify_EmptyTailEventKeepsJSONEmptyAndPersistsAnchor(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("AGENTDECK_INSTANCE_ID", "inst-sticky")
	t.Setenv("CODEX_SESSION_ID", "")

	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Seed sticky mapping with a thread_id-bearing event.
	os.Args = []string{"agent-deck", "codex-notify", `{"event":"turn/started","thread_id":"thr-sticky","turn_id":"turn-main"}`}
	handleCodexNotify()

	// Tail event has no session_id/thread_id; should backfill from sticky store.
	os.Args = []string{"agent-deck", "codex-notify", `{"event":"turn/completed"}`}
	handleCodexNotify()

	hookPath := filepath.Join(getHooksDir(), "inst-sticky.json")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	var hook hookStatusFile
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	if hook.SessionID != "" {
		t.Fatalf("hook session_id = %q, want empty for compatibility", hook.SessionID)
	}
	if got := session.ReadHookSessionAnchor("inst-sticky"); got != "thr-sticky" {
		t.Fatalf("session anchor = %q, want thr-sticky", got)
	}
	if hook.CodexStartedGeneration == "" || hook.CodexCompletedGeneration != "" {
		t.Fatalf("identity-less completion must fail closed: %#v", hook)
	}
}

func TestWriteCodexHookStatus_NewerStartSupersedesCompletedGeneration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeCodexHookStatus("inst-generation", "running", "thread-1", "turn.started", "turn-1")
	writeCodexHookStatus("inst-generation", "waiting", "thread-1", "turn.completed", "turn-1")
	path := filepath.Join(getHooksDir(), "inst-generation.json")
	var completed hookStatusFile
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &completed); err != nil {
		t.Fatal(err)
	}
	if completed.CodexStartedGeneration != completed.CodexCompletedGeneration {
		t.Fatal("matching completion was not retained")
	}
	writeCodexHookStatus("inst-generation", "running", "thread-1", "turn.started", "turn-2")
	var superseded hookStatusFile
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &superseded); err != nil {
		t.Fatal(err)
	}
	if superseded.CodexStartedGeneration == superseded.CodexCompletedGeneration {
		t.Fatal("new start must supersede old completion evidence")
	}
}

func TestWriteCodexHookStatus_CompletionMustMatchTurnIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENTDECK_HOOKS_DIR", filepath.Join(t.TempDir(), "hooks"))

	writeCodexHookStatus("turn-match", "running", "thread-1", "turn.started", "turn-2")
	path := filepath.Join(getHooksDir(), "turn-match.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeCodexHookStatus("turn-match", "waiting", "thread-old", "turn.completed", "turn-1")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, before) {
		t.Fatalf("out-of-order completion mutated the newer running edge:\n before: %s\n after:  %s", before, data)
	}
	var got hookStatusFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "running" || got.Event != "turn.started" || got.CodexCompletedGeneration != "" {
		t.Fatalf("out-of-order completion changed live turn: %#v", got)
	}
	if anchor := session.ReadHookSessionAnchor("turn-match"); anchor != "thread-1" {
		t.Fatalf("out-of-order completion changed session anchor to %q", anchor)
	}

	writeCodexHookStatus("turn-match", "waiting", "thread-1", "turn.completed", "turn-2")
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.CodexCompletedGeneration == "" || got.CodexCompletedGeneration != got.CodexStartedGeneration {
		t.Fatalf("matching completion did not converge: %#v", got)
	}
}

func TestWriteCodexHookStatus_IdentityLessCompletionFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENTDECK_HOOKS_DIR", filepath.Join(t.TempDir(), "hooks"))
	writeCodexHookStatus("no-turn", "running", "thread-1", "turn.started", "turn-1")
	writeCodexHookStatus("no-turn", "waiting", "thread-1", "agent-turn-complete", "")
	data, err := os.ReadFile(filepath.Join(getHooksDir(), "no-turn.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got hookStatusFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.CodexCompletedGeneration != "" {
		t.Fatalf("identity-less completion converged: %#v", got)
	}
}

func TestWriteCodexHookStatus_LegacyCompletionConvergesWithoutStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENTDECK_HOOKS_DIR", filepath.Join(t.TempDir(), "hooks"))
	writeCodexHookStatus("legacy-complete", "waiting", "thread-1", "agent-turn-complete", "turn-1")
	data, err := os.ReadFile(filepath.Join(getHooksDir(), "legacy-complete.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got hookStatusFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.CodexStartedGeneration == "" || got.CodexStartedGeneration != got.CodexCompletedGeneration {
		t.Fatalf("completion-only notify did not converge: %#v", got)
	}
	if got.CodexStartedSessionID != "thread-1" || got.CodexCompletedSessionID != "thread-1" {
		t.Fatalf("completion-only notify lost session identity: %#v", got)
	}
}

func TestWriteCodexHookStatus_IdentityLessStartClearsPriorCompletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENTDECK_HOOKS_DIR", filepath.Join(t.TempDir(), "hooks"))
	writeCodexHookStatus("stale-pair", "waiting", "thread-1", "agent-turn-complete", "turn-1")
	writeCodexHookStatus("stale-pair", "running", "", "turn.started", "")
	data, err := os.ReadFile(filepath.Join(getHooksDir(), "stale-pair.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got hookStatusFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.CodexStartedGeneration != "" || got.CodexCompletedGeneration != "" ||
		got.CodexStartedSessionID != "" || got.CodexCompletedSessionID != "" {
		t.Fatalf("identity-less start retained stale completion evidence: %#v", got)
	}
}

func TestCleanStaleHookFilesPreservesCodexWriterLockForLiveStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(getHooksDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	instanceID := "codex-live-writer"
	statusPath := filepath.Join(getHooksDir(), instanceID+".json")
	lockPath := filepath.Join(getHooksDir(), instanceID+".codex-writer.lock")
	if err := os.WriteFile(statusPath, []byte(`{"status":"running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer closeChecked(lock)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	cleanStaleHookFiles()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("writer lock for live status was reaped: %v", err)
	}
}

func TestCodexHooksInstallUninstall(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("CODEX_HOME", "")

	handleCodexHooksInstall()

	configPath := getCodexConfigPath()
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, codexNotifyMarkerBegin) {
		t.Fatalf("config missing marker begin")
	}
	if !strings.Contains(text, codexNotifyLine) {
		t.Fatalf("config missing notify line")
	}

	handleCodexHooksUninstall()

	content, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after uninstall: %v", err)
	}
	text = string(content)
	if strings.Contains(text, codexNotifyMarkerBegin) {
		t.Fatalf("expected codex notify block removed, got: %q", text)
	}
}

func TestCodexHooksInstall_UpgradesLegacyTableWithoutMarkers(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("CODEX_HOME", "")

	configPath := getCodexConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := "model = \"gpt-5\"\n\n[notify]\nprogram = [\"agent-deck\", \"codex-notify\"]\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	handleCodexHooksInstall()

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, codexNotifyMarkerBegin) || !strings.Contains(text, codexNotifyLine) {
		t.Fatalf("expected agent-deck notify block after upgrade, got: %q", text)
	}
	if strings.Contains(text, "[notify]") || strings.Contains(text, "program =") {
		t.Fatalf("expected legacy notify table removed, got: %q", text)
	}
}

func TestCodexHooksInstall_UpgradesLegacyMarkerBlock(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("CODEX_HOME", "")

	configPath := getCodexConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := codexNotifyMarkerBegin + "\n[notify]\nprogram = [\"agent-deck\", \"codex-notify\"]\n" + codexNotifyMarkerEnd + "\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	handleCodexHooksInstall()

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, codexNotifyLine) {
		t.Fatalf("expected upgraded notify line, got: %q", text)
	}
	if strings.Contains(text, "[notify]") || strings.Contains(text, "program =") {
		t.Fatalf("expected legacy notify format removed, got: %q", text)
	}
}

func TestPlanCodexNotifyInstall_RelocatesMarkedBlockToTOMLRoot(t *testing.T) {
	misScoped := "[features]\nexperimental = true\n\n" + codexNotifyBlock()

	if state, err := codexNotifyStatus(misScoped); err != nil || state != codexComponentInvalid {
		t.Fatalf("mis-scoped status = %q, err=%v; want INVALID", state, err)
	}
	updated, changed, err := planCodexNotifyInstall(misScoped)
	if err != nil {
		t.Fatalf("plan install: %v", err)
	}
	if !changed {
		t.Fatal("mis-scoped canonical block was left unchanged")
	}
	if blockAt, tableAt := strings.Index(updated, codexNotifyMarkerBegin), strings.Index(updated, "[features]"); blockAt < 0 || tableAt < 0 || blockAt > tableAt {
		t.Fatalf("notify block was not relocated before the first table:\n%s", updated)
	}
	if strings.Count(updated, codexNotifyLine) != 1 {
		t.Fatalf("relocation duplicated notify setting:\n%s", updated)
	}
	if state, err := codexNotifyStatus(updated); err != nil || state != codexComponentInstalled {
		t.Fatalf("relocated status = %q, err=%v; want INSTALLED", state, err)
	}
}

func TestPlanCodexNotifyInstall_PreservesUnmarkedTableSetting(t *testing.T) {
	misScoped := "[features]\n" + codexNotifyLine + "\n"

	if state, err := codexNotifyStatus(misScoped); err != nil || state != codexComponentNotInstalled {
		t.Fatalf("table-scoped status = %q, err=%v; want NOT INSTALLED", state, err)
	}
	updated, changed, err := planCodexNotifyInstall(misScoped)
	if err != nil || !changed {
		t.Fatalf("plan install changed=%v err=%v", changed, err)
	}
	if !strings.Contains(updated, misScoped) {
		t.Fatalf("install changed the user-owned table setting:\n%s", updated)
	}
	if strings.Count(updated, codexNotifyLine) != 2 {
		t.Fatalf("install should prepend its own root setting without moving the table field:\n%s", updated)
	}
}

func TestPlanCodexNotifyUninstall_PreservesUnmarkedNonExactForms(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "inline comment",
			content: `model = "gpt-5"
notify = ["agent-deck", "codex-notify"] # installed manually

[features]
experimental = true
`,
		},
		{
			name: "multiline array",
			content: `model = "gpt-5"
notify = [
  "agent-deck",
  "codex-notify",
]

[features]
experimental = true
`,
		},
		{
			name: "quoted key",
			content: `"notify" = ["agent-deck", "codex-notify"]
model = "gpt-5"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if state, err := codexNotifyStatus(tt.content); err != nil || state != codexComponentCustom {
				t.Fatalf("status = %q, err=%v; want CUSTOM", state, err)
			}
			installed, changed, err := planCodexNotifyInstall(tt.content)
			if err != nil || changed || installed != tt.content {
				t.Fatalf("idempotent install changed=%v err=%v\n%s", changed, err, installed)
			}

			updated, changed, err := planCodexNotifyUninstall(tt.content)
			if err != nil || changed || updated != tt.content {
				t.Fatalf("uninstall changed=%v err=%v", changed, err)
			}
		})
	}
}

func TestPlanCodexNotifyUninstall_RemovesUpstreamExactUnmarkedForm(t *testing.T) {
	const content = "model = \"gpt-5\"\n" + codexNotifyLine + "\n"

	if state, err := codexNotifyStatus(content); err != nil || state != codexComponentInstalled {
		t.Fatalf("status = %q, err=%v; want INSTALLED", state, err)
	}
	updated, changed, err := planCodexNotifyUninstall(content)
	if err != nil || !changed {
		t.Fatalf("uninstall changed=%v err=%v", changed, err)
	}
	if updated != "model = \"gpt-5\"\n" {
		t.Fatalf("unexpected config after uninstall: %q", updated)
	}
}

func TestPlanCodexNotifyInstall_PreservesCanonicalTextInsideMultilineString(t *testing.T) {
	const content = `description = """
notify = ["agent-deck", "codex-notify"]
"""
model = "gpt-5"
`

	updated, changed, err := planCodexNotifyInstall(content)
	if err != nil || !changed {
		t.Fatalf("install changed=%v err=%v", changed, err)
	}
	var decoded map[string]interface{}
	if _, err := toml.Decode(updated, &decoded); err != nil {
		t.Fatalf("updated config is invalid: %v\n%s", err, updated)
	}
	if got := decoded["description"]; got != "notify = [\"agent-deck\", \"codex-notify\"]\n" {
		t.Fatalf("multiline string changed: %#v\n%s", got, updated)
	}
	if _, canonical, err := codexRootNotifyState(updated); err != nil || !canonical {
		t.Fatalf("root notify was not installed: canonical=%v err=%v\n%s", canonical, err, updated)
	}
}

func TestPlanCodexNotifyUninstall_PreservesAssignmentTextInsideMultilineString(t *testing.T) {
	const content = `description = """
notify = ["agent-deck", "codex-notify"]
"""
notify = ["agent-deck", "codex-notify"]
model = "gpt-5"
`

	updated, changed, err := planCodexNotifyUninstall(content)
	if err != nil || !changed {
		t.Fatalf("uninstall changed=%v err=%v", changed, err)
	}
	var decoded map[string]interface{}
	if _, err := toml.Decode(updated, &decoded); err != nil {
		t.Fatalf("updated config is invalid: %v\n%s", err, updated)
	}
	if got := decoded["description"]; got != "notify = [\"agent-deck\", \"codex-notify\"]\n" {
		t.Fatalf("multiline string changed: %#v\n%s", got, updated)
	}
	if _, present := decoded["notify"]; present {
		t.Fatalf("root notify remains after uninstall:\n%s", updated)
	}
}

func TestCodexHookPathsUseEffectiveCodexHome(t *testing.T) {
	for _, tt := range []struct {
		name        string
		profile     string
		codexHome   string
		globalHome  string
		profileHome string
		wantHome    string
	}{
		{name: "CODEX_HOME", codexHome: "env-home", globalHome: "global-home", profileHome: "profile-home", profile: "work", wantHome: "env-home"},
		{name: "profile config", globalHome: "global-home", profileHome: "profile-home", profile: "work", wantHome: "profile-home"},
		{name: "global config", globalHome: "global-home", profile: "work", wantHome: "global-home"},
		{name: "default", profile: "work", wantHome: ".codex"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("AGENTDECK_PROFILE", tt.profile)
			t.Setenv("CODEX_HOME", expandTestPath(home, tt.codexHome))
			session.ClearUserConfigCache()
			t.Cleanup(session.ClearUserConfigCache)

			cfg := &session.UserConfig{}
			if tt.globalHome != "" {
				cfg.Codex.ConfigDir = expandTestPath(home, tt.globalHome)
			}
			if tt.profileHome != "" {
				cfg.Profiles = map[string]session.ProfileSettings{
					tt.profile: {Codex: session.ProfileCodexSettings{ConfigDir: expandTestPath(home, tt.profileHome)}},
				}
			}
			if tt.globalHome != "" || tt.profileHome != "" {
				if err := session.SaveUserConfig(cfg); err != nil {
					t.Fatalf("save user config: %v", err)
				}
				session.ClearUserConfigCache()
			}

			wantHome := filepath.Join(home, tt.wantHome)
			if got := getCodexConfigPath(); got != filepath.Join(wantHome, "config.toml") {
				t.Fatalf("config path = %q, want %q", got, filepath.Join(wantHome, "config.toml"))
			}
			if got := getCodexHooksPath(); got != filepath.Join(wantHome, "hooks.json") {
				t.Fatalf("hooks path = %q, want %q", got, filepath.Join(wantHome, "hooks.json"))
			}
		})
	}
}

func expandTestPath(home, name string) string {
	if name == "" {
		return ""
	}
	return filepath.Join(home, name)
}

func TestCodexSharedStatusWriteNeverMutatesAnchor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	id := "codex-anchor-owner"
	session.WriteHookSessionAnchor(id, "thread-current")
	if !writeHookStatusFile(id, hookStatusFile{Status: "dead", SessionID: "thread-stale", Event: "SessionEnd", Timestamp: 1}, false) {
		t.Fatal("status write failed")
	}
	if got := session.ReadHookSessionAnchor(id); got != "thread-current" {
		t.Fatalf("shared writer mutated Codex anchor: %q", got)
	}
}

func setCodexNotifyStdin(t *testing.T, data []byte) {
	t.Helper()
	input, err := os.CreateTemp(t.TempDir(), "codex-notify-input-*")
	if err != nil {
		t.Fatalf("create input: %v", err)
	}
	if _, err := input.Write(data); err != nil {
		_ = input.Close()
		t.Fatalf("write input: %v", err)
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		_ = input.Close()
		t.Fatalf("rewind input: %v", err)
	}
	original := os.Stdin
	os.Stdin = input
	t.Cleanup(func() {
		os.Stdin = original
		_ = input.Close()
	})
}

func captureCodexNotifyStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = original
		_ = writer.Close()
	}()

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		_ = reader.Close()
		done <- string(data)
	}()

	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	return <-done
}

func setupCodexNotifyInvocation(t *testing.T, instanceID string, args []string, input []byte) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("AGENTDECK_HOOKS_DIR", filepath.Join(t.TempDir(), "hooks"))
	t.Setenv("AGENTDECK_INSTANCE_ID", instanceID)
	t.Setenv("CODEX_SESSION_ID", "")

	originalArgs := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = originalArgs })
	setCodexNotifyStdin(t, input)
}

func readCodexNotifyHook(t *testing.T, instanceID string) hookStatusFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(getHooksDir(), instanceID+".json"))
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	var hook hookStatusFile
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	return hook
}

func countCodexLifecycleHandlers(t *testing.T, data []byte, event string) int {
	t.Helper()
	top, err := decodeJSONObject(data, "test hooks document")
	if err != nil {
		t.Fatal(err)
	}
	rawHooks, ok := top["hooks"]
	if !ok {
		return 0
	}
	hooks, err := decodeJSONObject(rawHooks, "test hooks")
	if err != nil {
		t.Fatal(err)
	}
	rawEvent, ok := hooks[event]
	if !ok {
		return 0
	}
	groups, err := decodeJSONArray(rawEvent, "test event")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for groupIndex, rawGroup := range groups {
		group, err := decodeJSONObject(rawGroup, "test group")
		if err != nil {
			t.Fatal(err)
		}
		rawHandlers, ok := group["hooks"]
		if !ok {
			continue
		}
		handlers, err := decodeJSONArray(rawHandlers, "test handlers")
		if err != nil {
			t.Fatal(err)
		}
		for handlerIndex, rawHandler := range handlers {
			handler, err := decodeJSONObject(rawHandler, "test handler")
			if err != nil {
				t.Fatalf("group %d handler %d: %v", groupIndex, handlerIndex, err)
			}
			if isAgentDeckCodexLifecycleHandler(handler) {
				count++
			}
		}
	}
	return count
}

func TestHandleCodexNotify_LifecyclePayloadsWriteGenerationWithoutStdout(t *testing.T) {
	tests := []struct {
		name                string
		payload             string
		status              string
		sessionID           string
		event               string
		startedGeneration   string
		completedGeneration string
	}{
		{
			name:              "prompt submit",
			payload:           `{"hook_event_name":"UserPromptSubmit","session_id":"session-prompt","turn_id":"turn-prompt"}`,
			status:            "running",
			sessionID:         "session-prompt",
			event:             "UserPromptSubmit",
			startedGeneration: "session-prompt:turn-prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instanceID := "inst-lifecycle"
			setupCodexNotifyInvocation(t, instanceID, []string{"agent-deck", "codex-notify"}, []byte(tt.payload))

			if output := captureCodexNotifyStdout(t, handleCodexNotify); output != "" {
				t.Fatalf("stdout = %q, want empty", output)
			}
			hook := readCodexNotifyHook(t, instanceID)
			if hook.Status != tt.status || hook.SessionID != tt.sessionID || hook.Event != tt.event {
				t.Fatalf("hook = %#v, want status=%q session=%q event=%q", hook, tt.status, tt.sessionID, tt.event)
			}
			if hook.CodexStartedGeneration != tt.startedGeneration || hook.CodexCompletedGeneration != tt.completedGeneration {
				t.Fatalf("generation evidence = %q/%q, want %q/%q", hook.CodexStartedGeneration, hook.CodexCompletedGeneration, tt.startedGeneration, tt.completedGeneration)
			}
		})
	}
}

func TestHandleCodexNotify_NonAuthoritativeLifecycleEventsAreNoop(t *testing.T) {
	for _, payload := range []string{
		`{"hook_event_name":"PostToolUse","session_id":"session-post-tool"}`,
		`{"hook_event_name":"Unknown","session_id":"session-unknown"}`,
	} {
		t.Run(payload, func(t *testing.T) {
			instanceID := "inst-non-authoritative"
			setupCodexNotifyInvocation(t, instanceID, []string{"agent-deck", "codex-notify"}, []byte(payload))

			if output := captureCodexNotifyStdout(t, handleCodexNotify); output != "" {
				t.Fatalf("stdout = %q, want empty", output)
			}
			_, err := os.Stat(filepath.Join(getHooksDir(), instanceID+".json"))
			if !os.IsNotExist(err) {
				t.Fatalf("unexpected hook status file, stat err = %v", err)
			}
		})
	}
}

func TestHandleCodexNotify_RejectsOversizedPayloads(t *testing.T) {
	overflow := strings.Repeat("x", maxHookPayloadSize+1)
	overflowJSON := `{"hook_event_name":"Unknown","prompt":"` + overflow + `"}`

	for _, tt := range []struct {
		name  string
		args  []string
		input []byte
	}{
		{
			name:  "stdin",
			args:  []string{"agent-deck", "codex-notify"},
			input: []byte(overflowJSON),
		},
		{
			name: "argv json",
			args: []string{"agent-deck", "codex-notify", overflowJSON},
		},
		{
			name:  "plain argv event plus stdin",
			args:  []string{"agent-deck", "codex-notify", "Unknown"},
			input: []byte(overflowJSON),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			instanceID := "inst-oversized"
			setupCodexNotifyInvocation(t, instanceID, tt.args, tt.input)

			if output := captureCodexNotifyStdout(t, handleCodexNotify); output != "" {
				t.Fatalf("stdout = %q, want empty", output)
			}
			_, err := os.Stat(filepath.Join(getHooksDir(), instanceID+".json"))
			if !os.IsNotExist(err) {
				t.Fatalf("unexpected hook status file, stat err = %v", err)
			}
			if anchor := session.ReadHookSessionAnchor(instanceID); anchor != "" {
				t.Fatalf("session anchor = %q, want empty", anchor)
			}
		})
	}
}

func TestCodexHooksInstall_IsIdempotentAndPreservesUnrelatedJSON(t *testing.T) {
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	hooksPath := filepath.Join(codexHome, "hooks.json")
	initialHooks := []byte(`{
  "description": "User-maintained hooks.",
  "metadata": {"source": "plugin"},
  "hooks": {
    "UserPromptSubmit": [
      {
        "matcher": "preserve-me",
        "hooks": [{"type": "command", "command": "user-hook", "timeout": 9}]
      }
    ],
    "PostToolUse": [
      {"hooks": [{"type": "command", "command": "other-hook"}]}
    ]
  }
}`)
	if err := os.WriteFile(hooksPath, initialHooks, 0644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	notifyChanged, lifecycleChanged, err := installCodexHooks(configPath, hooksPath)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !notifyChanged || !lifecycleChanged {
		t.Fatalf("first install changed notify=%v lifecycle=%v, want both true", notifyChanged, lifecycleChanged)
	}
	first, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read first hooks: %v", err)
	}

	notifyChanged, lifecycleChanged, err = installCodexHooks(configPath, hooksPath)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if notifyChanged || lifecycleChanged {
		t.Fatalf("second install changed notify=%v lifecycle=%v, want both false", notifyChanged, lifecycleChanged)
	}
	second, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read second hooks: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("second install changed hooks.json bytes")
	}
	if countCodexLifecycleHandlers(t, second, "UserPromptSubmit") != 1 {
		t.Fatal("expected exactly one UserPromptSubmit Agent Deck handler")
	}
	for _, preserved := range []string{`"User-maintained hooks."`, `"source": "plugin"`, `"preserve-me"`, `"user-hook"`, `"PostToolUse"`, `"other-hook"`} {
		if !strings.Contains(string(second), preserved) {
			t.Fatalf("hooks.json lost unrelated content %q: %s", preserved, second)
		}
	}
}

func TestCodexHooksInstall_InvalidLifecyclePreflightDoesNotWriteEitherFile(t *testing.T) {
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	hooksPath := filepath.Join(codexHome, "hooks.json")
	configBefore := []byte("model = \"gpt-5\"\n")
	hooksBefore := []byte(`{"hooks":{"UserPromptSubmit":{}}}`)
	if err := os.WriteFile(configPath, configBefore, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(hooksPath, hooksBefore, 0644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	if _, _, err := installCodexHooks(configPath, hooksPath); err == nil {
		t.Fatal("install succeeded with malformed lifecycle event")
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	hooksAfter, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	if string(configAfter) != string(configBefore) {
		t.Fatalf("config changed despite failed preflight: %q", configAfter)
	}
	if string(hooksAfter) != string(hooksBefore) {
		t.Fatalf("hooks changed despite failed preflight: %q", hooksAfter)
	}
}

func TestCodexHooksUninstall_PreservesUnrelatedHandlersAndDocument(t *testing.T) {
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	hooksPath := filepath.Join(codexHome, "hooks.json")
	if err := os.WriteFile(configPath, []byte(codexNotifyBlock()), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	hooks := []byte(`{
  "description": "Agent Deck Codex lifecycle status hooks.",
  "metadata": {"source": "plugin"},
  "hooks": {
    "UserPromptSubmit": [
      {"hooks": [
        {"type": "command", "command": "agent-deck codex-notify", "timeout": 3},
        {"type": "command", "command": "user-hook", "timeout": 9}
      ]},
      {"matcher": "keep-group", "hooks": [
        {"type": "command", "command": "agent-deck codex-notify", "timeout": 3}
      ]}
    ],
    "PostToolUse": [
      {"hooks": [{"type": "command", "command": "other-hook"}]}
    ]
  }
}`)
	if err := os.WriteFile(hooksPath, hooks, 0644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	notifyChanged, lifecycleChanged, err := uninstallCodexHooks(configPath, hooksPath)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !notifyChanged || !lifecycleChanged {
		t.Fatalf("uninstall changed notify=%v lifecycle=%v, want both true", notifyChanged, lifecycleChanged)
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(configAfter), codexNotifyMarkerBegin) {
		t.Fatalf("config retained notify block: %q", configAfter)
	}
	hooksAfter, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	if countCodexLifecycleHandlers(t, hooksAfter, "UserPromptSubmit") != 0 {
		t.Fatal("uninstall retained an Agent Deck lifecycle handler")
	}
	for _, preserved := range []string{`"metadata"`, `"source": "plugin"`, `"user-hook"`, `"keep-group"`, `"PostToolUse"`, `"other-hook"`} {
		if !strings.Contains(string(hooksAfter), preserved) {
			t.Fatalf("hooks.json lost unrelated content %q: %s", preserved, hooksAfter)
		}
	}
	if strings.Contains(string(hooksAfter), codexLifecycleDescription) {
		t.Fatalf("Agent Deck description remained after all handlers were removed: %s", hooksAfter)
	}
	if _, err := os.Stat(hooksPath); err != nil {
		t.Fatalf("hooks.json was removed: %v", err)
	}
}

func TestCodexHooksStatus_DistinguishesComponentStates(t *testing.T) {
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	hooksPath := filepath.Join(codexHome, "hooks.json")

	overall, notify, lifecycle, err := codexHooksStatus(configPath, hooksPath)
	if err != nil || overall != "NOT INSTALLED" || notify != codexComponentNotInstalled || lifecycle != codexComponentNotInstalled {
		t.Fatalf("empty status = %q/%q/%q err=%v", overall, notify, lifecycle, err)
	}

	if err := os.WriteFile(configPath, []byte(codexNotifyLine+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	overall, notify, lifecycle, err = codexHooksStatus(configPath, hooksPath)
	if err != nil || overall != "PARTIAL" || notify != codexComponentInstalled || lifecycle != codexComponentNotInstalled {
		t.Fatalf("notify-only status = %q/%q/%q err=%v", overall, notify, lifecycle, err)
	}

	installedHooks, changed, err := planCodexLifecycleInstall(nil, false)
	if err != nil || !changed {
		t.Fatalf("plan lifecycle install changed=%v err=%v", changed, err)
	}
	if err := os.WriteFile(hooksPath, installedHooks, 0644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}
	overall, notify, lifecycle, err = codexHooksStatus(configPath, hooksPath)
	if err != nil || overall != "INSTALLED" || notify != codexComponentInstalled || lifecycle != codexComponentInstalled {
		t.Fatalf("installed status = %q/%q/%q err=%v", overall, notify, lifecycle, err)
	}

	if err := os.WriteFile(configPath, nil, 0644); err != nil {
		t.Fatalf("clear config: %v", err)
	}
	overall, notify, lifecycle, err = codexHooksStatus(configPath, hooksPath)
	if err != nil || overall != "PARTIAL" || notify != codexComponentNotInstalled || lifecycle != codexComponentInstalled {
		t.Fatalf("lifecycle-only status = %q/%q/%q err=%v", overall, notify, lifecycle, err)
	}

	if err := os.WriteFile(configPath, []byte("notify = [\"user-command\"]\n"), 0644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}
	overall, notify, lifecycle, err = codexHooksStatus(configPath, hooksPath)
	if err != nil || overall != "PARTIAL" || notify != codexComponentCustom || lifecycle != codexComponentInstalled {
		t.Fatalf("custom notify status = %q/%q/%q err=%v", overall, notify, lifecycle, err)
	}

	if err := os.WriteFile(hooksPath, []byte(`[]`), 0644); err != nil {
		t.Fatalf("write invalid hooks: %v", err)
	}
	overall, _, lifecycle, err = codexHooksStatus(configPath, hooksPath)
	if err == nil || overall != "ERROR" || lifecycle != codexComponentInvalid {
		t.Fatalf("invalid status = %q/%q err=%v", overall, lifecycle, err)
	}
}

func TestCodexHooksUsage_DescribesNotifyAndLifecycleHooks(t *testing.T) {
	var output strings.Builder
	printCodexHooksUsage(&output)
	if !strings.Contains(output.String(), "notify and lifecycle") {
		t.Fatalf("usage does not describe both integrations: %q", output.String())
	}
}
