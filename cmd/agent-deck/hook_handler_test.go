package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func TestMapEventToStatus(t *testing.T) {
	tests := []struct {
		event  string
		expect string
	}{
		{"SessionStart", "waiting"},
		{"BeforeAgent", "running"},
		{"AfterAgent", "waiting"},
		{"UserPromptSubmit", "running"},
		{"PreToolUse", "running"},
		{"Stop", "waiting"},
		{"PermissionRequest", "waiting"},
		{"Notification", ""},
		{"SessionEnd", "dead"},
		{"PreCompact", ""},
		{"UnknownEvent", ""},
		// Hermes shell hook events
		{"pre_llm_call", "running"},
		{"post_llm_call", "waiting"},
		{"pre_tool_call", "running"},
		// Hermes-only key: mid-turn, a finished tool call means the LLM keeps
		// working; the turn-end waiting edge belongs to post_llm_call.
		{"post_tool_call", "running"},
		// Claude/Cursor post-tool events keep mapping to waiting.
		{"PostToolUse", "waiting"},
		{"on_session_start", "waiting"},
		// Hermes fires on_session_end after EVERY run_conversation call (once
		// per user message), not at process exit — it is the turn-end edge,
		// and the only one an interrupted turn gets (post_llm_call is skipped
		// on interrupt). The real process-exit event is on_session_finalize.
		{"on_session_end", "waiting"},
		{"on_session_finalize", "dead"},
		// Per-API-call heartbeat: keeps "running" fresh through long
		// multi-step turns that would outlive the hook freshness window.
		{"pre_api_request", "running"},
		{"post_api_request", "running"},
		{"subagent_stop", ""},
		// Cursor Agent CLI hook events (camelCase)
		{"sessionStart", "waiting"},
		{"beforeSubmitPrompt", "running"},
		{"preToolUse", "running"},
		{"postToolUse", "waiting"},
		{"postToolUseFailure", "waiting"},
		{"stop", "waiting"},
		{"sessionEnd", "dead"},
	}

	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			got := mapEventToStatus(tt.event)
			if got != tt.expect {
				t.Errorf("mapEventToStatus(%q) = %q, want %q", tt.event, got, tt.expect)
			}
		})
	}
}

func TestHookStatusFile_JSON(t *testing.T) {
	sf := hookStatusFile{
		Status:    "running",
		SessionID: "abc-123",
		Event:     "UserPromptSubmit",
		Timestamp: 1707900000,
	}

	data, err := json.Marshal(sf)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var read hookStatusFile
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if read.Status != "running" {
		t.Errorf("Status = %q, want running", read.Status)
	}
	if read.SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want abc-123", read.SessionID)
	}
	if read.Event != "UserPromptSubmit" {
		t.Errorf("Event = %q, want UserPromptSubmit", read.Event)
	}
	if read.Timestamp != 1707900000 {
		t.Errorf("Timestamp = %d, want 1707900000", read.Timestamp)
	}
}

func TestHookHandler_WritesStatusFile(t *testing.T) {
	tmpDir := t.TempDir()
	hooksDir := filepath.Join(tmpDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks dir: %v", err)
	}

	instanceID := "test-instance-123"
	sf := hookStatusFile{
		Status:    "waiting",
		SessionID: "sess-456",
		Event:     "PermissionRequest",
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(sf)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Simulate atomic write
	filePath := filepath.Join(hooksDir, instanceID+".json")
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		t.Fatalf("Failed to write tmp: %v", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		t.Fatalf("Failed to rename: %v", err)
	}

	// Verify file exists and has correct content
	readData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read back: %v", err)
	}

	var read hookStatusFile
	if err := json.Unmarshal(readData, &read); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if read.Status != "waiting" {
		t.Errorf("Status = %q, want waiting", read.Status)
	}
	if read.Event != "PermissionRequest" {
		t.Errorf("Event = %q, want PermissionRequest", read.Event)
	}
}

