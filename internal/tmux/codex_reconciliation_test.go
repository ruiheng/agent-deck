package tmux

import (
	"testing"
	"time"
)

func TestCodexReconciliationDue(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		compatible bool
		last       time.Time
		want       bool
	}{
		{"non codex", false, time.Time{}, false},
		{"cold codex", true, time.Time{}, true},
		{"inside interval", true, now.Add(-9 * time.Second), false},
		{"at interval", true, now.Add(-10 * time.Second), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{codexStatusCompatible: tt.compatible}
			if tt.last.IsZero() {
				s.stateTracker = &StateTracker{}
			} else {
				s.stateTracker = &StateTracker{lastCodexPaneReconcileAttempt: tt.last}
			}
			if got := s.codexPaneReconciliationDueLocked(now); got != tt.want {
				t.Fatalf("due = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodexWorkingTitlePolicy(t *testing.T) {
	now := time.Now()
	s := &Session{codexStatusCompatible: true, stateTracker: &StateTracker{
		lastCodexPaneReconcileAttempt: now,
		spinnerTracker:                NewSpinnerActivityTracker(),
	}}
	if s.codexPaneReconciliationDueLocked(now) {
		t.Fatal("warm Codex title should not be due before interval")
	}
	s.stateTracker.lastCodexPaneReconcileAttempt = now.Add(-codexPaneReconcileInterval)
	if !s.codexPaneReconciliationDueLocked(now) {
		t.Fatal("due Working edge must capture before returning")
	}

	s.stateTracker.lastCodexTitleState = CodexTitleWorking
	s.stateTracker.codexWorkingContradicted = true
	if !s.stateTracker.codexWorkingContradicted {
		t.Fatal("contradiction must survive spinner-frame-only title updates")
	}
}

func TestCodexCaptureAccountingAndEvidencePolicy(t *testing.T) {
	now := time.Now()
	s := &Session{codexStatusCompatible: true, stateTracker: &StateTracker{
		spinnerTracker: NewSpinnerActivityTracker(),
	}}
	if !s.codexPaneReconciliationDueLocked(now) {
		t.Fatal("cold compatible session must be due")
	}
	s.noteCodexPaneCaptureLocked(now)
	if s.codexPaneReconciliationDueLocked(now.Add(9 * time.Second)) {
		t.Fatal("any Codex pane read must satisfy the next periodic deadline")
	}
	if !s.codexPaneReconciliationDueLocked(now.Add(codexPaneReconcileInterval)) {
		t.Fatal("periodic capture must become eligible at the interval")
	}

	var sample CodexStatusSample
	noteCodexPaneRead(&sample, now)
	if !sample.PaneRead || sample.EvidenceStatus != "" || !sample.EvidenceAt.IsZero() {
		t.Fatalf("non-decisive pane read must not create evidence: %+v", sample)
	}
	noteCodexDecisivePane(&sample, "active", now)
	if sample.EvidenceStatus != "active" || !sample.EvidenceAt.Equal(now) {
		t.Fatalf("decisive pane evidence = %+v, want active at %v", sample, now)
	}

	nonCodex := &Session{stateTracker: &StateTracker{}}
	nonCodex.noteCodexPaneCaptureLocked(now)
	if !nonCodex.stateTracker.lastCodexPaneReconcileAttempt.IsZero() {
		t.Fatal("non-Codex pane work must not enter the periodic capture budget")
	}
}

func TestCodexTitleStateRespectsPaneGenerationBoundary(t *testing.T) {
	name := "codex-title-generation-boundary"
	now := time.Now()
	oldCache, oldAt := paneCacheData, paneCacheTime
	paneCacheMu.Lock()
	paneCacheData = map[string]PaneInfo{name: {Title: "prefix | Working"}}
	paneCacheTime = now.Add(-time.Second)
	paneCacheMu.Unlock()
	t.Cleanup(func() {
		paneCacheMu.Lock()
		paneCacheData, paneCacheTime = oldCache, oldAt
		paneCacheMu.Unlock()
	})

	s := &Session{Name: name, codexStatusCompatible: true, paneGenerationStartedAt: now}
	if got := s.codexTitleStateFromCacheLocked(); got != CodexTitleUnknown {
		t.Fatalf("pre-generation title = %v, want unknown", got)
	}
	s.paneGenerationStartedAt = time.Time{}
	if got := s.codexTitleStateFromCacheLocked(); got != CodexTitleWorking {
		t.Fatalf("lazy reconnect title = %v, want working", got)
	}
}

func TestCodexIndeterminatePanePreservesWithoutEvidence(t *testing.T) {
	now := time.Now()
	s := &Session{codexStatusCompatible: true, stateTracker: &StateTracker{
		spinnerTracker: NewSpinnerActivityTracker(),
	}}
	got, sample, err := s.classifyCodexCapturedPane("plain output without a prompt", CodexTitleWorking, now, "active")
	if err != nil {
		t.Fatalf("classifyCodexCapturedPane() error = %v", err)
	}
	if got != "active" || !sample.Preserved || sample.EvidenceStatus != "" || !sample.PaneRead {
		t.Fatalf("indeterminate capture = (%q,%+v), want preserved active without evidence", got, sample)
	}
}
