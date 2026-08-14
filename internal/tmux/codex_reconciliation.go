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
	exists, paneDead := s.statusLiveness()
	if !exists {
		s.mu.Lock()
		s.lastStableStatus = "inactive"
		s.lastSubstate = SubstateNone
		s.mu.Unlock()
		return "inactive", CodexStatusSample{}, nil
	}
	if paneDead {
		s.mu.Lock()
		s.lastStableStatus = "inactive"
		s.lastSubstate = SubstateNone
		s.mu.Unlock()
		return "inactive", CodexStatusSample{}, nil
	}

	now := time.Now()
	s.mu.Lock()
	s.ensureStateTrackerLocked()
	title, titleObservedAt := s.codexTitleObservationFromCacheLocked()
	previousTitle := s.stateTracker.lastCodexTitleState
	titleEdge := title != previousTitle
	due := s.codexPaneReconciliationDueLocked(now)
	preStatus := s.lastStableStatus
	if preStatus == "" {
		preStatus = "waiting"
	}

	// A latch records distrust of the current Working observation, not just the
	// last title edge. ConfirmCodexDemotion intentionally leaves title-edge
	// state untouched, so its failed Working verification must also be cleared
	// by the next observed title-away state. Spinner-frame churn remains Working
	// and therefore cannot clear the latch.
	s.clearCodexWorkingContradictionOnTitleAwayLocked(title)

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
			EvidenceAt:     titleObservedAt,
			Observation:    CodexStatusObservationTitleWorking,
		}, nil
	}

	// A stable non-contradicted Working title stays a lightweight fast path
	// until the periodic deadline. Spinner-frame changes do not create edges.
	if title == CodexTitleWorking && !due && !s.stateTracker.codexWorkingContradicted {
		s.stateTracker.lastCodexTitleState = title
		s.markCodexTitleWorkingLocked()
		s.mu.Unlock()
		// A stable title is a lightweight local hold, not a new live
		// observation. Renewing its evidence on every poll could let it mask a
		// newer hook on another surface.
		return "active", CodexStatusSample{TitleState: title}, nil
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
		// advanced before the request, so retry is bounded. A failed Working
		// verification also blocks the same cached title from promoting on the
		// next lightweight poll.
		s.mu.Lock()
		s.ensureStateTrackerLocked()
		if title == CodexTitleWorking {
			s.stateTracker.codexWorkingContradicted = true
		}
		s.mu.Unlock()
		return preStatus, CodexStatusSample{
			TitleState:  title,
			Preserved:   true,
			Observation: CodexStatusObservationCaptureFailure,
		}, nil
	}

	return s.classifyCodexCapturedPane(StripANSI(raw), title, observedAt, preStatus)
}

// contradictCodexWorkingLocked records that the current strict Working title
// disagreed with a readable pane result. Call with s.mu held.
func (s *Session) contradictCodexWorkingLocked(title CodexTitleState) {
	if title == CodexTitleWorking && s.codexStatusCompatible {
		s.ensureStateTrackerLocked()
		s.stateTracker.codexWorkingContradicted = true
	}
}

// clearCodexWorkingContradictionOnTitleAwayLocked accepts a current semantic
// title observation away from Working as the lifecycle boundary for a prior
// Working contradiction. It is intentionally independent of
// lastCodexTitleState because forced confirmation does not mutate title-edge
// bookkeeping. Call with s.mu held.
func (s *Session) clearCodexWorkingContradictionOnTitleAwayLocked(title CodexTitleState) {
	if title != CodexTitleWorking && s.codexStatusCompatible {
		s.ensureStateTrackerLocked()
		s.stateTracker.codexWorkingContradicted = false
	}
}

