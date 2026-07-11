package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
	"github.com/asheshgoplani/agent-deck/internal/tmux"
)

// PR #474 ("show current task description inline for selected session") wires
// Claude's tmux pane_title through tmux.GetCachedPaneInfo into
// sessionRenderState.paneTitle, which renderSessionItem appends after the
// badges. The user-visible bug is that the inline title only updates the
// FIRST time — subsequent task transitions (Claude /rename or new task
// description) do not propagate to the rendered row.
//
// These tests pin the contract at the snapshot rebuild seam: regardless of
// how often or how stale the underlying tmux pane cache becomes,
// refreshSessionRenderSnapshot must reflect the current best-known pane title
// for the session. Bug repros target two failure modes:
//
//  1. Subsequent fresh cache values overwrite previous ones (the obvious read
//     test — sanity check that the rebuild loop reads the cache at all).
//  2. When the pane cache goes stale (background tick suppressed by navigation
//     hot-window or a slow list-panes), the rebuild must NOT blow away the
//     previously-known paneTitle. Today's behaviour clears it to "" because
//     GetCachedPaneInfo returns ok=false past the 4-second freshness threshold;
//     the renderer then drops the inline suffix until the next successful
//     RefreshPaneInfoCache, which the user perceives as "title only updated
//     once."

// instWithTmuxName returns an Instance whose GetTmuxSession() is non-nil and
// whose tmux Session.Name matches the provided cache key. Tests seed the cache
// under tmuxName; refreshSessionRenderSnapshot looks up cache entries by
// tmuxSess.Name.
func instWithTmuxName(t *testing.T, instID, tmuxName string) *session.Instance {
	t.Helper()
	inst := &session.Instance{ID: instID, Title: instID, Tool: "claude"}
	tmuxSess := tmux.ReconnectSessionLazy(tmuxName, instID, "/tmp", "claude", "idle")
	inst.SetTmuxSessionForTest(tmuxSess)
	return inst
}

// newHomeForSnapshotTest returns a Home with the atomic snapshot pre-seeded to
// an empty map (mirrors the production initialiser at home.go:792). Without
// this, atomic.Value.Load() returns nil and getSessionRenderSnapshot() falls
// through to the empty-map branch — which is fine for the very first call but
// hides the in-progress snapshot from later assertions.
func newHomeForSnapshotTest() *Home {
	h := &Home{}
	h.sessionRenderSnapshot.Store(make(map[string]sessionRenderState))
	return h
}

func TestSessionHasWindowsUsesLiveWindowCache(t *testing.T) {
	const tmuxName = "agentdeck-snapshot-window-current"
	inst := instWithTmuxName(t, "inst-window", tmuxName)
	h := newHomeForSnapshotTest()
	h.sessionRenderSnapshot.Store(map[string]sessionRenderState{
		inst.ID: {tmuxName: "agentdeck-snapshot-window-old", hasWindows: false},
	})
	tmux.SeedWindowCacheForTest(t, map[string][]tmux.WindowInfo{
		tmuxName: {
			{Index: 0, Name: "main"},
			{Index: 1, Name: "tests"},
		},
	})

	if !h.sessionHasWindows(session.Item{Type: session.ItemTypeSession, Session: inst}) {
		t.Fatal("sessionHasWindows must use current tmux window cache, not stale render snapshot")
	}
}

func TestRecordFocusedSessionUsesCurrentTmuxName(t *testing.T) {
	const currentTmuxName = "agentdeck-snapshot-focus-current"
	inst := instWithTmuxName(t, "inst-focus", currentTmuxName)
	h := newHomeForSnapshotTest()
	h.flatItems = []session.Item{{Type: session.ItemTypeSession, Session: inst}}
	h.cursor = 0
	h.sessionRenderSnapshot.Store(map[string]sessionRenderState{
		inst.ID: {tmuxName: "agentdeck-snapshot-focus-old"},
	})

	h.recordFocusedSession()

	h.focusMu.Lock()
	got := h.focusedSessionName
	h.focusMu.Unlock()
	if got != currentTmuxName {
		t.Fatalf("focusedSessionName = %q, want current tmux name %q", got, currentTmuxName)
	}
}

