# Codex 0.147 Working-Line Busy Detection

## Status

Accepted design specification for task `codex-pane-status`.

## Problem

Agent Deck v1.11.0 can report a running Codex CLI 0.147+ session as `waiting` even while the pane visibly shows:

```text
• Working (47s • esc to interrupt)
```

Codex now renders that active-work row above blank spacing, its persistent `›` composer, and the footer. In the captured layout the row is roughly five to eight lines from the bottom. The current detector finds Codex's configured `"esc to interrupt"` busy string in the recent 25-line pane slice, but validates interrupt strings only against `lastNLines(content, 3)`. The working row is therefore rejected as lacking nearby busy context. The persistent Codex composer still matches the prompt detector, so both the normal pane path and the content-hash fallback can classify the live turn as `waiting` instead of `active`.

## Goals

- Recognize the captured Codex 0.147+ `• Working (...)` row as authoritative busy evidence when it appears above the persistent composer and footer.
- Preserve the existing protection against ordinary model prose that merely mentions `esc to interrupt`.
- Preserve all existing pane capture, activity-timestamp, process, title, prompt, and content-hash fallback behavior.
- Keep the change local to Codex's built-in busy patterns and add a regression test based on the captured layout.

## Non-Goals

- Redesigning the status state machine or changing busy-versus-prompt precedence.
- Broadening the generic interrupt-string context window.
- Adding a generic TUI-layout abstraction, a new detector interface, or new configuration.
- Changing Codex prompt detection, spinner handling, lifecycle hooks, session hooks, or process detection.
- Supporting speculative Codex layouts that do not contain the captured working-row shape.

## Existing Detection Flow

`Session.hasBusyIndicatorResolved` resolves the tool-specific patterns and examines the last 25 non-trailing pane lines. It evaluates compiled busy regexes first, then busy strings, then spinner/grace-period fallbacks.

The existing interrupt strings remain intentionally guarded: even if `esc to interrupt` occurs in the 25-line slice, an interrupt string is accepted only when `hasInterruptBusyContext` finds the phrase in the final three pane lines with status-like context. That three-line gate prevents model output such as prose or quoted status explanations from making an idle prompt appear busy.

Both `Session.GetStatus` and `Session.getStatusFallback` call the same busy detector before prompt classification. Once the new Codex evidence is recognized, their existing precedence already produces `active`; neither status path needs modification.

## Design

### Add one Codex-only working-row busy regex

In `internal/tmux/patterns.go`, add the following compiled pattern to `DefaultRawPatterns("codex").BusyPatterns`, alongside the existing interrupt strings:

```go
`re:(?mi)^[ \t]*•[ \t]+working[ \t]*\([^\n)]*\besc[ \t]+to[ \t]+interrupt\b[^\n)]*\)[ \t]*$`
```

The negated classes explicitly exclude newlines so the match remains on one physical pane line. Horizontal whitespace classes must be used around the anchored tokens rather than unrestricted `\s`, so the expression cannot consume intervening newlines.

Required semantics:

- `(?m)` makes `^` and `$` apply to individual pane lines inside the existing recent-content slice.
- `(?i)` tolerates capitalization-only presentation changes.
- The literal leading `•`, the word `working`, the parenthesized status payload, and `esc to interrupt` together identify the captured Codex active row.
- Line anchors and the required Codex row shape prevent an inline prose sentence or quoted explanation containing `esc to interrupt` from matching this new path.
- The expression is evaluated by the existing busy-regex loop over the last 25 lines, so a row five to eight lines above the bottom is recognized without weakening the final-three-line interrupt-string guard.

Add a short adjacent comment explaining that Codex 0.147+ renders the active row above its persistent composer/footer and that the anchored shape is intentional false-positive containment.

Do not remove or alter `"ctrl+c to interrupt"`, `"esc to interrupt"`, or `"press esc to interrupt"`. They remain compatibility fallbacks for older layouts whose interrupt affordance is in the final status-bar lines.

### Leave shared detector logic unchanged

No change is expected in `internal/tmux/tmux.go`.

In particular:

- keep `recentLines := lastNLines(content, 25)` unchanged;
- keep `statusBarLines := lastNLines(content, 3)` unchanged for interrupt strings;
- keep `hasInterruptBusyContext` unchanged;
- keep regex-before-string evaluation unchanged;
- keep busy evidence authoritative over the persistent Codex `›` prompt;
- keep spinner tracking and grace-period behavior unchanged; and
- keep the normal activity/tmux-pane path and `getStatusFallback` behavior unchanged.

