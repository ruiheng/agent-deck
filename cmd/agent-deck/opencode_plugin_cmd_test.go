package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSummarizeOpenCodePluginPhase0Run_PassWithObservedEvidence(t *testing.T) {
	run := openCodePluginPhase0Run{
		InstanceID:      "inst-123",
		ExitCode:        1,
		HookJSONPresent: true,
		HookSIDPresent:  true,
		Records: []openCodePluginPhase0Record{
			{Kind: "plugin_init", InstanceID: "inst-123"},
			{Kind: "event", InstanceID: "inst-123", EventType: "session.created", SessionID: "ses_123", SessionField: "session.id"},
		},
	}

	summarizeOpenCodePluginPhase0Run(&run)

	if !run.Pass {
		t.Fatalf("expected pass, got failures: %v", run.FailureReasons)
	}
	if run.ObservedSessionID != "ses_123" {
		t.Fatalf("ObservedSessionID = %q, want ses_123", run.ObservedSessionID)
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
