package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeOpenCodePluginPhase0Run_PassWithObservedEvidence(t *testing.T) {
	run := openCodePluginPhase0Run{
		InstanceID:      "inst-123",
		ExitCode:        1,
		HookJSONPresent: true,
		HookSIDPresent:  true,
		HookSIDValue:    "ses_123",
		HookStatus: &openCodePluginHookStatusFile{
			SessionID: "ses_123",
		},
		Records: []openCodePluginPhase0Record{
			{Kind: "plugin_init", InstanceID: "inst-123"},
			{Kind: "event", InstanceID: "inst-123", EventType: "session.updated", SessionID: "ses_123", SessionField: "properties.sessionID"},
		},
	}

	summarizeOpenCodePluginPhase0Run(&run)

	if !run.Pass {
		t.Fatalf("expected pass, got failures: %v", run.FailureReasons)
	}
	if run.ObservedSessionID != "ses_123" {
		t.Fatalf("ObservedSessionID = %q, want ses_123", run.ObservedSessionID)
	}
	if !run.HookJSONMatches || !run.HookSIDMatches {
		t.Fatalf("expected hook artifacts to match observed session ID, got json=%t sid=%t", run.HookJSONMatches, run.HookSIDMatches)
	}
}

func TestSummarizeOpenCodePluginPhase0Run_FailsWhenEvidenceMissing(t *testing.T) {
	run := openCodePluginPhase0Run{
		InstanceID: "inst-456",
		ExitCode:   1,
	}

	summarizeOpenCodePluginPhase0Run(&run)

	if run.Pass {
		t.Fatal("expected failure when proof evidence is missing")
	}
	if len(run.FailureReasons) == 0 {
		t.Fatal("expected concrete failure reasons")
	}
}

func TestSummarizeOpenCodePluginPhase0Run_FailsWhenHookArtifactsMismatch(t *testing.T) {
	run := openCodePluginPhase0Run{
		InstanceID:      "inst-789",
		ExitCode:        0,
		HookJSONPresent: true,
		HookSIDPresent:  true,
		HookSIDValue:    "ses_wrong",
		HookStatus: &openCodePluginHookStatusFile{
			SessionID: "ses_other",
		},
		Records: []openCodePluginPhase0Record{
			{Kind: "plugin_init", InstanceID: "inst-789"},
			{Kind: "event", InstanceID: "inst-789", EventType: "message.updated", SessionID: "ses_expected", SessionField: "properties.sessionID"},
		},
	}

	summarizeOpenCodePluginPhase0Run(&run)

	if run.Pass {
		t.Fatal("expected failure when hook artifacts do not match observed session ID")
	}
	joined := strings.Join(run.FailureReasons, "\n")
	if !strings.Contains(joined, "hook JSON session_id does not match observed sessionID") {
		t.Fatalf("expected hook JSON mismatch failure, got %v", run.FailureReasons)
	}
	if !strings.Contains(joined, ".sid anchor does not match observed sessionID") {
		t.Fatalf("expected hook SID mismatch failure, got %v", run.FailureReasons)
	}
}

