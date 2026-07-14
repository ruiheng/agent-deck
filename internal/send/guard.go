package send

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// IsComposerPlaceholder reports whether the visible composer text is Claude's
// idle-suggestion placeholder rather than operator input. Claude renders hint
// suggestions in the empty composer, e.g.:
//
//	❯ Try "write a test for <filepath>"
//
// Treating these as operator drafts would make every automated send hold and
// Ctrl+C an actually-empty composer (issue #1409).
func IsComposerPlaceholder(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, `Try "`) && strings.HasSuffix(t, `"`)
}

// ComposerDraft returns the normalized text currently sitting in the visible
// composer and whether a composer is visible at all. Claude's idle-suggestion
// placeholder is reported as an empty draft. Callers must pass ANSI-stripped
// pane content (same contract as CurrentComposerPrompt).
func ComposerDraft(content string) (draft string, composerVisible bool) {
	body, ok := CurrentComposerPrompt(content)
	if !ok {
		return "", false
	}
	body = NormalizePromptText(body)
	if IsComposerPlaceholder(body) {
		return "", true
	}
	return body, true
}

// ComposerHasDraft reports whether the visible composer holds operator input.
// This is the shared "is the composer busy?" check automated senders must run
// before injecting keystrokes into the pane (issue #1409).
func ComposerHasDraft(content string) bool {
	draft, visible := ComposerDraft(content)
	return visible && draft != ""
}

type styledRune struct {
	r   rune
	dim bool
}

// CodexComposerDraft inspects raw capture-pane -e output and returns the text
// in the bottom Codex composer. Codex renders empty-composer suggestions using
// SGR faint/dim; those suggestions are placeholders, not operator drafts.
// Raw ANSI is required so placeholder styling is not lost before detection.
func CodexComposerDraft(raw string) (draft string, composerVisible bool) {
	lines := ansiStyledLines(raw)
	start := 0
	if len(lines) > 40 {
		start = len(lines) - 40
	}

	for i := len(lines) - 1; i >= start; i-- {
		line := lines[i]
		marker := 0
		for marker < len(line) && (line[marker].r == ' ' || line[marker].r == '\t') {
			marker++
		}
		if marker >= len(line) || line[marker].r != '›' {
			continue
		}

		body := append([]styledRune(nil), line[marker+1:]...)
		for j := i + 1; j < len(lines); j++ {
			continuation := lines[j]
			indent := 0
			for indent < len(continuation) && (continuation[indent].r == ' ' || continuation[indent].r == '\t') {
				indent++
			}
			if indent == len(continuation) {
				// Preserve the ability to find text after an explicitly inserted
				// blank line; the Codex metadata line below bounds the composer.
				continue
			}
			if indent == 0 || looksLikeCodexStatusMetadata(styledText(continuation)) {
				break
			}
			body = append(body, styledRune{r: ' '})
			body = append(body, continuation[indent:]...)
		}

		var visible strings.Builder
		hasNonDimText := false
		for _, sr := range body {
			visible.WriteRune(sr.r)
			if !sr.dim && !strings.ContainsRune(" \t\r\n", sr.r) {
				hasNonDimText = true
			}
		}
		if !hasNonDimText {
			return "", true
		}
		return NormalizePromptText(visible.String()), true
	}
	return "", false
}

func styledText(line []styledRune) string {
	var text strings.Builder
	for _, sr := range line {
		text.WriteRune(sr.r)
	}
	return text.String()
}

func looksLikeCodexStatusMetadata(line string) bool {
	parts := strings.Split(strings.TrimSpace(line), "·")
	return len(parts) >= 4
}

