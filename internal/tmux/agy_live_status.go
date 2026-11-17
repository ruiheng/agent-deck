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

// agyBusyFooterRe and agyIdleFooterRe match Antigravity CLI's single-line
// bottom shortcut bar rendered below the framed `>` composer:
//
//	busy: "esc to cancel          auto · Orchestrator (Carbon)"
//	idle: "? for shortcuts        auto · Orchestrator (Carbon)"
var (
	agyBusyFooterRe = regexp.MustCompile(`(?m)^[ \t]*esc to cancel(?:[ \t]{2,}\S.*|[ \t]*$)`)
	agyIdleFooterRe = regexp.MustCompile(`(?m)^[ \t]*\? for shortcuts(?:[ \t]{2,}\S.*|[ \t]*$)`)
)

// agyComposerFooterLines locates Antigravity's framed `>` composer near the tail
// of the pane (`────` / `>` / `────`) and returns only the footer rows below it.
// Anchoring to that live composer keeps quoted status lines in assistant prose
// from triggering busy or idle detection.
func agyComposerFooterLines(content string) ([]string, bool) {
	stripped := StripANSI(content)
	lines := lastNLines(stripped, 20)
	minLower := max(0, len(lines)-6)
	for lower := len(lines) - 1; lower >= minLower; lower-- {
		if !isHorizontalRuleLine(lines[lower]) {
			continue
		}
		for upper := lower - 1; upper >= 0; upper-- {
			if !isHorizontalRuleLine(lines[upper]) {
				continue
			}
			if upper+1 < lower {
				prompt := strings.TrimSpace(strings.ReplaceAll(lines[upper+1], "\u00a0", " "))
				if prompt == ">" || strings.HasPrefix(prompt, "> ") {
					return lines[lower+1:], true
				}
			}
		}
	}
	return nil, false
}

// agyBusyPatternContent scopes regex busy/prompt checks to the live composer
// footer when the composer frame is visible, or the last 3 pane lines.
func agyBusyPatternContent(content string) string {
	if footer, ok := agyComposerFooterLines(content); ok {
		return strings.Join(footer, "\n")
	}
	return strings.Join(lastNLines(content, 3), "\n")
}

// agyComposerIsIdle reports whether the live composer footer clearly shows
// `? for shortcuts` without `esc to cancel`.
func agyComposerIsIdle(content string) bool {
	footer, ok := agyComposerFooterLines(content)
	if !ok {
		return false
	}
	joined := strings.Join(footer, "\n")
	return agyIdleFooterRe.MatchString(joined) && !agyBusyFooterRe.MatchString(joined)
}

// agyLiveSpinnerContent drops spinner scanning when Antigravity's live composer
// or interactive menu already proves the turn is waiting on the user.
func agyLiveSpinnerContent(content string) string {
	if agyComposerIsIdle(content) || hasOpenInteractiveMenu(content) {
		return ""
	}
	return content
}

// hasAgyLiveBusy reports whether a captured pane shows Antigravity CLI actively
// running a turn (spinner in recent pane or live `esc to cancel` footer).
func hasAgyLiveBusy(content string) bool {
	stripped := StripANSI(content)
	if hasOpenInteractiveMenu(stripped) || agyComposerIsIdle(stripped) {
		return false
	}
	if agyBusyFooterRe.MatchString(agyBusyPatternContent(stripped)) {
		return true
	}
	_, _, found := findSpinnerInContent(strings.Join(lastNLines(stripped, 15), "\n"), agySpinnerChars)
	return found
}

// hasAgyPrompt reports whether Antigravity CLI is waiting for user input.
func (d *PromptDetector) hasAgyPrompt(content string) bool {
	stripped := StripANSI(content)
	if hasAgyLiveBusy(stripped) {
		return false
	}
	if agyComposerIsIdle(stripped) || hasOpenInteractiveMenu(stripped) {
		return true
	}
	return agyIdleFooterRe.MatchString(strings.Join(lastNLines(stripped, 5), "\n"))
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
