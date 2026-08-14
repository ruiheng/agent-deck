package tmux

import (
	"errors"
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
	noteCodexDecisivePane(&sample, "active", now, CodexStatusObservationVisibleBusy)
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

type codexCaptureResult struct {
	content string
	err     error
}

type codexCaptureRecorder struct {
	results []codexCaptureResult
	calls   int
	fresh   []bool
}

func (r *codexCaptureRecorder) capture(fresh bool) (string, error) {
	r.calls++
	r.fresh = append(r.fresh, fresh)
	if r.calls > len(r.results) {
		return "", errors.New("unexpected Codex status capture")
	}
	result := r.results[r.calls-1]
	return result.content, result.err
}

// newCodexStatusTestSession exercises GetStatusSample through its real title,
// deadline, and capture-selection logic while keeping tmux I/O deterministic.
func newCodexStatusTestSession(name, preStatus string, lastAttempt time.Time, results ...codexCaptureResult) (*Session, *codexCaptureRecorder) {
	recorder := &codexCaptureRecorder{results: results}
	session := &Session{
		Name:                  name,
		DisplayName:           name,
		codexStatusCompatible: true,
		lastStableStatus:      preStatus,
		stateTracker: &StateTracker{
			lastCodexPaneReconcileAttempt: lastAttempt,
			lastActivityTimestamp:         7,
			spinnerTracker:                NewSpinnerActivityTracker(),
		},
		statusCapture: recorder.capture,
		statusLivenessProbe: func() (bool, bool) {
			return true, false
		},
		statusWindowActivityProbe: func() (int64, error) {
			return 7, nil
		},
	}
	return session, recorder
}

func seedCodexPaneTitle(t *testing.T, name, title string, observedAt time.Time) {
	t.Helper()
	paneCacheMu.Lock()
	oldData, oldAt := paneCacheData, paneCacheTime
	paneCacheData = map[string]PaneInfo{name: {Title: title}}
	paneCacheTime = observedAt
	paneCacheMu.Unlock()
	t.Cleanup(func() {
		paneCacheMu.Lock()
		paneCacheData, paneCacheTime = oldData, oldAt
		paneCacheMu.Unlock()
	})
}

func TestCodexWorkingReconciliationBlocksRePromotionAfterFailure(t *testing.T) {
	now := time.Now()
	s, captures := newCodexStatusTestSession(
		"codex-working-failure",
		"waiting",
		now.Add(-codexPaneReconcileInterval),
		codexCaptureResult{err: errors.New("capture unavailable")},
	)
	seedCodexPaneTitle(t, s.Name, "agent-deck | Working", now)

	got, sample, err := s.GetStatusSample()
	if err != nil || got != "waiting" || !sample.Preserved {
		t.Fatalf("failed due Working sample = (%q,%+v,%v), want preserved waiting", got, sample, err)
	}
	if captures.calls != 1 || !s.stateTracker.codexWorkingContradicted {
		t.Fatalf("failed Working must capture once and latch contradiction; calls=%d contradicted=%v", captures.calls, s.stateTracker.codexWorkingContradicted)
	}

	got, sample, err = s.GetStatusSample()
	if err != nil || got != "waiting" || sample.EvidenceStatus != "" {
		t.Fatalf("same unverified Working title = (%q,%+v,%v), want waiting without evidence", got, sample, err)
	}
	if captures.calls != 1 {
		t.Fatalf("same non-due Working title must not re-capture or re-promote; calls=%d", captures.calls)
	}
}

func TestCodexWorkingAwayAndBackClearsContradiction(t *testing.T) {
	now := time.Now()
	s, captures := newCodexStatusTestSession("codex-working-away-and-back", "waiting", now)
	s.stateTracker.lastCodexTitleState = CodexTitleWorking
	s.stateTracker.codexWorkingContradicted = true
	seedCodexPaneTitle(t, s.Name, "agent-deck | ambiguous", now)

	got, sample, err := s.GetStatusSample()
	if err != nil || got != "waiting" || sample.EvidenceStatus != "" {
		t.Fatalf("Working away edge = (%q,%+v,%v), want waiting without evidence", got, sample, err)
	}
	if s.stateTracker.codexWorkingContradicted {
		t.Fatal("a title transition away from Working must clear the contradiction latch")
	}

	seedCodexPaneTitle(t, s.Name, "agent-deck | Working ⠋", time.Now())
	got, sample, err = s.GetStatusSample()
	if err != nil || got != "active" || sample.Observation != CodexStatusObservationTitleWorking || sample.EvidenceStatus != "active" {
		t.Fatalf("Working away-and-back edge = (%q,%+v,%v), want immediate title promotion", got, sample, err)
	}
	if captures.calls != 0 {
		t.Fatalf("warm away-and-back title edges must not capture; calls=%d", captures.calls)
	}
}

func TestCodexWorkingReconciliationLatchesVisibleNoBusyPrompt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		preStatus   string
		acknowledge bool
		want        string
	}{
		{name: "waiting prompt", preStatus: "waiting", want: "waiting"},
		{name: "acknowledged prompt", preStatus: "active", acknowledge: true, want: "idle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			s, captures := newCodexStatusTestSession(
				"codex-working-prompt-"+tc.name,
				tc.preStatus,
				now.Add(-codexPaneReconcileInterval),
				codexCaptureResult{content: "› "},
			)
			s.stateTracker.acknowledged = tc.acknowledge
			seedCodexPaneTitle(t, s.Name, "agent-deck | Working ⠼", now)

			got, sample, err := s.GetStatusSample()
			if err != nil || got != tc.want || !sample.HasDecisivePaneEvidence(tc.want) {
				t.Fatalf("Working + prompt = (%q,%+v,%v), want decisive %q", got, sample, err, tc.want)
			}
			if captures.calls != 1 || !s.stateTracker.codexWorkingContradicted {
				t.Fatalf("visible no-busy prompt must latch contradiction; calls=%d contradicted=%v", captures.calls, s.stateTracker.codexWorkingContradicted)
			}
		})
	}
}

