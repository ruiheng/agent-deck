package tmux

import (
	"time"
)

// GetStatus preserves the public status API. Instance.UpdateStatus uses
// GetStatusSample so it can act on the additional, one-call Codex facts.
func (s *Session) GetStatus() (string, error) {
	status, _, err := s.GetStatusSample()
	return status, err
}

// GetStatusSample applies Codex-specific title reconciliation before falling
// back to the established classifier. The normal classifier remains untouched
// for every non-Codex session.
func (s *Session) GetStatusSample() (string, CodexStatusSample, error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	if !s.IsCodexStatusCompatible() {
		status, err := s.getStatusLegacy(nil)
		return status, CodexStatusSample{}, err
	}
	return s.getCodexStatusSample()
}

func (s *Session) getCodexStatusSample() (string, CodexStatusSample, error) {
	// Preserve normal dead-session precedence before interpreting cached title
	// metadata. A stale cache title must never revive a missing/dead pane.
	if !s.Exists() {
		s.mu.Lock()
		s.lastStableStatus = "inactive"
		s.lastSubstate = SubstateNone
		s.mu.Unlock()
		return "inactive", CodexStatusSample{}, nil
	}
	if s.IsPaneDead() {
		s.mu.Lock()
		s.lastStableStatus = "inactive"
		s.lastSubstate = SubstateNone
		s.mu.Unlock()
		return "inactive", CodexStatusSample{}, nil
	}

	now := time.Now()
	s.mu.Lock()
	s.ensureStateTrackerLocked()
	title := s.codexTitleStateFromCacheLocked()
	previousTitle := s.stateTracker.lastCodexTitleState
	titleEdge := title != previousTitle
	due := s.codexPaneReconciliationDueLocked(now)
	preStatus := s.lastStableStatus
	if preStatus == "" {
		preStatus = "waiting"
	}

	// A transition away from Working is what clears its contradiction marker.
	if title != CodexTitleWorking {
		s.stateTracker.codexWorkingContradicted = false
	}

	// A warm, non-contradicted Working edge can promote immediately. The
	// periodic deadline makes cold/restarted wrappers go through a pane read
	// first, preventing a stale cached title from renewing active indefinitely.
	if title == CodexTitleWorking && titleEdge && !due && !s.stateTracker.codexWorkingContradicted {
		s.stateTracker.lastCodexTitleState = title
		s.markCodexTitleWorkingLocked()
		s.mu.Unlock()
		return "active", CodexStatusSample{
			TitleState:     title,
			EvidenceStatus: "active",
			EvidenceAt:     now,
		}, nil
	}

	// A stable non-contradicted Working title stays a lightweight fast path
	// until the periodic deadline. Spinner-frame changes do not create edges.
	if title == CodexTitleWorking && !due && !s.stateTracker.codexWorkingContradicted {
		s.stateTracker.lastCodexTitleState = title
		s.markCodexTitleWorkingLocked()
		s.mu.Unlock()
		return "active", CodexStatusSample{
			TitleState:     title,
			EvidenceStatus: "active",
			EvidenceAt:     now,
		}, nil
	}

	// New Ready must obtain an independent fresh pane observation immediately;
	// stable Ready falls back to the bounded periodic capture. Ready also clears
	// spinner grace before classification so a just-finished turn is not held
	// active by a prior Working sample.
	forceFresh := title == CodexTitleReady && titleEdge
	needsCapture := due || forceFresh
	if title == CodexTitleReady {
		s.expireSpinnerGraceLocked()
	}
	s.stateTracker.lastCodexTitleState = title
	if !needsCapture {
		s.mu.Unlock()
		sample := CodexStatusSample{TitleState: title}
		status, err := s.getStatusLegacy(&sample)
		return status, sample, err
	}
	s.mu.Unlock()

	var (
		raw        string
		observedAt time.Time
		err        error
	)
	if forceFresh {
		raw, observedAt, err = s.captureStatusPane(true)
	} else {
		raw, observedAt, err = s.captureStatusPane(false)
	}
	if err != nil {
		// The design is fail-closed: a title-only edge never manufactures a
		// new result on a capture failure. The deadline was deliberately
		// advanced before the request, so retry is bounded.
		return preStatus, CodexStatusSample{TitleState: title, Preserved: true}, nil
	}

	return s.classifyCodexCapturedPane(StripANSI(raw), title, observedAt, preStatus)
}

