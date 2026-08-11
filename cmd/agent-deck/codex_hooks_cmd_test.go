package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	os.Args = []string{"agent-deck", "codex-notify", `{"event":"turn/started","thread_id":"thr-sticky"}`}
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

func TestGetCodexConfigPath_UsesCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))

	got := getCodexConfigPath()
	if !strings.HasSuffix(got, filepath.Join("codex-home", "config.toml")) {
		t.Fatalf("getCodexConfigPath() = %q, expected suffix codex-home/config.toml", got)
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

func TestHandleCodexNotify_LifecyclePayloadsWriteStatusWithoutStdout(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		status    string
		sessionID string
		event     string
	}{
		{
			name:      "prompt submit",
			payload:   `{"hook_event_name":"UserPromptSubmit","session_id":"session-prompt"}`,
			status:    "running",
			sessionID: "session-prompt",
			event:     "UserPromptSubmit",
		},
		{
			name:      "stop",
			payload:   `{"hook_event_name":"Stop","thread_id":"thread-stop"}`,
			status:    "waiting",
			sessionID: "thread-stop",
			event:     "Stop",
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
			if hook.Status != tt.status {
				t.Fatalf("status = %q, want %q", hook.Status, tt.status)
			}
			if hook.SessionID != tt.sessionID {
				t.Fatalf("session id = %q, want %q", hook.SessionID, tt.sessionID)
			}
			if hook.Event != tt.event {
				t.Fatalf("event = %q, want %q", hook.Event, tt.event)
			}
		})
	}
}

func TestHandleCodexNotify_UnknownLifecycleEventIsNoop(t *testing.T) {
	instanceID := "inst-post-tool"
	setupCodexNotifyInvocation(t, instanceID, []string{"agent-deck", "codex-notify"}, []byte(`{"hook_event_name":"PostToolUse","session_id":"session-post-tool"}`))

	if output := captureCodexNotifyStdout(t, handleCodexNotify); output != "" {
		t.Fatalf("stdout = %q, want empty", output)
	}
	_, err := os.Stat(filepath.Join(getHooksDir(), instanceID+".json"))
	if !os.IsNotExist(err) {
		t.Fatalf("unexpected hook status file, stat err = %v", err)
	}
}

func TestHandleCodexNotify_RejectsOversizedPayloads(t *testing.T) {
	overflow := strings.Repeat("x", maxHookPayloadSize+1)
	overflowJSON := `{"hook_event_name":"Stop","prompt":"` + overflow + `"}`

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
			name:  "argv json",
			args:  []string{"agent-deck", "codex-notify", overflowJSON},
			input: nil,
		},
		{
			name:  "plain argv event plus stdin",
			args:  []string{"agent-deck", "codex-notify", "Stop"},
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
	if countCodexLifecycleHandlers(t, second, "Stop") != 1 {
		t.Fatal("expected exactly one Stop Agent Deck handler")
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
	hooksBefore := []byte(`{"hooks":{"Stop":{}}}`)
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
    "Stop": [
      {"hooks": [{"type": "command", "command": "agent-deck codex-notify", "timeout": 3}]}
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
	if countCodexLifecycleHandlers(t, hooksAfter, "UserPromptSubmit") != 0 || countCodexLifecycleHandlers(t, hooksAfter, "Stop") != 0 {
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