// ansiStyledLines decodes the visible text and faint state from terminal ANSI
// output. Only SGR intensity affects composer classification; other escape
// sequences are consumed without contributing visible text.
func ansiStyledLines(raw string) [][]styledRune {
	lines := [][]styledRune{{}}
	dim := false
	for i := 0; i < len(raw); {
		if raw[i] == '\x1b' {
			i = consumeANSI(raw, i, &dim)
			continue
		}
		r, size := utf8.DecodeRuneInString(raw[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		i += size
		switch r {
		case '\n':
			lines = append(lines, []styledRune{})
		case '\r':
			// capture-pane may include CRLF; CR has no visible width here.
		default:
			lines[len(lines)-1] = append(lines[len(lines)-1], styledRune{r: r, dim: dim})
		}
	}
	return lines
}

func consumeANSI(raw string, start int, dim *bool) int {
	if start+1 >= len(raw) {
		return len(raw)
	}
	switch raw[start+1] {
	case '[': // CSI
		for i := start + 2; i < len(raw); i++ {
			if raw[i] < 0x40 || raw[i] > 0x7e {
				continue
			}
			if raw[i] == 'm' {
				applySGR(raw[start+2:i], dim)
			}
			return i + 1
		}
		return len(raw)
	case ']': // OSC, terminated by BEL or ST (ESC backslash)
		for i := start + 2; i < len(raw); i++ {
			if raw[i] == '\a' {
				return i + 1
			}
			if raw[i] == '\x1b' && i+1 < len(raw) && raw[i+1] == '\\' {
				return i + 2
			}
		}
		return len(raw)
	case '(', ')', '*', '+': // character-set selection plus one final byte
		if start+2 < len(raw) {
			return start + 3
		}
		return len(raw)
	default:
		return start + 2
	}
}

func applySGR(params string, dim *bool) {
	if params == "" {
		*dim = false
		return
	}
	for _, param := range strings.Split(params, ";") {
		code, err := strconv.Atoi(param)
		if err != nil {
			continue
		}
		switch code {
		case 0, 22:
			*dim = false
		case 2:
			*dim = true
		}
	}
}

// CodexComposerGuardResult reports whether a Codex draft remained occupied
// after the bounded hold. The guard never mutates the pane.
type CodexComposerGuardResult struct {
	Held    time.Duration
	Draft   string
	Blocked bool
}

// GuardCodexComposerDraft waits for an operator draft to clear on its own.
// Unlike Claude's guard, it never sends Ctrl+C because that can interrupt the
// active Codex turn. A draft still present at the deadline blocks delivery.
func GuardCodexComposerDraft(t interface{ CapturePaneFresh() (string, error) }, holdWait, pollInterval time.Duration) CodexComposerGuardResult {
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	start := time.Now()
	deadline := start.Add(holdWait)

	for {
		raw, err := t.CapturePaneFresh()
		if err != nil {
			return CodexComposerGuardResult{Held: time.Since(start)}
		}
		draft, visible := CodexComposerDraft(raw)
		if !visible || draft == "" {
			return CodexComposerGuardResult{Held: time.Since(start)}
		}
		if !time.Now().Before(deadline) {
			return CodexComposerGuardResult{
				Held:    time.Since(start),
				Draft:   draft,
				Blocked: true,
			}
		}
		sleepFor := pollInterval
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}
}

// ComposerGuardTarget is the minimal pane surface GuardComposerDraft needs to
// hold an automated send while an operator draft occupies the composer.
// *tmux.Session satisfies it.
type ComposerGuardTarget interface {
	CapturePaneFresh() (string, error)
	SendCtrlC() error
}

// ComposerGuardOptions tunes GuardComposerDraft. All bounds are mandatory so
// the guard can never hold a delivery indefinitely.
type ComposerGuardOptions struct {
	// HoldWait is the maximum time to wait for an operator draft to clear on
	// its own (operator submits or erases it) before falling back to
	// save-clear-restore.
	HoldWait time.Duration
	// PollInterval is the capture cadence during the hold phase.
	// Defaults to 250ms when <= 0.
	PollInterval time.Duration
	// ClearWait is the maximum time to wait, per Ctrl+C attempt, for the
	// composer to actually clear.
	ClearWait time.Duration
	// Strip is applied to raw captured pane content before composer
	// introspection (pass tmux.StripANSI). nil means identity.
	Strip func(string) string
}

// ComposerGuardResult reports what the guard did.
type ComposerGuardResult struct {
	// Held is the total wall-clock time the guard spent before returning.
	Held time.Duration
	// SavedDraft is the operator draft that was cleared to make way for the
	// automated send. Empty when the composer was empty or cleared on its
	// own. Callers must restore it (type it back, without Enter) after the
	// automated delivery is confirmed.
	SavedDraft string
	// DraftCleared is true when the guard issued Ctrl+C and confirmed the
	// composer emptied.
	DraftCleared bool
	// ClearFailed is true when Ctrl+C attempts were exhausted and the
	// composer still held the draft. The caller proceeds with the send
	// regardless (delivery must not be dropped), accepting the residual
	// merge risk for this pathological case.
	ClearFailed bool
}

// maxComposerClearAttempts bounds Ctrl+C attempts during save-clear.
const maxComposerClearAttempts = 2

// GuardComposerDraft implements the composer-collision guard for automated
// sends (issue #1409): an automated SendKeysAndEnter against a composer that
// already holds half-typed operator input would merge with it and submit the
// merged prompt. The guard:
//
//  1. Holds (bounded by HoldWait) while the composer shows a non-empty
//     operator draft, polling for it to clear on its own.
//  2. If the draft is still present at the bound, saves it, clears the
//     composer with Ctrl+C (Claude clears the current input on a single
//     Ctrl+C; same primitive the full-resend recovery path already uses)
//     and confirms the clear, bounded by ClearWait per attempt.
//
// The guard never blocks delivery indefinitely and never errors: on capture
// failures or a composer that refuses to clear it returns and lets the caller
// proceed, because watchers/conductors depend on the send going through.
func GuardComposerDraft(t ComposerGuardTarget, opts ComposerGuardOptions) ComposerGuardResult {
	strip := opts.Strip
	if strip == nil {
		strip = func(s string) string { return s }
	}
	poll := opts.PollInterval
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}

	start := time.Now()
	deadline := start.Add(opts.HoldWait)
	lastDraft := ""

	for {
		raw, err := t.CapturePaneFresh()
		if err != nil {
			// Pane not introspectable: never block delivery on it.
			return ComposerGuardResult{Held: time.Since(start)}
		}
		draft, visible := ComposerDraft(strip(raw))
		if !visible || draft == "" {
			return ComposerGuardResult{Held: time.Since(start)}
		}
		lastDraft = draft
		if !time.Now().Before(deadline) {
			break
		}
		sleepFor := poll
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}

	// Hold bound reached with the operator draft still present: save it and
	// clear the composer so the automated message cannot merge with it.
	res := ComposerGuardResult{SavedDraft: lastDraft}
	clearPoll := poll
	if clearPoll > 100*time.Millisecond {
		clearPoll = 100 * time.Millisecond
	}
	for attempt := 0; attempt < maxComposerClearAttempts; attempt++ {
		if err := t.SendCtrlC(); err != nil {
			break
		}
		clearDeadline := time.Now().Add(opts.ClearWait)
		for {
			raw, err := t.CapturePaneFresh()
			if err == nil && !ComposerHasDraft(strip(raw)) {
				res.DraftCleared = true
				res.Held = time.Since(start)
				return res
			}
			if !time.Now().Before(clearDeadline) {
				break
			}
			sleepFor := clearPoll
			if remaining := time.Until(clearDeadline); remaining < sleepFor {
				sleepFor = remaining
			}
			if sleepFor > 0 {
				time.Sleep(sleepFor)
			}
		}
	}
	res.ClearFailed = true
	res.Held = time.Since(start)
	return res
}