func TestHookHandler_MissingInstanceID(t *testing.T) {
	// When AGENTDECK_INSTANCE_ID is not set, handleHookHandler should return silently
	os.Unsetenv("AGENTDECK_INSTANCE_ID")

	// This should not panic or produce any output
	handleHookHandler()
}

func TestHookPayload_Unmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		event   string
		session string
	}{
		{
			name:    "SessionStart",
			input:   `{"hook_event_name":"SessionStart","session_id":"abc-123","source":"claude"}`,
			event:   "SessionStart",
			session: "abc-123",
		},
		{
			name:    "Stop",
			input:   `{"hook_event_name":"Stop","session_id":"def-456"}`,
			event:   "Stop",
			session: "def-456",
		},
		{
			name:    "unknown fields ignored",
			input:   `{"hook_event_name":"UserPromptSubmit","session_id":"ghi-789","extra":"ignored"}`,
			event:   "UserPromptSubmit",
			session: "ghi-789",
		},
		{
			name:    "Cursor stop with conversation_id",
			input:   `{"hook_event_name":"stop","conversation_id":"conv-123"}`,
			event:   "stop",
			session: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p hookPayload
			if err := json.Unmarshal([]byte(tt.input), &p); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if p.HookEventName != tt.event {
				t.Errorf("HookEventName = %q, want %q", p.HookEventName, tt.event)
			}
			if p.SessionID != tt.session {
				t.Errorf("SessionID = %q, want %q", p.SessionID, tt.session)
			}
		})
	}
}

func TestWriteHookStatus_EmptyEventDoesNotBackfillJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	instanceID := "inst-sticky"
	writeHookStatus(instanceID, "waiting", "sess-1", "SessionStart", "")
	writeHookStatus(instanceID, "running", "", "UserPromptSubmit", "")

	data, err := os.ReadFile(filepath.Join(getHooksDir(), instanceID+".json"))
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
	if got := session.ReadHookSessionAnchor(instanceID); got != "sess-1" {
		t.Fatalf("session anchor = %q, want sess-1", got)
	}
}

func TestWriteHookStatus_ClearsStickySessionOnSessionEnd(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	instanceID := "inst-end"
	writeHookStatus(instanceID, "waiting", "sess-2", "SessionStart", "")
	writeHookStatus(instanceID, "dead", "", "SessionEnd", "")
	writeHookStatus(instanceID, "waiting", "", "Stop", "")

	data, err := os.ReadFile(filepath.Join(getHooksDir(), instanceID+".json"))
	if err != nil {
		t.Fatalf("read hook file: %v", err)
	}
	var hook hookStatusFile
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	if hook.SessionID != "" {
		t.Fatalf("hook session_id = %q, want empty after SessionEnd cleanup", hook.SessionID)
	}
	if got := session.ReadHookSessionAnchor(instanceID); got != "" {
		t.Fatalf("session anchor = %q, want empty after SessionEnd cleanup", got)
	}
}

func TestWriteHookStatus_StopDoesNotClearStickySession(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	instanceID := "inst-stop"
	writeHookStatus(instanceID, "waiting", "sess-3", "SessionStart", "")
	writeHookStatus(instanceID, "waiting", "", "Stop", "")

	if got := session.ReadHookSessionAnchor(instanceID); got != "sess-3" {
		t.Fatalf("session anchor = %q, want sess-3", got)
	}
}

func TestIsTerminalHookEvent(t *testing.T) {
	tests := []struct {
		event  string
		expect bool
	}{
		{event: "SessionEnd", expect: true},
		{event: "session_end", expect: true},
		{event: "on_session_end", expect: false}, // Hermes per-turn event
		{event: "on_session_finalize", expect: true},
		{event: "session.closed", expect: true},
		{event: "ThreadClosed", expect: true},
		{event: "thread/terminated", expect: true},
		{event: "thread_done", expect: true},
		{event: "thread-exit", expect: true},
		{event: "Stop", expect: false},
		{event: "UserPromptSubmit", expect: false},
		{event: "thread_suspended", expect: false},
		{event: "session_pending", expect: false},
	}

	for _, tt := range tests {
		if got := isTerminalHookEvent(tt.event); got != tt.expect {
			t.Fatalf("isTerminalHookEvent(%q) = %v, want %v", tt.event, got, tt.expect)
		}
	}
}

