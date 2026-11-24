package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func setupAgyHooksTest(t *testing.T) (home, cfgDir string) {
	t.Helper()
	home = t.TempDir()
	cfgDir = filepath.Join(home, ".gemini", "config")
	t.Setenv("HOME", home)
	t.Setenv("AGY_CONFIG_DIR", cfgDir)
	session.ClearUserConfigCache()
	t.Cleanup(session.ClearUserConfigCache)
	return home, cfgDir
}

func TestHandleAgyHooksInstall_InjectsAndClearsOptOut(t *testing.T) {
	_, cfgDir := setupAgyHooksTest(t)

	if err := session.SetAgyHooksEnabled(false); err != nil {
		t.Fatalf("seed opt-out: %v", err)
	}

	out := captureStdout(t, func() {
		handleAgyHooksInstall(false)
	})
	if !strings.Contains(out, "Antigravity hooks installed successfully.") {
		t.Fatalf("unexpected install output: %q", out)
	}
	if !session.CheckAgyHooksInstalled(cfgDir) {
		t.Fatal("hooks not installed in hooks.json")
	}

	session.ClearUserConfigCache()
	cfg, err := session.LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	eff := cfg.EffectiveAntigravitySettings()
	if !eff.GetHooksEnabled() {
		t.Fatal("expected [antigravity] hooks_enabled opt-out cleared after install")
	}
}

func TestHandleAgyHooksUninstall_SetsOptOutAndRemoves(t *testing.T) {
	_, cfgDir := setupAgyHooksTest(t)
	if _, err := session.InjectAgyHooks(cfgDir); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		handleAgyHooksUninstall(false)
	})
	if !strings.Contains(out, "Antigravity hooks removed successfully.") {
		t.Fatalf("unexpected uninstall output: %q", out)
	}
	if session.CheckAgyHooksInstalled(cfgDir) {
		t.Fatal("hooks still present after uninstall")
	}

	session.ClearUserConfigCache()
	cfg, err := session.LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	eff := cfg.EffectiveAntigravitySettings()
	if eff.GetHooksEnabled() {
		t.Fatal("expected [antigravity] hooks_enabled = false after uninstall")
	}
}

func TestHandleAgyHooksStatus_JSON(t *testing.T) {
	_, cfgDir := setupAgyHooksTest(t)
	if _, err := session.InjectAgyHooks(cfgDir); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		runAgyHooksStatus(true)
	})
	var parsed hooksStatusJSON
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json output invalid (%v): %s", err, out)
	}
	if parsed.Kind != "agy" || parsed.State != "installed" || !strings.HasSuffix(parsed.Config, "hooks.json") {
		t.Fatalf("unexpected status JSON: %+v", parsed)
	}

	if err := session.SetAgyHooksEnabled(false); err != nil {
		t.Fatal(err)
	}
	session.ClearUserConfigCache()
	outDisabled := captureStdout(t, func() {
		runAgyHooksStatus(true)
	})
	var parsedDisabled hooksStatusJSON
	if err := json.Unmarshal([]byte(outDisabled), &parsedDisabled); err != nil {
		t.Fatalf("disabled json output invalid (%v): %s", err, outDisabled)
	}
	if len(parsedDisabled.Details) == 0 || !strings.Contains(parsedDisabled.Details[0], "auto-install disabled") {
		t.Fatalf("expected auto-install disabled detail, got %+v", parsedDisabled)
	}
}

func TestHandleAgyHooks_UsageAndUnknown(t *testing.T) {
	if os.Getenv("AGY_HOOKS_HELPER") == "1" {
		args := strings.Split(os.Getenv("AGY_HOOKS_ARGS"), ",")
		if len(args) == 1 && args[0] == "" {
			args = nil
		}
		handleAgyHooks(args)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleAgyHooks_UsageAndUnknown")
	cmd.Env = append(os.Environ(), "AGY_HOOKS_HELPER=1", "AGY_HOOKS_ARGS=bogus")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for bogus subcommand, got ok: %s", out)
	}
	if !strings.Contains(string(out), "Unknown agy-hooks subcommand: bogus") {
		t.Fatalf("unexpected error output: %s", out)
	}
}
