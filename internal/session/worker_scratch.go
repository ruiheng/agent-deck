// Package session — worker-scratch CLAUDE_CONFIG_DIR (issue #59, v1.7.68).
//
// Background. Conductors store the real telegram bot token under
// `~/.claude/channels/telegram/.env`. When a non-conductor claude
// worker is launched via `agent-deck launch` / `agent-deck add` on the
// same host, three layers of prior work stripped TELEGRAM_STATE_DIR
// from the child's env (issue #680 narrow, S8 broadened in v1.7.40).
// But the Telegram plugin is ENABLED GLOBALLY in the profile's
// `settings.json` — and without TSD it falls back to the default path
// `~/.claude/channels/telegram/`, which is the conductor's token dir.
// The worker reads the conductor's `.env`, spawns its own `bun
// telegram` poller, and the Bot API returns 409 Conflict when two
// pollers race the same token. Messages drop for everyone.
//
// Fix. Prepare an ephemeral scratch CLAUDE_CONFIG_DIR for every worker
// spawn. The scratch dir is a shallow mirror of the ambient profile:
// every entry is symlinked to the source EXCEPT `settings.json`,
// which is copied and mutated so
// `enabledPlugins["telegram@claude-plugins-official"] = false`. That
// pins the plugin OFF before it has a chance to load — categorically
// different from TSD stripping, which only moves its state dir.
//
// Scope. Applies to claude workers whose
// `telegramStateDirStripExpr(inst) != ""` (the existing predicate).
// Conductors, explicit telegram channel owners, and non-claude tools
// use the ambient profile as-is.
//
// Cleanup. `CleanupWorkerScratchConfigDir` removes the dir on
// session stop/remove — best-effort, no-op on first-time misses. The
// scratch dir lives under `~/.agent-deck/worker-scratch/<instance-id>/`.

package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// telegramPluginID is the Claude Code plugin id we force-disable in
// the worker's scratch settings.json. Kept in sync with
// `telegramChannelPrefix` consumers in env.go / telegram_validator.go.
const telegramPluginID = "telegram@claude-plugins-official"

// NeedsWorkerScratchConfigDir returns true when a scratch CLAUDE_CONFIG_DIR
// should be prepared for this instance at spawn time. The predicate
// mirrors `telegramStateDirStripExpr` so both the env strip (TSD) and
// the plugin disable (this scratch dir) fire for exactly the same
// sessions — layered defense against the conductor-poller storm.
func (i *Instance) NeedsWorkerScratchConfigDir() bool {
	return telegramStateDirStripExpr(i) != ""
}

// WorkerScratchDirRoot returns the path that holds every worker's
// scratch config dir. Callers with a valid home should prefer
// workerScratchDirFor below which derives this from the effective
// HOME at call time.
func workerScratchDirRoot(home string) string {
	return filepath.Join(home, ".agent-deck", "worker-scratch")
}

// workerScratchDirFor returns the scratch path for a specific instance
// id under the given home. `<home>/.agent-deck/worker-scratch/<id>/`.
func workerScratchDirFor(home, instanceID string) string {
	return filepath.Join(workerScratchDirRoot(home), instanceID)
}