// TestParentIsDSP_EnvVarOverride verifies the explicit env-var path of the DSP
// detection used by handleHookHandler to emit a PermissionRequest allow.
// The env-var path is the cross-platform fallback when /proc is unavailable
// (macOS) or when the launch site wants to declare DSP intent explicitly.
func TestParentIsDSP_EnvVarOverride(t *testing.T) {
	t.Setenv("AGENTDECK_DSP_MODE", "1")
	if !parentIsDSP() {
		t.Errorf("parentIsDSP() = false; expected true when AGENTDECK_DSP_MODE=1")
	}
}

// TestParentIsDSP_NoOverrideNoFlag verifies that without the env var override
// AND without --dangerously-skip-permissions in the parent's cmdline,
// parentIsDSP returns false. The Go test runner is not launched with DSP, so
// /proc/<ppid>/cmdline should not contain the flag in normal CI.
func TestParentIsDSP_NoOverrideNoFlag(t *testing.T) {
	t.Setenv("AGENTDECK_DSP_MODE", "")
	if parentIsDSP() {
		t.Skipf("parentIsDSP() returned true unexpectedly; the test runner's parent appears to have --dangerously-skip-permissions in its cmdline. Skipping the negative assertion.")
	}
}

func TestAgyHookHandler_CamelCaseEventsAndOutputs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTDECK_PROFILE", "agy-hook-test")
	t.Setenv("AGENTDECK_INSTANCE_ID", "inst-agy-1")
	t.Setenv("AGENTDECK_STOP_SYNC", "1")
	session.ClearUserConfigCache()
	t.Cleanup(session.ClearUserConfigCache)

	for _, tc := range []struct {
		name       string
		payload    string
		wantStatus string
	}{
		{
			name:       "preInvocation sets running and emits JSON",
			payload:    `{"hookEventName":"preInvocation","conversationId":"agy-cid-1","invocationNum":1}`,
			wantStatus: "running",
		},
		{
			name:       "postInvocation keeps running and emits JSON",
			payload:    `{"hookEventName":"postInvocation","conversationId":"agy-cid-1"}`,
			wantStatus: "running",
		},
		{
			name:       "stop with continuationActive=true emits {} and sets waiting",
			payload:    `{"hookEventName":"stop","conversationId":"agy-cid-1","continuationActive":true}`,
			wantStatus: "waiting",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				runHookHandlerWithStdin(t, tc.payload)
			})
			trimmed := strings.TrimSpace(out)
			if trimmed == "" || !json.Valid([]byte(trimmed)) {
				t.Fatalf("agy hook stdout must be valid JSON, got %q", out)
			}
			raw, err := os.ReadFile(filepath.Join(session.GetHooksDir(), "inst-agy-1.json"))
			if err != nil {
				t.Fatalf("read hook status file: %v", err)
			}
			var sf hookStatusFile
			if err := json.Unmarshal(raw, &sf); err != nil {
				t.Fatalf("unmarshal hook status: %v", err)
			}
			if sf.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", sf.Status, tc.wantStatus)
			}
			if sf.SessionID != "agy-cid-1" {
				t.Fatalf("session_id = %q, want agy-cid-1", sf.SessionID)
			}
		})
	}
}

func TestAgyContextJSON_Shape(t *testing.T) {
	raw := agyContextJSON("hello from conductor")
	var parsed struct {
		InjectSteps []struct {
			SystemMessage struct {
				SystemMessage string `json:"systemMessage"`
			} `json:"systemMessage"`
		} `json:"injectSteps"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("agyContextJSON invalid JSON (%v): %s", err, raw)
	}
	if len(parsed.InjectSteps) != 1 || parsed.InjectSteps[0].SystemMessage.SystemMessage != "hello from conductor" {
		t.Fatalf("unexpected injectSteps payload: %s", raw)
	}
}
