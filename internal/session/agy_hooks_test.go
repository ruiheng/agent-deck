package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectAgyHooks_FreshAndPreservesPeers(t *testing.T) {
	cfgDir := t.TempDir()
	existing := `{
  "other-tool": {
    "Stop": [{ "type": "command", "command": "./custom-stop.sh" }]
  }
}`
	if err := os.WriteFile(filepath.Join(cfgDir, "hooks.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InjectAgyHooks(cfgDir)
	if err != nil {
		t.Fatalf("InjectAgyHooks: %v", err)
	}
	if !installed {
		t.Fatal("expected InjectAgyHooks to report installed=true")
	}
	if !CheckAgyHooksInstalled(cfgDir) {
		t.Fatal("CheckAgyHooksInstalled = false after InjectAgyHooks")
	}

	data, err := os.ReadFile(filepath.Join(cfgDir, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["other-tool"]; !ok {
		t.Fatal("existing named hook 'other-tool' was stripped")
	}
	if !strings.Contains(string(data), agentDeckAgyStopCommand) {
		t.Fatalf("expected Stop hook command %q in hooks.json: %s", agentDeckAgyStopCommand, string(data))
	}

	// Idempotent second call.
	installed2, err := InjectAgyHooks(cfgDir)
	if err != nil {
		t.Fatalf("second InjectAgyHooks: %v", err)
	}
	if installed2 {
		t.Fatal("expected second InjectAgyHooks to be no-op (false)")
	}
}

func TestRemoveAgyHooks_AndAutoInstallOptOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ClearUserConfigCache()
	t.Cleanup(ClearUserConfigCache)

	cfgDir := t.TempDir()
	if _, err := InjectAgyHooks(cfgDir); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveAgyHooks(cfgDir)
	if err != nil {
		t.Fatalf("RemoveAgyHooks: %v", err)
	}
	if !removed {
		t.Fatal("expected RemoveAgyHooks to report removed=true")
	}
	if CheckAgyHooksInstalled(cfgDir) {
		t.Fatal("hooks still reported installed after removal")
	}

	// SetAgyHooksEnabled(false) persists [antigravity] hooks_enabled = false and blocks AutoInstallAgyHooks.
	if err := SetAgyHooksEnabled(false); err != nil {
		t.Fatalf("SetAgyHooksEnabled(false): %v", err)
	}
	ClearUserConfigCache()
	cfg, err := LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.EffectiveAntigravitySettings().GetHooksEnabled() {
		t.Fatal("expected Antigravity.GetHooksEnabled() = false after opt-out")
	}
	autoInstalled, err := AutoInstallAgyHooks(cfg, cfgDir)
	if err != nil {
		t.Fatalf("AutoInstallAgyHooks: %v", err)
	}
	if autoInstalled {
		t.Fatal("AutoInstallAgyHooks must respect [antigravity] hooks_enabled = false")
	}
	if CheckAgyHooksInstalled(cfgDir) {
		t.Fatal("hooks must not be installed when [antigravity] hooks_enabled = false")
	}

	// Legacy [agy] hooks_enabled = false also blocks AutoInstallAgyHooks.
	disabled := false
	legacyCfg := &UserConfig{Agy: AntigravitySettings{HooksEnabled: &disabled}}
	legacyInstalled, err := AutoInstallAgyHooks(legacyCfg, cfgDir)
	if err != nil {
		t.Fatalf("AutoInstallAgyHooks (legacy [agy]): %v", err)
	}
	if legacyInstalled {
		t.Fatal("AutoInstallAgyHooks must respect legacy [agy] hooks_enabled = false")
	}

	// Re-enable installs normally.
	if err := SetAgyHooksEnabled(true); err != nil {
		t.Fatalf("SetAgyHooksEnabled(true): %v", err)
	}
	ClearUserConfigCache()
	cfg, err = LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig after re-enable: %v", err)
	}
	autoInstalled, err = AutoInstallAgyHooks(cfg, cfgDir)
	if err != nil {
		t.Fatalf("AutoInstallAgyHooks after re-enable: %v", err)
	}
	if !autoInstalled {
		t.Fatal("expected AutoInstallAgyHooks to install once re-enabled")
	}
}