func TestCodexWorkingVisibleBusyClearsContradiction(t *testing.T) {
	now := time.Now()
	s, captures := newCodexStatusTestSession(
		"codex-working-busy",
		"waiting",
		now.Add(-codexPaneReconcileInterval),
		codexCaptureResult{content: "• Working (1s · esc to interrupt)"},
	)
	s.stateTracker.lastCodexTitleState = CodexTitleWorking
	s.stateTracker.codexWorkingContradicted = true
	seedCodexPaneTitle(t, s.Name, "agent-deck | Working", now)

	got, sample, err := s.GetStatusSample()
	if err != nil || got != "active" || !sample.HasDecisivePaneEvidence("active") || sample.Observation != CodexStatusObservationVisibleBusy {
		t.Fatalf("visible busy reconciliation = (%q,%+v,%v), want decisive active busy", got, sample, err)
	}
	if captures.calls != 1 || s.stateTracker.codexWorkingContradicted {
		t.Fatalf("visible busy must be the only successful clear; calls=%d contradicted=%v", captures.calls, s.stateTracker.codexWorkingContradicted)
	}
}

func TestCodexReadyReconciliationForcesOneFreshPromptCapture(t *testing.T) {
	now := time.Now()
	s, captures := newCodexStatusTestSession(
		"codex-ready-prompt",
		"active",
		now,
		codexCaptureResult{content: "› "},
	)
	s.stateTracker.lastCodexTitleState = CodexTitleWorking
	seedCodexPaneTitle(t, s.Name, "agent-deck | Ready", now)

	got, sample, err := s.GetStatusSample()
	if err != nil || got != "waiting" || !sample.ReadyPromptAgreement || !sample.HasDecisivePaneEvidence("waiting") {
		t.Fatalf("Ready + prompt = (%q,%+v,%v), want decisive waiting agreement", got, sample, err)
	}
	if captures.calls != 1 || len(captures.fresh) != 1 || !captures.fresh[0] {
		t.Fatalf("Ready edge must use exactly one fresh capture; calls=%d fresh=%v", captures.calls, captures.fresh)
	}
}