This boundary directly preserves the existing prose defense and fallback behavior while fixing only the new Codex row shape.

## Regression Tests

Add a focused regression in `internal/tmux/status_fixes_test.go` using a pane string modeled on the captured Codex 0.147 layout. The fixture must contain, in order:

1. ordinary prior output;
2. `• Working (47s • esc to interrupt)`;
3. enough blank/composer/footer lines that the interrupt phrase is outside `lastNLines(content, 3)` and approximately five to eight lines from the bottom;
4. the persistent `›` composer; and
5. a representative Codex model/workdir/context footer.

Construct a fresh `Session` with the detected tool set to `codex`, call `hasBusyIndicator` with the captured-layout fixture, and require `true`. The test should first assert that the fixture's last three lines do not contain `esc to interrupt`; this pins the regression to the new 0.147 layout instead of accidentally passing through the old string guard.

In the same focused test or a companion table case, include Codex-shaped idle content where prose mentions or quotes `esc to interrupt` inline above the composer/footer but does not form the anchored `• Working (...)` row. Require `false`. This supplements the existing `TestEscToInterruptOnlyMatchedInStatusBar` coverage and demonstrates that the new Codex exception does not turn generic prose into busy evidence.

Existing legacy Codex prompt/busy tests and `TestEscToInterruptOnlyMatchedInStatusBar` must continue to pass unchanged unless only comments or fixture names require clarification.

## Validation

The implementation is complete when all of the following hold:

- the captured Codex 0.147 layout is detected as busy even though its interrupt phrase is outside the final three lines;
- the persistent `›` composer cannot cause that same pane to be classified as waiting because existing busy-first precedence remains intact;
- prose-only `esc to interrupt` cases remain not busy;
- older Codex interrupt-line cases still pass through the existing string patterns; and
- the full tmux package test suite passes.

Minimum targeted validation:

```text
go test ./internal/tmux -run 'TestCodex.*Working|TestEscToInterruptOnlyMatchedInStatusBar|TestDefaultRawPatterns_Codex'
go test ./internal/tmux
```

The coder may choose the exact regression-test name, but it should clearly name Codex 0.147 and the working row above the composer.

## Compatibility, Operations, and Rollback

There are no data, API, configuration, migration, or operational changes. The built-in pattern is compiled through the existing `CompilePatterns` path and is automatically used wherever default Codex patterns are resolved. Existing custom pattern overrides retain their current merge and resolution behavior.

Rollback is a direct removal of the new Codex regex and its regression test. No persisted state needs repair.

## Risks and Mitigations

- **Exact copied UI text in model output:** A model could theoretically emit the exact anchored `• Working (... esc to interrupt)` line in the recent pane region. Requiring the complete captured row shape, rather than widening the generic phrase window or treating `•` as a spinner, keeps this risk substantially narrower than the alternatives.
- **Future Codex presentation changes:** A materially different working-row glyph or label will not match. This is intentional for an upstream-suitable narrow fix; future observed layouts should add similarly evidenced patterns rather than loosening this detector speculatively.
- **Regex accidentally spanning lines:** Use horizontal whitespace and a single-line parenthesized payload as specified. The regression fixture and negative prose case should catch an overly permissive implementation.

## Alternatives Rejected

- **Increase the interrupt context window from three to ten or more lines:** This would fix the capture but weaken the established false-positive defense for every interrupt string in the affected tool path.
- **Make the three-line window tool-specific:** This adds detector branching while still accepting loosely shaped prose anywhere in the wider Codex window.
- **Treat `•` as a Codex spinner:** Codex and model output use bullet glyphs decoratively; the non-Claude spinner fallback would then classify unrelated recent bullets as busy.
- **Change prompt precedence or suppress the persistent composer:** Busy-first precedence is already correct, and prompt changes would affect legitimate waiting detection.
- **Add lifecycle-hook integration:** Explicitly separate serial work and outside this design.

## Implementation Scope

Expected modified implementation and test files only:

- `internal/tmux/patterns.go`
- `internal/tmux/status_fixes_test.go`

No lifecycle-hook work is part of this implementation contract.