// classifyCodexCapturedPane reuses the established precedence in a compact
// form for the explicit, due Codex path. It deliberately asks busy before
// prompt; strict visible busy wins over a Ready title race.
func (s *Session) classifyCodexCapturedPane(content string, title CodexTitleState, observedAt time.Time, preStatus string) (string, CodexStatusSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureStateTrackerLocked()
	sample := CodexStatusSample{PaneRead: true, TitleState: title}

	// Ready disables only grace; the visible strict row still owns the result.
	// Working's title-only fast path also marks the spinner tracker. A due
	// Working capture must judge agreement from the currently visible row, not
	// from that title-created grace period.
	if title == CodexTitleReady || title == CodexTitleWorking {
		s.expireSpinnerGraceLocked()
	}
	s.lastSubstate = s.classifySubstate(content)
	s.noteSampleAuthFailureLocked(
		s.lastSubstate == SubstateAuth401 && s.isAuthFailureIndicator(content),
		content,
	)
	if s.lastSubstate == SubstateModelUnavailable {
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "error"
		s.startupAt = time.Time{}
		sample.EvidenceStatus = "error"
		sample.EvidenceAt = observedAt
		return "error", sample, nil
	}

	if s.hasErrorBannerIndicator(content) {
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "error"
		s.startupAt = time.Time{}
		sample.EvidenceStatus = "error"
		sample.EvidenceAt = observedAt
		return "error", sample, nil
	}
	if s.hasBusyIndicator(content) {
		s.stateTracker.lastChangeTime = observedAt
		s.stateTracker.realActivityConfirmed = true
		s.stateTracker.acknowledged = false
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "active"
		s.startupAt = time.Time{}
		s.stateTracker.codexWorkingContradicted = false
		sample.EvidenceStatus = "active"
		sample.EvidenceAt = observedAt
		return "active", sample, nil
	}
	if s.markBackgroundWorkActiveLocked(content, 0, s.DisplayName) {
		sample.EvidenceStatus = "active"
		sample.EvidenceAt = observedAt
		return "active", sample, nil
	}
	if s.hasPromptIndicator(content) {
		if s.stateTracker.acknowledged {
			s.resetPromptNoBusyHoldLocked()
			s.lastStableStatus = "idle"
			s.startupAt = time.Time{}
			sample.EvidenceStatus = "idle"
			sample.EvidenceAt = observedAt
			return "idle", sample, nil
		}

		if title == CodexTitleReady {
			// Ready is the first no-busy observation; this fresh prompt is the
			// second, so bypass the ordinary two-poll hold.
			s.resetPromptNoBusyHoldLocked()
			s.lastStableStatus = "waiting"
			s.startupAt = time.Time{}
			sample.ReadyPromptAgreement = true
			sample.EvidenceStatus = "waiting"
			sample.EvidenceAt = observedAt
			return "waiting", sample, nil
		}

		if title == CodexTitleWorking {
			s.stateTracker.codexWorkingContradicted = true
		}
		if s.shouldHoldActiveOnPromptLocked() {
			return "active", sample, nil
		}
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "waiting"
		s.startupAt = time.Time{}
		sample.EvidenceStatus = "waiting"
		sample.EvidenceAt = observedAt
		return "waiting", sample, nil
	}

	if title == CodexTitleWorking {
		s.stateTracker.codexWorkingContradicted = true
	}
	// Indeterminate capture preserves the prior stable result. It contributes no
	// evidence and does not let title promotion create waiting/error.
	sample.Preserved = true
	return preStatus, sample, nil
}

// ConfirmCodexDemotion takes exactly one uncached second pane observation for
// a candidate waiting/error transition. It intentionally does not rerun
// liveness checks or title-edge mutation.
func (s *Session) ConfirmCodexDemotion(expected string) (bool, time.Time) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	if !s.IsCodexStatusCompatible() || (expected != "waiting" && expected != "error") {
		return false, time.Time{}
	}
	s.mu.Lock()
	title := s.codexTitleStateFromCacheLocked()
	if title == CodexTitleReady {
		s.expireSpinnerGraceLocked()
	}
	s.mu.Unlock()

	raw, observedAt, err := s.captureStatusPane(true)
	if err != nil {
		return false, time.Time{}
	}
	got, sample, _ := s.classifyCodexCapturedPane(StripANSI(raw), title, observedAt, expected)
	return got == expected && sample.PaneRead, sample.EvidenceAt
}
