package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRequiresTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig() = nil error, want missing targets error")
	}
}

func TestLoadConfigRejectsEmptyTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"targets":[[]]}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig() = nil error, want empty target error")
	}
}

func TestLoadConfigParsesTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	cfg := config{
		Targets: [][]string{
			{"notify-a", "--foo"},
			{"notify-b"},
		},
		ContinueOnError: true,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(got.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(got.Targets))
	}
	if got.Targets[0][0] != "notify-a" || got.Targets[1][0] != "notify-b" {
		t.Fatalf("Targets = %#v, want parsed targets", got.Targets)
	}
	if !got.ContinueOnError {
		t.Fatal("ContinueOnError = false, want true")
	}
}
