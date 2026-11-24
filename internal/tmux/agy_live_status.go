package tmux

import (
	"regexp"
	"strings"
	"time"
)

// agySpinnerChars lists Antigravity CLI's 8-dot Braille circle spinner alongside
// standard 6-dot Braille spinners.
var agySpinnerChars = []string{
	"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷",
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
}

// agyBusyFooterRe matches Antigravity CLI's bottom status footer while running.
var agyBusyFooterRe = regexp.MustCompile(`(?m)^[ \t]*esc to cancel(?:[ \t]{2,}\S.*|[ \t]*$)`)

// agyComposerIsIdle reports whether Antigravity's live composer footer is
// waiting (`? for shortcuts`) without an active busy footer or onboarding menu.
func agyComposerIsIdle(stripped string) bool {
	return antigravityPrompt.MatchString(stripped) &&
		!antigravityBusy.MatchString(stripped) &&
		!antigravityMenuAfterFooter(stripped)
}

// agyLiveSpinnerContent drops spinner scanning when Antigravity's live
// composer or an interactive/onboarding menu proves the turn is waiting.
func agyLiveSpinnerContent(content string) string {
	stripped := StripANSI(content)
	if agyComposerIsIdle(stripped) || hasOpenInteractiveMenu(stripped) ||
		antigravityMenuRow.MatchString(strings.Join(lastNLines(stripped, 6), "\n")) {
		return ""
	}
	return content
}

// hasAgyLiveBusy reports whether a captured pane shows Antigravity CLI actively
// running a turn (live `esc to cancel` footer or spinner in recent pane).
func hasAgyLiveBusy(content string) bool {
	stripped := StripANSI(content)
	if hasOpenInteractiveMenu(stripped) || agyComposerIsIdle(stripped) ||
		antigravityMenuRow.MatchString(strings.Join(lastNLines(stripped, 6), "\n")) {
		return false
	}
	if antigravityBusy.MatchString(stripped) {
		return true
	}
	if agyBusyFooterRe.MatchString(strings.Join(lastNLines(stripped, 3), "\n")) {
		return true
	}
	_, _, found := findSpinnerInContent(strings.Join(lastNLines(stripped, 15), "\n"), agySpinnerChars)
	return found
}

// AgyForegroundBusySince captures the pane at most once per bgWorkCacheTTL (or
// sooner when notBefore is newer than the last probe) and reports whether the
// live pane proves Antigravity is still running despite a hook transition.
func (s *Session) AgyForegroundBusySince(notBefore time.Time) bool {
	s.mu.Lock()
	if !s.agyBusyCheckedAt.IsZero() && time.Since(s.agyBusyCheckedAt) < bgWorkCacheTTL &&
		!s.agyBusyCheckedAt.Before(notBefore) {
		busy := s.agyBusy
		s.mu.Unlock()
		return busy
	}
	s.mu.Unlock()

	rawContent, err := s.CapturePane()
	if err != nil {
		return false
	}
	busy := hasAgyLiveBusy(rawContent)

	s.mu.Lock()
	s.agyBusy = busy
	s.agyBusyCheckedAt = time.Now()
	s.mu.Unlock()
	return busy
}