func TestCloneOpenCodePluginPhase0Env_CreatesIndependentRunRoots(t *testing.T) {
	prepared := &openCodePluginPhase0PreparedEnv{
		tempRoot:      t.TempDir(),
		tempConfigDir: filepath.Join(t.TempDir(), ".config", "opencode"),
		tempDataDir:   filepath.Join(t.TempDir(), ".local", "share", "opencode"),
		tempCacheDir:  filepath.Join(t.TempDir(), ".cache", "opencode"),
		tempStateDir:  filepath.Join(t.TempDir(), ".local", "state", "opencode"),
	}
	for _, item := range []struct {
		path string
		file string
		body string
	}{
		{path: prepared.tempConfigDir, file: "plugins/proof.js", body: "seed-plugin"},
		{path: prepared.tempDataDir, file: "history.json", body: "seed-data"},
		{path: prepared.tempCacheDir, file: "cache.txt", body: "seed-cache"},
		{path: prepared.tempStateDir, file: "state.txt", body: "seed-state"},
	} {
		if err := os.MkdirAll(filepath.Join(item.path, filepath.Dir(item.file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(item.path, item.file), []byte(item.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	hostEnv, err := cloneOpenCodePluginPhase0Env(prepared, filepath.Join(prepared.tempRoot, "host"))
	if err != nil {
		t.Fatal(err)
	}
	sandboxEnv, err := cloneOpenCodePluginPhase0Env(prepared, filepath.Join(prepared.tempRoot, "sandbox"))
	if err != nil {
		t.Fatal(err)
	}

	hostHistory := filepath.Join(hostEnv.tempDataDir, "history.json")
	if err := os.WriteFile(hostHistory, []byte("host-only"), 0o644); err != nil {
		t.Fatal(err)
	}

	sandboxData, err := os.ReadFile(filepath.Join(sandboxEnv.tempDataDir, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sandboxData) != "seed-data" {
		t.Fatalf("sandbox clone was polluted by host mutation: %q", string(sandboxData))
	}
}

func TestBuildOpenCodePluginPhase0HostEnv_UsesRunSpecificClone(t *testing.T) {
	prepared := &openCodePluginPhase0PreparedEnv{
		tempRoot:      filepath.Join(t.TempDir(), "prepared-root"),
		tempConfigDir: filepath.Join(t.TempDir(), ".config", "opencode"),
		tempDataDir:   filepath.Join(t.TempDir(), ".local", "share", "opencode"),
		tempCacheDir:  filepath.Join(t.TempDir(), ".cache", "opencode"),
		tempStateDir:  filepath.Join(t.TempDir(), ".local", "state", "opencode"),
	}
	runEnv := &openCodePluginPhase0PreparedEnv{
		tempRoot:      filepath.Join(prepared.tempRoot, "host"),
		tempConfigDir: filepath.Join(prepared.tempRoot, "host", ".config", "opencode"),
		tempDataDir:   filepath.Join(prepared.tempRoot, "host", ".local", "share", "opencode"),
		tempCacheDir:  filepath.Join(prepared.tempRoot, "host", ".cache", "opencode"),
		tempStateDir:  filepath.Join(prepared.tempRoot, "host", ".local", "state", "opencode"),
	}

	env := buildOpenCodePluginPhase0HostEnv(runEnv, "inst-host", "/tmp/report.jsonl", "/tmp/hooks")

	wantHome := filepath.Join(prepared.tempRoot, "host")
	if got := lookupEnvValue(env, "HOME"); got != wantHome {
		t.Fatalf("HOME = %q, want %q", got, wantHome)
	}
	if got := lookupEnvValue(env, "XDG_CONFIG_HOME"); got != filepath.Join(wantHome, ".config") {
		t.Fatalf("XDG_CONFIG_HOME = %q, want %q", got, filepath.Join(wantHome, ".config"))
	}
	if got := lookupEnvValue(env, "XDG_DATA_HOME"); got != filepath.Join(wantHome, ".local", "share") {
		t.Fatalf("XDG_DATA_HOME = %q, want %q", got, filepath.Join(wantHome, ".local", "share"))
	}
	if got := lookupEnvValue(env, "XDG_CACHE_HOME"); got != filepath.Join(wantHome, ".cache") {
		t.Fatalf("XDG_CACHE_HOME = %q, want %q", got, filepath.Join(wantHome, ".cache"))
	}
	if got := lookupEnvValue(env, "XDG_STATE_HOME"); got != filepath.Join(wantHome, ".local", "state") {
		t.Fatalf("XDG_STATE_HOME = %q, want %q", got, filepath.Join(wantHome, ".local", "state"))
	}
}

func lookupEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func TestCopyDirTree_SkipsPluginsTopLevel(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.MkdirAll(filepath.Join(src, "plugins", "proof"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "plugins", "proof", "index.js"), []byte("plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "opencode.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyDirTree(src, dst, map[string]bool{"plugins": true}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dst, "plugins")); !os.IsNotExist(err) {
		t.Fatalf("plugins directory should be skipped, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "opencode.json")); err != nil {
		t.Fatalf("expected opencode.json to be copied: %v", err)
	}
}