func TestConfirmCodexDemotionRejectsIndeterminatePane(t *testing.T) {
	s, captures := newCodexStatusTestSession(
		"codex-indeterminate-confirmation",
		"active",
		time.Now(),
		codexCaptureResult{content: "ordinary output without a prompt"},
	)

	confirmed, observedAt := s.ConfirmCodexDemotion("waiting")
	if confirmed || !observedAt.IsZero() {
		t.Fatalf("indeterminate confirmation = (%v,%v), want rejected zero observation", confirmed, observedAt)
	}
	if captures.calls != 1 || len(captures.fresh) != 1 || !captures.fresh[0] {
		t.Fatalf("confirmation must make exactly one fresh attempt; calls=%d fresh=%v", captures.calls, captures.fresh)
	}
}

func TestCodexFailedConfirmationUnderWorkingDoesNotRepromote(t *testing.T) {
	now := time.Now()
	s, captures := newCodexStatusTestSession(
		"codex-failed-working-confirmation",
		"waiting",
		now,
		codexCaptureResult{err: errors.New("confirmation capture unavailable")},
	)
	seedCodexPaneTitle(t, s.Name, "agent-deck | Working", now)

	confirmed, observedAt := s.ConfirmCodexDemotion("waiting")
	if confirmed || !observedAt.IsZero() {
		t.Fatalf("failed Working confirmation = (%v,%v), want rejected zero observation", confirmed, observedAt)
	}
	if captures.calls != 1 || !captures.fresh[0] || !s.stateTracker.codexWorkingContradicted {
		t.Fatalf("failed Working confirmation must latch once; calls=%d fresh=%v contradicted=%v", captures.calls, captures.fresh, s.stateTracker.codexWorkingContradicted)
	}

	got, sample, err := s.GetStatusSample()
	if err != nil || got != "waiting" || sample.EvidenceStatus != "" {
		t.Fatalf("same Working title after failed confirmation = (%q,%+v,%v), want preserved waiting", got, sample, err)
	}
	if captures.calls != 1 {
		t.Fatalf("failed confirmation must not trigger a third same-call/next-edge capture; calls=%d", captures.calls)
	}
}

func TestCodexStableWorkingDoesNotRenewEvidence(t *testing.T) {
	now := time.Now()
	s, captures := newCodexStatusTestSession("codex-stable-working", "active", now)
	s.stateTracker.lastCodexTitleState = CodexTitleWorking
	seedCodexPaneTitle(t, s.Name, "agent-deck | Working ⠼", now.Add(-time.Second))

	for attempt := 0; attempt < 2; attempt++ {
		got, sample, err := s.GetStatusSample()
		if err != nil || got != "active" || sample.EvidenceStatus != "" || !sample.EvidenceAt.IsZero() {
			t.Fatalf("stable Working poll %d = (%q,%+v,%v), want active without new evidence", attempt+1, got, sample, err)
		}
	}
	if captures.calls != 0 {
		t.Fatalf("warm stable Working title must not capture before its deadline; calls=%d", captures.calls)
	}
}

func TestCodexWorkingEdgeUsesCacheObservationTime(t *testing.T) {
	now := time.Now()
	observedAt := now.Add(-time.Second)
	s, captures := newCodexStatusTestSession("codex-working-edge", "waiting", now)
	seedCodexPaneTitle(t, s.Name, "agent-deck | Working", observedAt)

	got, sample, err := s.GetStatusSample()
	if err != nil || got != "active" || sample.Observation != CodexStatusObservationTitleWorking || !sample.EvidenceAt.Equal(observedAt) {
		t.Fatalf("Working title edge = (%q,%+v,%v), want cache observation at %v", got, sample, err, observedAt)
	}
	if captures.calls != 0 {
		t.Fatalf("warm Working title edge must not capture; calls=%d", captures.calls)
	}
}

func TestCodexSpinnerGraceDoesNotPublishPaneEvidence(t *testing.T) {
	now := time.Now()
	s := &Session{codexStatusCompatible: true, lastStableStatus: "active", stateTracker: &StateTracker{
		spinnerTracker: NewSpinnerActivityTracker(),
	}}
	s.stateTracker.spinnerTracker.lastBusyTime = now

	got, sample, err := s.classifyCodexCapturedPane("› ", CodexTitleUnknown, now, "active")
	if err != nil || got != "active" || sample.Observation != CodexStatusObservationBusyGrace || sample.EvidenceStatus != "" || !sample.EvidenceAt.IsZero() {
		t.Fatalf("spinner grace = (%q,%+v,%v), want local active hold without evidence", got, sample, err)
	}
}
