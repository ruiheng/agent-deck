package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/asheshgoplani/agent-deck/internal/atomicfile"
)

const (
	agyHookEntryKey         = "agent-deck"
	agentDeckAgyHookCommand = "agent-deck hook-handler"
	agentDeckAgyStopCommand = "AGENTDECK_STOP_SYNC=1 agent-deck hook-handler"
)

type agyHookEventSpec struct {
	event   string
	command string
}

// We register session/invocation lifecycle hooks and intentionally omit
// PreToolUse/PostToolUse so agent-deck does not alter agy tool permissions or
// introduce spurious per-tool "waiting" status flickers.
var agyHookEvents = []agyHookEventSpec{
	{event: "SessionStart", command: agentDeckAgyHookCommand},
	{event: "PreInvocation", command: agentDeckAgyHookCommand},
	{event: "PostInvocation", command: agentDeckAgyHookCommand},
	{event: "Stop", command: agentDeckAgyStopCommand},
}

type agyHookHandler struct {
	Type    string `json:"type,omitempty"`
	Command string `json:"command"`
}

// InjectAgyHooks installs agent-deck lifecycle hooks under the top-level
// "agent-deck" named hook in Antigravity CLI's hooks.json (usually
// ~/.gemini/config/hooks.json). Preserves existing named hooks and any custom
// handlers/fields. Returns true if hooks.json was updated, false if already installed.
func InjectAgyHooks(configDir string) (bool, error) {
	hooksPath := filepath.Join(configDir, "hooks.json")

	top, err := readAgyHooksRoot(hooksPath)
	if err != nil {
		return false, err
	}
	if agyHooksAlreadyInstalled(top) {
		return false, nil
	}

	entry := agyNamedHookObject(top)
	if enabledRaw, ok := entry.get("enabled"); ok {
		var enabled bool
		if json.Unmarshal(enabledRaw, &enabled) == nil && !enabled {
			entry.del("enabled")
		}
	}

	for _, spec := range agyHookEvents {
		merged, err := mergeAgyHookEvent(entry, spec.event, spec.command)
		if err != nil {
			return false, fmt.Errorf("merge %s hook: %w", spec.event, err)
		}
		entry.set(spec.event, merged)
	}
	top.set(agyHookEntryKey, mustMarshal(entry))

	finalData, err := indentSettings(top)
	if err != nil {
		return false, fmt.Errorf("marshal hooks.json: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	if err := atomicfile.WriteFile(hooksPath, finalData, 0644); err != nil {
		return false, fmt.Errorf("write hooks.json: %w", err)
	}

	sessionLog.Info("agy_hooks_installed", slog.String("config_dir", configDir))
	return true, nil
}

// AutoInstallAgyHooks silently installs agy hooks on TUI startup unless
// [antigravity] hooks_enabled = false (or legacy [agy] hooks_enabled = false)
// is configured. Returns true if hooks were installed.
func AutoInstallAgyHooks(cfg *UserConfig, configDir string) (bool, error) {
	if cfg != nil {
		eff := cfg.EffectiveAntigravitySettings()
		if !eff.GetHooksEnabled() {
			return false, nil
		}
	}
	if CheckAgyHooksInstalled(configDir) {
		return false, nil
	}
	return InjectAgyHooks(configDir)
}

// SetAgyHooksEnabled persists [antigravity] hooks_enabled to config.toml without
// rewriting the file if the effective setting already matches.
func SetAgyHooksEnabled(enabled bool) error {
	cfg, err := LoadUserConfig()
	if err != nil {
		return err
	}
	eff := cfg.EffectiveAntigravitySettings()
	if eff.GetHooksEnabled() == enabled && cfg.Agy.HooksEnabled == nil {
		return nil
	}
	updated := *cfg
	updated.Agy.HooksEnabled = nil
	if enabled {
		updated.Antigravity.HooksEnabled = nil
	} else {
		disabled := false
		updated.Antigravity.HooksEnabled = &disabled
	}
	return SaveUserConfig(&updated)
}

// RemoveAgyHooks removes agent-deck's hook handlers from agy's hooks.json.
// Preserves other named hooks and custom handlers inside the "agent-deck" map.
// Returns true if entries were removed, false if none were present.
func RemoveAgyHooks(configDir string) (bool, error) {
	hooksPath := filepath.Join(configDir, "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read hooks.json: %w", err)
	}

	top, err := parseAgyHooksRoot(data)
	if err != nil {
		return false, err
	}
	rawEntry, ok := top.get(agyHookEntryKey)
	if !ok {
		return false, nil
	}

	var entry jsonObject
	if err := json.Unmarshal(rawEntry, &entry); err != nil {
		return false, fmt.Errorf("parse %s hook entry: %w", agyHookEntryKey, err)
	}

	removed := false
	for _, spec := range agyHookEvents {
		rawEvent, exists := entry.get(spec.event)
		if !exists {
			continue
		}
		var handlers []json.RawMessage
		if err := json.Unmarshal(rawEvent, &handlers); err != nil {
			continue
		}
		cleaned, didRemove := removeAgentDeckFromAgyEvent(handlers)
		if !didRemove {
			continue
		}
		removed = true
		if len(cleaned) == 0 {
			entry.del(spec.event)
		} else {
			entry.set(spec.event, mustMarshal(cleaned))
		}
	}

	if !removed {
		return false, nil
	}
	if len(entry) == 0 {
		top.del(agyHookEntryKey)
	} else {
		top.set(agyHookEntryKey, mustMarshal(entry))
	}

	finalData, err := indentSettings(top)
	if err != nil {
		return false, fmt.Errorf("marshal hooks.json: %w", err)
	}
	if err := atomicfile.WriteFile(hooksPath, finalData, 0644); err != nil {
		return false, fmt.Errorf("write hooks.json: %w", err)
	}

	sessionLog.Info("agy_hooks_removed", slog.String("config_dir", configDir))
	return true, nil
}