// TestRefreshSessionRenderSnapshot_PaneTitleUpdatesEachRefresh is the sanity
// check: when the tmux pane cache is fresh on every call,
// refreshSessionRenderSnapshot must propagate the latest title into the
// snapshot every time. Failure here would mean the rebuild loop is reading
// stale data even when the cache is fresh.
func TestRefreshSessionRenderSnapshot_PaneTitleUpdatesEachRefresh(t *testing.T) {
	const tmuxName = "agentdeck-snapshot-test-A"
	inst := instWithTmuxName(t, "inst-A", tmuxName)
	h := newHomeForSnapshotTest()

	// Tick 1: cache reports task one.
	tmux.SeedPaneInfoCacheForTest(t, map[string]tmux.PaneInfo{
		tmuxName: {Title: "⠐ implementing task one"},
	})
	h.refreshSessionRenderSnapshot([]*session.Instance{inst})
	if got := h.getSessionRenderSnapshot()[inst.ID].paneTitle; got != "implementing task one" {
		t.Fatalf("first refresh: paneTitle = %q, want %q", got, "implementing task one")
	}

	// Tick 2: cache reports task two — same instance, new title. The snapshot
	// must reflect the new value, not the cached "task one" from tick 1.
	tmux.SeedPaneInfoCacheForTest(t, map[string]tmux.PaneInfo{
		tmuxName: {Title: "⠐ implementing task two"},
	})
	h.refreshSessionRenderSnapshot([]*session.Instance{inst})
	if got := h.getSessionRenderSnapshot()[inst.ID].paneTitle; got != "implementing task two" {
		t.Errorf("second refresh: paneTitle = %q, want %q (REGRESSION: subsequent updates not propagating, PR #474)", got, "implementing task two")
	}
}

// TestRefreshSessionRenderSnapshot_PaneTitlePreservedWhenCacheStale is the
// regression pin for the "first time only" symptom. When the tmux cache goes
// stale (GetCachedPaneInfo returns ok=false past 4 s), the snapshot rebuild
// must keep the previously-known paneTitle so the rendered row continues to
// show the task description. Today's implementation clears it to "", which
// the user reads as "the title stopped updating."
//
// Why this matters: snapshot rebuilds can happen after RefreshPaneInfoCache
// was skipped or failed. If the pane cache crosses the 4-second freshness
// boundary while a later path keeps rebuilding the snapshot, every rebuild
// used to zero paneTitle.
func TestRefreshSessionRenderSnapshot_PaneTitlePreservedWhenCacheStale(t *testing.T) {
	const tmuxName = "agentdeck-snapshot-test-B"
	inst := instWithTmuxName(t, "inst-B", tmuxName)
	h := newHomeForSnapshotTest()

	// Seed cache fresh, do a refresh, confirm the snapshot picked up the title.
	tmux.SeedPaneInfoCacheForTest(t, map[string]tmux.PaneInfo{
		tmuxName: {Title: "⠐ long-running task"},
	})
	h.refreshSessionRenderSnapshot([]*session.Instance{inst})
	if got := h.getSessionRenderSnapshot()[inst.ID].paneTitle; got != "long-running task" {
		t.Fatalf("setup refresh: paneTitle = %q, want %q", got, "long-running task")
	}

	// Cache goes stale (e.g. backgroundStatusUpdate suppressed for >4 s by
	// navigation hot-window). processStatusUpdate fires anyway and rebuilds
	// the snapshot — this is the call that today wipes paneTitle.
	tmux.ExpirePaneInfoCacheForTest(t)
	h.refreshSessionRenderSnapshot([]*session.Instance{inst})

	if got := h.getSessionRenderSnapshot()[inst.ID].paneTitle; got != "long-running task" {
		t.Errorf("stale-cache refresh: paneTitle = %q, want %q preserved (REGRESSION: rebuild zeroes title when cache is stale, PR #474)", got, "long-running task")
	}
}

func TestRefreshSessionRenderSnapshot_UsesHookBadgeTimeWithoutTmux(t *testing.T) {
	inst := session.NewInstance("hook-no-tmux", t.TempDir())
	inst.ID = "inst-hook-no-tmux"
	inst.CreatedAt = time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	inst.LastStartedAt = inst.CreatedAt.Add(2 * time.Minute)
	hookTime := inst.CreatedAt.Add(7 * time.Minute)

	hooksDir := session.GetHooksDir()
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	hookPath := filepath.Join(hooksDir, inst.ID+".json")
	data, err := json.Marshal(map[string]any{
		"status":     "waiting",
		"session_id": "sess-hook-no-tmux",
		"event":      "Stop",
		"ts":         hookTime.Unix(),
	})
	if err != nil {
		t.Fatalf("marshal hook status: %v", err)
	}
	if err := os.WriteFile(hookPath, data, 0o644); err != nil {
		t.Fatalf("write hook status: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(hookPath) })

	watcher, err := session.NewStatusFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewStatusFileWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)
	go watcher.Start()

	deadline := time.Now().Add(time.Second)
	for watcher.GetHookStatus(inst.ID) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if watcher.GetHookStatus(inst.ID) == nil {
		t.Fatal("watcher did not load hook status")
	}

	h := newHomeForSnapshotTest()
	h.hookWatcher = watcher
	h.refreshSessionRenderSnapshot([]*session.Instance{inst})

	want := time.Unix(hookTime.Unix(), 0)
	if got := h.getSessionRenderSnapshot()[inst.ID].badgeTime; !got.Equal(want) {
		t.Fatalf("badgeTime = %s, want hook UpdatedAt %s for no-tmux session", got, want)
	}
}