// clearCodexWorkingContradictionOnVisibleBusyLocked accepts only current
// visible busy evidence as confirmation that a previously contradicted title
// is trustworthy again. Spinner grace is intentionally not sufficient.
// Call with s.mu held.
func (s *Session) clearCodexWorkingContradictionOnVisibleBusyLocked() {
	if s.codexStatusCompatible {
		s.ensureStateTrackerLocked()
		s.stateTracker.codexWorkingContradicted = false
	}
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
		s.contradictCodexWorkingLocked(title)
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "error"
		s.startupAt = time.Time{}
		noteCodexDecisivePane(&sample, "error", observedAt, CodexStatusObservationError)
		return "error", sample, nil
	}

	if s.hasErrorBannerIndicator(content) {
		s.contradictCodexWorkingLocked(title)
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "error"
		s.startupAt = time.Time{}
		noteCodexDecisivePane(&sample, "error", observedAt, CodexStatusObservationError)
		return "error", sample, nil
	}
	busy := s.observeBusyIndicator(content)
	if busy.Active {
		s.stateTracker.lastChangeTime = observedAt
		s.stateTracker.realActivityConfirmed = true
		s.stateTracker.acknowledged = false
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "active"
		s.startupAt = time.Time{}
		if busy.Visible {
			s.clearCodexWorkingContradictionOnVisibleBusyLocked()
			noteCodexDecisivePane(&sample, "active", observedAt, CodexStatusObservationVisibleBusy)
		} else {
			noteCodexStatusObservation(&sample, CodexStatusObservationBusyGrace)
		}
		return "active", sample, nil
	}
	if s.markBackgroundWorkActiveLocked(content, 0, s.DisplayName) {
		noteCodexDecisivePane(&sample, "active", observedAt, CodexStatusObservationBackgroundWork)
		return "active", sample, nil
	}
	if s.hasPromptIndicator(content) {
		// Set the disagreement before the acknowledged/hold returns: all prompt
		// outcomes are visible no-busy evidence against a current Working title.
		s.contradictCodexWorkingLocked(title)
		if s.stateTracker.acknowledged {
			s.resetPromptNoBusyHoldLocked()
			s.lastStableStatus = "idle"
			s.startupAt = time.Time{}
			noteCodexDecisivePane(&sample, "idle", observedAt, CodexStatusObservationPrompt)
			return "idle", sample, nil
		}

		if title == CodexTitleReady {
			// Ready is the first no-busy observation; this fresh prompt is the
			// second, so bypass the ordinary two-poll hold.
			s.resetPromptNoBusyHoldLocked()
			s.lastStableStatus = "waiting"
			s.startupAt = time.Time{}
			sample.ReadyPromptAgreement = true
			noteCodexDecisivePane(&sample, "waiting", observedAt, CodexStatusObservationPrompt)
			return "waiting", sample, nil
		}

		if s.shouldHoldActiveOnPromptLocked() {
			noteCodexStatusObservation(&sample, CodexStatusObservationPromptHold)
			return "active", sample, nil
		}
		s.resetPromptNoBusyHoldLocked()
		s.lastStableStatus = "waiting"
		s.startupAt = time.Time{}
		noteCodexDecisivePane(&sample, "waiting", observedAt, CodexStatusObservationPrompt)
		return "waiting", sample, nil
	}

	s.contradictCodexWorkingLocked(title)
	// Indeterminate capture preserves the prior stable result. It contributes no
	// evidence and does not let title promotion create waiting/error.
	sample.Preserved = true
	sample.Observation = CodexStatusObservationIndeterminate
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
		// A forced confirmation is still a pane verification. When its current
		// title says Working, fail closed just as the regular reconciliation
		// path does so this unverified title cannot immediately re-promote.
		s.mu.Lock()
		s.contradictCodexWorkingLocked(title)
		s.mu.Unlock()
		return false, time.Time{}
	}
	got, sample, _ := s.classifyCodexCapturedPane(StripANSI(raw), title, observedAt, expected)
	if got != expected || !sample.HasDecisivePaneEvidence(expected) {
		return false, time.Time{}
	}
	return true, sample.EvidenceAt
}