// CheckAgyHooksInstalled reports whether all required agent-deck hooks are
// installed and enabled in agy's hooks.json.
func CheckAgyHooksInstalled(configDir string) bool {
	hooksPath := filepath.Join(configDir, "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		return false
	}
	top, err := parseAgyHooksRoot(data)
	if err != nil {
		return false
	}
	return agyHooksAlreadyInstalled(top)
}

func readAgyHooksRoot(hooksPath string) (jsonObject, error) {
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read hooks.json: %w", err)
	}
	return parseAgyHooksRoot(data)
}

func parseAgyHooksRoot(data []byte) (jsonObject, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var top jsonObject
	if err := json.Unmarshal(trimmed, &top); err != nil {
		return nil, fmt.Errorf("parse hooks.json: %w", err)
	}
	return top, nil
}

func agyNamedHookObject(top jsonObject) jsonObject {
	rawEntry, ok := top.get(agyHookEntryKey)
	if !ok {
		return nil
	}
	var entry jsonObject
	if err := json.Unmarshal(rawEntry, &entry); err != nil {
		return nil
	}
	return entry
}

func agyHooksAlreadyInstalled(top jsonObject) bool {
	rawEntry, ok := top.get(agyHookEntryKey)
	if !ok {
		return false
	}
	var entry jsonObject
	if err := json.Unmarshal(rawEntry, &entry); err != nil {
		return false
	}
	if enabledRaw, hasEnabled := entry.get("enabled"); hasEnabled {
		var enabled bool
		if json.Unmarshal(enabledRaw, &enabled) == nil && !enabled {
			return false
		}
	}
	for _, spec := range agyHookEvents {
		rawEvent, exists := entry.get(spec.event)
		if !exists {
			return false
		}
		var handlers []json.RawMessage
		if err := json.Unmarshal(rawEvent, &handlers); err != nil {
			return false
		}
		found := false
		for _, h := range handlers {
			if rawHookCommandContains(h, spec.command) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func mergeAgyHookEvent(entry jsonObject, event, command string) (json.RawMessage, error) {
	var handlers []json.RawMessage
	if rawEvent, exists := entry.get(event); exists {
		if err := json.Unmarshal(rawEvent, &handlers); err != nil {
			return nil, err
		}
	}
	for _, h := range handlers {
		if rawHookCommandContains(h, command) {
			return mustMarshal(handlers), nil
		}
	}
	handlers = append(handlers, mustMarshal(agyHookHandler{
		Type:    "command",
		Command: command,
	}))
	return mustMarshal(handlers), nil
}

func removeAgentDeckFromAgyEvent(handlers []json.RawMessage) ([]json.RawMessage, bool) {
	removed := false
	var cleaned []json.RawMessage
	for _, h := range handlers {
		if rawHookCommandContains(h, agentDeckAgyHookCommand) || rawHookCommandContains(h, agentDeckAgyStopCommand) {
			removed = true
			continue
		}
		cleaned = append(cleaned, h)
	}
	return cleaned, removed
}