// EnsureWorkerScratchConfigDir prepares (idempotently) a scratch
// CLAUDE_CONFIG_DIR for this instance and returns its path. Returns
// "" (no error) when the instance is a conductor, explicit telegram
// channel owner, or non-claude tool — callers should treat "" as
// "use the ambient profile as-is".
//
// `sourceProfileDir` is the ambient CLAUDE_CONFIG_DIR that the worker
// would otherwise load. The scratch dir mirrors it via symlinks and
// rewrites `settings.json` with the telegram plugin pinned off.
//
// Not an error when source is absent — we still create the scratch
// dir with a minimal `settings.json` (defense-in-depth).
func (i *Instance) EnsureWorkerScratchConfigDir(sourceProfileDir string) (string, error) {
	if !i.NeedsWorkerScratchConfigDir() {
		return "", nil
	}
	if i.ID == "" {
		return "", fmt.Errorf("EnsureWorkerScratchConfigDir: instance has no ID")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	scratch := workerScratchDirFor(home, i.ID)

	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", fmt.Errorf("mkdir scratch: %w", err)
	}

	// Write the mutated settings.json (telegram plugin pinned off).
	// Any prior scratch settings.json is clobbered — this is called at
	// spawn time where stale state is a liability, not an asset.
	settings := map[string]interface{}{}
	if sourceProfileDir != "" {
		if data, readErr := os.ReadFile(filepath.Join(sourceProfileDir, "settings.json")); readErr == nil {
			_ = json.Unmarshal(data, &settings)
		}
		// Absent file is fine — we'll emit a minimal settings.json below.
	}
	plugins, _ := settings["enabledPlugins"].(map[string]interface{})
	if plugins == nil {
		plugins = map[string]interface{}{}
	}
	plugins[telegramPluginID] = false
	settings["enabledPlugins"] = plugins

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "settings.json"), out, 0o644); err != nil {
		return "", fmt.Errorf("write scratch settings: %w", err)
	}

	// Mirror everything else from source via symlinks. Skip settings.json
	// (we just wrote a mutated copy) and skip bare symlinks we've
	// already materialised on a prior call to keep Ensure idempotent.
	if sourceProfileDir != "" {
		if err := mirrorProfileEntries(scratch, sourceProfileDir); err != nil {
			return "", err
		}
	}

	return scratch, nil
}

// mirrorProfileEntries ensures every top-level entry in source (except
// settings.json) is reachable from dest. Symlinks are left alone because they
// stay live; copied fallback entries are refreshed on every Ensure call so a
// reused scratch dir still mirrors source profile updates.
func mirrorProfileEntries(dest, source string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("read source profile: %w", err)
	}

	sourceNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		sourceNames[entry.Name()] = struct{}{}
	}
	if err := pruneStaleProfileEntries(dest, sourceNames); err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "settings.json" {
			continue
		}
		linkPath := filepath.Join(dest, name)
		if info, statErr := os.Lstat(linkPath); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				continue // live mirror from a prior Ensure call
			}
			if err := os.RemoveAll(linkPath); err != nil {
				return fmt.Errorf("refresh copied mirror %s: %w", name, err)
			}
		}
		target := filepath.Join(source, name)
		if err := mirrorProfileEntry(target, linkPath); err != nil {
			return fmt.Errorf("mirror %s: %w", name, err)
		}
	}
	return nil
}

func pruneStaleProfileEntries(dest string, sourceNames map[string]struct{}) error {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return fmt.Errorf("read scratch profile: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "settings.json" {
			continue
		}
		if _, ok := sourceNames[name]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dest, name)); err != nil {
			return fmt.Errorf("prune stale mirror %s: %w", name, err)
		}
	}
	return nil
}

func mirrorProfileEntry(source, dest string) error {
	if err := os.Symlink(source, dest); err == nil {
		return nil
	}

	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirRecursive(source, dest)
	}
	return copyFileWithPerm(source, dest, info.Mode()&os.ModePerm)
}

// CleanupWorkerScratchConfigDir removes the scratch dir for this
// instance. Best-effort — callers ignore the error. Called from the
// session stop / remove path so short-lived workers don't leak
// scratch dirs across reboots.
func (i *Instance) CleanupWorkerScratchConfigDir() {
	if i.WorkerScratchConfigDir == "" {
		return
	}
	_ = os.RemoveAll(i.WorkerScratchConfigDir)
	i.WorkerScratchConfigDir = ""
}

// prepareWorkerScratchConfigDirForSpawn is the spawn-path wrapper
// around EnsureWorkerScratchConfigDir. Called from Start(),
// StartWithMessage(), and the restart fallback path. Best-effort —
// a failure here falls back to the ambient profile rather than
// blocking the spawn, with a warning to the session log.
func (i *Instance) prepareWorkerScratchConfigDirForSpawn() {
	if !i.NeedsWorkerScratchConfigDir() {
		return
	}
	sourceDir := GetClaudeConfigDirForInstance(i)
	scratch, err := i.EnsureWorkerScratchConfigDir(sourceDir)
	if err != nil {
		sessionLog.Warn("worker_scratch_prepare_failed",
			slog.String("instance_id", i.ID),
			slog.String("source", sourceDir),
			slog.String("error", err.Error()),
		)
		return
	}
	i.WorkerScratchConfigDir = scratch
}
