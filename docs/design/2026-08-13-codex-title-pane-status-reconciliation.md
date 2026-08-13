# Codex Title and Pane Status Reconciliation

## Status

Draft design specification, round 3, for task `20260813-codex-status-title-pane-reconciliation`.

## Review Traceability

- **CDX-001** is addressed in Existing smoothing, Explicit Codex Ready, Bounded Codex pane reconciliation, and End-to-end status transitions. Codex demotion no longer depends on a process-local marker surviving into a later public read: Ready plus one agreeing fresh pane sample is sufficient, while an unknown title requires one immediately forced second fresh pane sample.
- **CDX-002** is addressed in Do not skip Codex reconciliation at outer polling gates, State/API/Compatibility Effects, and the web-overlay tests. The wire evidence uses whole-second ordering, conservatively gives live title/pane evidence precedence for an unorderable same-second race, and publishes evidence changes even when the coarse status is unchanged.
- **CDX-003** is addressed by the capture-trigger table in Bounded Codex pane reconciliation and the capture-budget tests. It distinguishes pre-existing demand-driven captures from the added periodic budget and specifies deadline advancement and failure retry eligibility for every capture path.
- **CDX-004** is addressed in Explicit Codex Working, the capture-trigger table, and process-equivalent transition tests. A new Working edge whose reconciliation deadline is already due—including every zero-state cold wrapper—must complete its pane check before the same public status call returns.
- **CDX-005** is addressed in the cross-surface evidence contract and web-overlay tests. The published timestamp/revision now advances on the decisive title or pane observation that establishes or reaffirms the current Codex status, not only on title transitions.

## Problem

Agent Deck can remain wrong in both directions after Codex hook freshness expires:

- A live Codex pane shows `• Working (... esc to interrupt)` while Agent Deck remains `waiting` or `idle`.
- A Codex pane and its tmux title show `Ready` while Agent Deck remains `running`.

The pane patterns themselves are not the primary defect. The strict Codex 0.147+ working-row regex already recognizes captured busy content, and prompt-without-busy content already settles toward waiting or idle. The defect is that `Session.GetStatus` normally captures the pane only after a `window_activity` change, during a spike window, or while its own `lastStableStatus` is `active`. Codex redraws do not reliably advance `window_activity`, and a hook can change the outer `Instance.Status` without updating the tmux session's independent `lastStableStatus`. Consequently:

- a stale `waiting` hook can leave `Instance.Status=waiting` while `tmux.Session.lastStableStatus=waiting`; unchanged activity then prevents capture of the visible Working row; and
- a stale `running` hook can leave `Instance.Status=running` while `tmux.Session.lastStableStatus=waiting`; unchanged activity again prevents capture of the visible prompt/Ready state, so instance-level running-to-waiting confirmation never begins.

The existing generic title fast path does not close the gap. `AnalyzePaneTitle` recognizes Claude-style braille and done markers without knowing the tool. It can incidentally promote a Codex Working title while a braille frame is present, but it does not recognize Codex state words, cannot distinguish a braille rune in user-controlled prefix text, and has no Ready state. The observed dynamic suffixes are:

```text
agent-deck | Working ⠼
agent-deck | Ready
```

Nor can title alone be treated as a durable authority: the prefix is user-controlled, title updates may be unavailable or stale, and historical/cached title data may describe an earlier state.

## Goals

- Recognize an explicit Codex `| Working` title suffix as immediate running evidence.
- Recognize an explicit Codex `| Ready` title suffix as immediate evidence that the pane must be reconciled instead of remaining running.
- Guarantee eventual pane-based convergence in both directions when the title is missing, stale, or ambiguous, even if `window_activity` never changes.
- Preserve notify integration, lifecycle hooks and their current freshness windows, existing pane/process fallbacks, spinner grace, prompt hysteresis, acknowledgment semantics, and behavior for every non-Codex tool.
- Bound added capture cost to low-frequency reconciliation for live Codex-compatible sessions, without adding per-session tmux metadata subprocesses.
- Keep the implementation small enough for an upstream contribution.

## Non-Goals

- Perfect or authoritative Codex execution-state detection.
- Depending on the user invoking `/title`, changing Codex launch configuration, or writing a title from Agent Deck.
- Trusting arbitrary user title text, parsing transcripts, or tracking Codex turns.
- Adding locks, transactions, watchers, UI rewrites, session-anchor changes, or a general status architecture.
- Changing hook installation, trust policy, notify behavior, hook-status files, session identifiers, or the 20-second running / 2-minute waiting freshness windows.
- Changing custom busy/prompt configuration, generic title behavior, shell foreground detection, or status behavior for non-Codex tools.

## Existing Boundaries

### Shared metadata cache

`RefreshPaneInfoCache` already obtains `pane_title`, `pane_current_command`, and `pane_dead` for all panes with one `list-panes -a` operation per status sweep (or one control-mode command when available). `GetCachedPaneInfo` enforces the existing four-second cache TTL. The title feature must consume this cache and must not add a title query per session.

### Hook fast path

`Instance.UpdateStatus` currently returns before tmux polling while a hook is fresh. Preserve the current windows:

- Codex `running`: 20 seconds;
- Codex `waiting`: 2 minutes.

Keep them as advisory edge holds when the cached title is missing, stale, or ambiguous. Before taking the Codex hook fast path, however, inspect the already-warmed title cache. An exact Codex Working or Ready suffix routes through `tmux.Session.GetStatus` instead of returning from the hook path:

- explicit Working bypasses a contradictory fresh waiting hook immediately, then either promotes on a warm not-due edge or completes an already-due pane check before returning;
- explicit Ready forces pane confirmation instead of allowing a fresh running hook to renew running; and
- a title that agrees with the hook may use either path with the same result, but using the tmux path keeps semantic-edge/reconciliation state current.

No freshness constant or hook record changes. If the title is unavailable or unknown, a fresh running/waiting hook keeps today's authority for at most 20 seconds/two minutes, after which pane reconciliation resumes. This preserves the accepted lifecycle-hook fallback while making hooks genuinely advisory when stronger current Codex title evidence exists.

### Pane classifier

When a pane is captured, existing precedence is retained:

1. terminal error banners;
2. explicit busy evidence, including the strict Codex `• Working (... esc to interrupt)` row;
3. background-work rules where applicable;
4. prompt and acknowledgment mapping; and
5. existing startup/waiting/idle fallback.

Busy remains authoritative over Codex's persistent `›` composer. This design changes when Codex is sampled, not the established content grammar or ordering.

### Existing smoothing

There are two distinct tmux smoothing layers and both remain as the default:

- `SpinnerActivityTracker` has a six-second grace period after observed pane busy evidence.
- `shouldHoldActiveOnPromptLocked` holds active for two prompt-without-busy samples before the tmux session settles to waiting or idle.

`Instance.UpdateStatus` also holds the first tmux-derived `running -> waiting/error` result for one confirming instance sample. That process-local marker is safe for the long-lived TUI but cannot be the convergence mechanism for CLI commands or storage-backed web requests: each such request hydrates a fresh `Instance`, samples once, and exits.

The Codex path therefore completes confirmation within one public `UpdateStatus` invocation:

- A semantic `Ready` title is one independent no-busy observation. One successful fresh pane sample that agrees with it by showing a prompt with neither busy nor error evidence is the second observation. This pair satisfies both prompt hysteresis and the outer running-demotion confirmation in that invocation. It may return waiting, or idle when already acknowledged. Busy/error precedence still wins, and a failed capture confirms nothing.
- With an unknown/ambiguous title, ordinary tmux prompt hysteresis remains unchanged. If a successful pane sample eventually yields a `running -> waiting/error` candidate that the outer debounce would hold, `UpdateStatus` performs exactly one uncached forced Codex confirmation sample synchronously. A second agreeing result completes the demotion; disagreement or failure keeps running and schedules no third same-call attempt.
- Ready does not corroborate an error classification. `Ready` plus an error banner therefore uses the same one-for-one forced confirmation as the unknown-title error path.

The existing process-local debounce remains unchanged for non-Codex tools. This narrow Codex completion rule preserves its two-observation safety goal while making repeated fresh wrappers converge instead of recreating the first-sample hold forever. It introduces no persisted confirmation field.

## Design

### 1. Parse a Codex-specific terminal title suffix

Add a separate Codex title parser in `internal/tmux/title_detection.go`; do not broaden or repurpose the current Claude-oriented `AnalyzePaneTitle` contract.

The parser returns a small enum with three values:

```go
type CodexTitleState int

const (
    CodexTitleUnknown CodexTitleState = iota
    CodexTitleWorking
    CodexTitleReady
)
```

`AnalyzeCodexPaneTitle(title string)` performs these steps:

1. strip ANSI presentation with the existing tmux helper, then reject any remaining newline or Unicode control rune;
2. trim outer spaces and tabs;
3. split at the final literal `|` separator and require a non-empty prefix after horizontal trimming; and
4. match the entire trimmed suffix, case-sensitively, as either:

```text
Working
Working [one braille spinner rune]
Ready
```

Implement the Working suffix with explicit rune checks rather than a permissive regular expression: it is exactly `Working`, or `Working` followed by one or more spaces/tabs and exactly one rune in U+2800 through U+28FF. Braille is optional because observed Codex versions/configurations may publish the state word between spinner frames. A title without `|`, an empty prefix, multiple spinner runes, a suffix such as `Working on tests`, `Ready for review`, `Not Ready`, or a state word occurring anywhere except the final suffix returns unknown.

Case-sensitive canonical tokens intentionally reduce false positives from ordinary user titles. The arbitrary text before the final separator is ignored for state parsing but must be non-empty. This supports user-selected dynamic prefixes without accepting a bare tmux title whose entire content happens to be `Working` or `Ready`.

The parser is only called for a `Session` already identified as Codex. It must not infer the runtime from `pane_current_command` or title text: the current command is commonly `bash` during tool execution, and title prefixes are user-controlled. Built-in Codex and custom Codex-compatible tools set a boolean tmux status-detection identity through one small setter when the `Instance` is constructed, hydrated, recreated, or changes tool identity. That setter is status-only metadata; it neither changes the persisted tool name nor the existing pattern override/custom-tool identity.

Use `GetCachedPaneInfoSnapshot` and reject a snapshot taken before the current pane-generation start when that boundary is known, in addition to the existing four-second TTL. For newly started/restarted sessions this is `Session.Created`/startup time. A lazily reconnected pre-existing pane has no trustworthy birth timestamp—its current `Created=time.Now()` is only wrapper construction time—so leave the generation boundary zero and rely on the cache TTL until an actual restart records a boundary. This rejects a previous same-name pane after Agent Deck itself recreates it without rejecting the first valid snapshot of an already-existing reconnected pane.

For a Codex-compatible session, do not subsequently feed the title to generic `AnalyzePaneTitle`; otherwise a braille rune in an unknown Codex title would bypass the strict grammar. `AnalyzePaneTitle` and its precedence for Claude-style spinner/done markers remain behaviorally unchanged for every non-Codex session.

### 2. Apply asymmetric title behavior in `Session.GetStatus`

After liveness/dead-pane checks and before the `window_activity` gate, read the existing cached `PaneInfo` once and interpret titles as follows.

#### Explicit Codex Working

For a Codex-compatible session with `CodexTitleWorking`, including when a contradictory hook is still fresh:

- determine whether reconciliation is due before mutating active/spinner state, and record `lastCodexTitleState=Working` without treating braille-frame changes as new semantic transitions;
- when the deadline is not due, set the same tracker fields as an explicit pane busy match—refresh `lastChangeTime`, mark real activity, clear acknowledgment, reset prompt-without-busy count, mark spinner busy, set `lastStableStatus=active`, and end the startup window—then return active immediately; and
- when the deadline is due, keep the Working result provisional and continue to a pane capture in this same `GetStatus` invocation. Do not seed `lastStableStatus=active`, prompt hysteresis, or spinner grace from the title before that capture.

This preserves the fast promotion for a warm long-lived session while closing the cold-reader gap. A newly hydrated wrapper has a zero attempt time, so reconciliation is due: stale Working+prompt is contradicted before its one-shot `UpdateStatus` returns, while genuine Working plus a strict busy row remains active. Working is a promotion edge, not a permanent lease.

When the due capture succeeds, captured pane precedence replaces the provisional title result for that invocation. The agreement check uses current visible busy evidence, not title-created spinner grace:

- strict busy evidence agrees, clears any contradiction, and returns active;
- prompt/no-busy or error disagrees, marks the current Working observation contradicted, and proceeds through the same bounded demotion/confirmation rules as an unknown title; and
- indeterminate content follows current classifier fallback without allowing title promotion to manufacture a waiting/error state.

On capture timeout/error, preserve the pre-edge stable/outer status rather than publishing the provisional Working promotion, record the attempt, and retry only on a later eligible trigger. This fail-closed rule prevents an unverified stale title from becoming running forever across fresh wrappers while also avoiding a false demotion. Repeated polls with the same semantic title must not promote again after contradiction; only a transition away from Working and back to Working, or pane busy evidence, clears it. Spinner frame changes do not clear the contradiction.

The title parser is Codex-gated before this state mutation. A Claude title, arbitrary shell title, or non-Codex custom tool containing the same suffix keeps existing behavior.

#### Explicit Codex Ready

`CodexTitleReady` does not directly assign waiting or idle. A semantic transition into Ready bypasses a contradictory fresh running hook and forces one uncached immediate poll through the existing pane classifier even when `window_activity` is unchanged. Ready plus a successful agreeing prompt/no-busy pane sample completes the waiting/idle demotion in that invocation. A Ready+error result, which Ready does not corroborate, receives at most one forced fresh confirmation. Once settled or recovered, stable Ready is sampled only on ordinary activity or the ten-second reconciliation deadline.

This is deliberately asymmetric:

- a new canonical Working suffix is strong positive evidence and can promote immediately; braille-frame changes are presentation, not new state edges.
- Ready is negative evidence about work, but the pane still owns error detection, acknowledgment, and the waiting-versus-idle distinction.

Before any capture while the current explicit title is Ready, expire the Codex session's spinner grace for that sample. Otherwise the grace established by the preceding Working observation would cause `hasBusyIndicator` to return true even though the title has explicitly moved to Ready. Record `lastCodexTitleState=Ready` and clear any Working contradiction.

Treat a new Ready title as the first no-busy observation for prompt hysteresis. For an unacknowledged active session, one successful fresh Ready+prompt/no-busy pane sample is the second independent observation and may settle tmux and the outer instance directly to waiting. An acknowledged session maps the same agreeing sample to idle, as it does today. This is not a title-only demotion: the pane must have been read successfully, the title must still be Ready for that decision, and the pane must agree. Missing/ambiguous titles retain the existing two tmux prompt holds and then use the one forced outer-confirmation sample described below.

If capture succeeds, existing pane precedence decides the result. In particular:

- a still-visible strict Working row wins and returns active;
- an error banner returns error;
- an unacknowledged prompt with no busy/error evidence settles to waiting on the Ready transition sample and an acknowledged prompt returns idle, with the title and successful pane sample jointly satisfying the two-observation guard; and
- content with neither prompt nor busy follows today's startup/fallback rules.

If capture times out or fails, preserve `lastStableStatus` exactly as current capture-failure handling does. Ready alone must never turn an indeterminate pane into waiting. If Ready is paired with an error banner, it provides no corroboration for that error; the outer Codex path performs at most one uncached forced confirmation before accepting an error demotion.

### 3. Add bounded Codex pane reconciliation when title is unavailable

Add transient Codex reconciliation fields to `StateTracker`: `lastCodexPaneReconcileAttempt`, `lastCodexTitleState`, and a `codexWorkingContradicted` boolean. Keep the known pane-generation boundary on `Session`, because it changes with process recreation rather than status samples. Keep the outward evidence pair on `Instance` as `codexStatusEvidenceSecond`, `codexStatusEvidenceStatus`, and `codexStatusEvidenceRevision`; `Instance` owns the final mapped/debounced status and is therefore the only layer that can safely pair evidence with what Web/TUI/CLI actually publish. Add one package constant:

```go
const codexPaneReconcileInterval = 10 * time.Second
```

Whenever `GetStatus` is reached for a live Codex-compatible session, it sets `needsBusyCheck=true` when any existing activity condition is true, a semantic Ready transition requests immediate confirmation, or the last attempted Codex reconciliation is at least ten seconds old. This includes polls that reached tmux by bypassing a contradictory fresh hook. A new Working semantic edge may promote without capture only when reconciliation is not due. If reconciliation is due, including the zero/cold state, it must capture and classify before that same `GetStatus` returns.

All Codex status captures go through one small wrapper that accepts a capture reason, records `lastCodexPaneReconcileAttempt` immediately before requesting the pane, and then invokes either the existing cached capture or an explicitly fresh capture for a same-call confirmation. This centralizes accounting without throttling any existing safety path. A capture that actually joins the 500 ms `CapturePane` cache still counts as an attempted observation for the periodic deadline; the forced confirmation invalidates/bypasses that cache so it cannot “confirm” the identical cached result.

The trigger contract is:

| Trigger | Existing or added | May run before 10 s deadline? | Capture form | Advances deadline before attempt? | Failure / next eligibility |
|---|---|---:|---|---:|---|
| `window_activity` changed / first tracker sample | Existing | Yes | Existing cached capture | Yes, for Codex only | Existing result preservation; a new activity event may try again immediately, otherwise periodic retry at +10 s |
| Activity spike-window check | Existing | Yes | Existing cached capture | Yes, for Codex only | Existing spike handling; another existing spike/activity trigger may run before +10 s |
| Sustained-spike confirmation | Existing | Yes | Existing cached capture | Yes, for Codex only | Existing spike reset/preservation; a later existing activity trigger remains eligible |
| Active-state recheck | Existing | Yes | Existing cached capture | Yes, for Codex only | Existing active preservation/fallthrough; the next active recheck remains eligible |
| `GetWindowActivity` fallback | Existing | Yes | Existing cached capture through `getStatusFallback` | Yes, for Codex only | Existing timeout/error behavior; the next fallback call remains eligible because fallback is a pre-existing safety path |
| New Working edge | Added title edge | Yes | Immediate promotion only when periodic is not due; otherwise existing cached capture in the same invocation | Yes when due capture occurs | Busy agrees and returns active; disagreement contradicts and enters bounded demotion; failure preserves pre-edge status; no deferral to a later public call |
| New Ready edge | Added title edge | Yes | Uncached fresh capture | Yes | Failure preserves prior state; no title-edge retry until another semantic edge, activity trigger, or periodic +10 s |
| Periodic reconciliation due | Added periodic | No | Existing cached capture | Yes | Regardless of timeout/error, periodic retry becomes eligible only at +10 s |
| Outer Codex demotion confirmation | Added bounded confirmation | Yes, once per `UpdateStatus` | Uncached fresh capture | Yes | Agreement accepts; disagreement/failure keeps running; no third same-call capture, later activity/title/periodic rules apply |

Thus the added steady-state budget remains at most one periodic request per live Codex session per ten seconds. Existing activity, spike, active-verification, and fallback captures are not newly rate-limited and are not counted in that incremental ceiling; they already occur in the baseline. A due Working edge or semantic Ready edge adds one immediate capture, and a candidate outer demotion adds at most one uncached confirmation. These are transition/cold-load costs, not steady-state leases. Advancing the periodic timestamp on every Codex capture means an existing demand-driven capture satisfies the next periodic observation instead of being followed immediately by a redundant periodic one.

The forced same-call confirmation is owned by `Instance.UpdateStatus`, after the first `GetStatus` result maps to a Codex `running -> waiting/error` candidate backed by a successful decisive pane read. Preserve the public `Session.GetStatus() (string, error)` API for existing consumers, but back it with an internal/enriched status-sample result containing only the Codex facts the instance needs: whether the pane was read successfully, whether the sampled title was Ready, whether that same sample was a Ready+prompt/no-busy/no-error agreement, and an optional decisive evidence candidate `(rawStatus, observedSecond)`. `Instance.UpdateStatus` uses the enriched form so it never infers evidence from mutable session state after the call. A reused `lastStableStatus`, no-capture fallback, spinner/prompt hold, or failed capture cannot start confirmation; it preserves the outer status until an eligible successful pane observation occurs.

When the enriched result does not already provide Ready+prompt agreement, `Instance.UpdateStatus` calls a narrow `tmux.Session.ConfirmCodexDemotion(expected)` helper exactly once. That helper performs a fresh capture and reuses a small extracted pane-classification helper containing the existing error/busy/background/prompt/acknowledgment precedence; it reports whether the independent result equals the expected waiting/error demotion and, on agreement, returns that confirmation observation time. It does not recurse through `UpdateStatus`, rerun liveness/process detection, or mutate the title semantic edge. Ready+prompt bypasses this extra capture because the title and pane are already two independent observations; Ready+error, unknown-title waiting, and unknown-title error require it. Active, inactive, stopped, startup, capture error, and non-Codex results never request it.

Only after mapping and accepting the final outward status does `Instance` commit the evidence candidate to its pair and advance the revision. A held/rejected candidate, prompt/spinner hold, failed capture, or failed confirmation commits nothing. A successful forced confirmation commits the second capture time, not the rejected first candidate. This prevents evidence for a transient tmux candidate from later being paired accidentally with an unrelated outward status.

The interval is measured with `time.Time`, not `window_activity`. Therefore a missing, stale, or ambiguous title and an unchanged activity timestamp can delay correction by at most the current hook's remaining freshness (when present), the next successful ten-second reconciliation, and bounded confirmation:

- `Working -> running`: immediate on a warm not-due explicit Working edge; on a due/cold explicit Working edge, in the same call after an agreeing busy pane; without a strict title, at most the remaining fresh waiting-hook window plus ten seconds to a pane sample.
- `Ready/prompt -> waiting or idle`: one immediate successful sample on explicit Ready; without usable title, at most the remaining fresh running-hook window plus ten seconds to the first pane sample, the existing prompt holds, and one immediate forced confirmation within the same `UpdateStatus` invocation. Idle after prior acknowledgment does not use the running-to-waiting debounce.

Explicit Ready converges from outer running to waiting in the status invocation whose fresh pane sample agrees. With missing/ambiguous title, add any remaining fresh-hook hold, at most the ten-second reconciliation delay, and existing tmux prompt holds; once tmux emits the demotion candidate, the forced fresh confirmation completes or rejects it before that invocation returns. This holds for the long-lived TUI, transition daemon, CLI helpers, and storage-backed/headless web loads, including repeated process-equivalent fresh wrappers. A capture error delays convergence until a later eligible attempt; it never manufactures a state.

The rule is strictly Codex-compatible. It does not turn every status tick or every tool into a pane capture. With `N` live Codex sessions, the maximum added steady-state periodic request rate is `N/10s`; transition confirmations add at most one request per candidate demotion. Connected/focused sessions use the existing control pipe, while other sessions use the already bounded/singleflight subprocess path. Existing status-worker concurrency limits remain in force.

### 4. Do not skip Codex reconciliation at outer polling gates

Two outer optimizations can currently prevent `GetStatus` from running at all and must receive narrow Codex exceptions:

- Before the hook fast path, `Instance.UpdateStatus` must obtain the strict Codex title state from the cached, birth-checked snapshot. A known Working/Ready state bypasses the Codex hook return and calls `GetStatus`; Unknown leaves existing hook behavior intact. Keep this decision in a small pure helper so fresh-running/fresh-waiting/known/unknown precedence is directly testable.
- `Instance.UpdateStatus`'s idle ten-second shortcut must not return early for Codex-compatible sessions when a semantic title edge needs processing or their reconciliation deadline is due. Expose cheap local queries for these conditions; they perform no tmux call.
- The TUI's PipeManager `LastOutputTime > 5s` sweep skip must not skip a Codex-compatible instance when a semantic title edge or the same deadline is due. Title refresh alone does not cure the ambiguous/missing-title case, and a stale Ready/Working title still needs pane confirmation. Any outer confirmation is synchronous inside the selected `UpdateStatus`, so it needs no separately scheduled sweep.

Do not remove either optimization globally. Non-Codex sessions and Codex sessions between reconciliation deadlines retain their current skip behavior.

The web service's storage-backed refresh and CLI status helper already invoke `UpdateStatus` after warming shared tmux caches, so the synchronous confirmation rule covers one-shot callers without any new caller loop. The web in-memory snapshot overlay is different: it reapplies hook files after loading the TUI-published snapshot and has no title observation in its DTO. It must not undo explicit-title reconciliation with either a stale or older fresh contradictory hook.

Hook files expose only `time.Now().Unix()` and are reconstructed at whole-second precision. Do not pretend that a sub-second observation can strictly order a same-second hook. Order the hook against the latest live Codex observation that actually established or reaffirmed the published status—not merely against the last title edge:

- Add `CodexStatusEvidenceAt int64` to lightweight `MenuSessionState`, `MenuSession`, and `sessionstatus.Input`. It is a Codex-only Unix-second evidence bucket, serialized as optional JSON `codexStatusEvidenceAt` with `omitempty`.
- On a semantic Working edge that promotes without capture, record the title observation as evidence for active. When a successful pane capture/confirmation decisively determines the returned Codex status, record that pane observation instead, whether it changes the coarse status or merely reaffirms it. Decisive means current explicit busy/background-work/error evidence, or a prompt/acknowledgment mapping after any prompt hold has settled. Spinner-grace holds, prompt-hysteresis holds, startup/indeterminate branches, failed captures, hook-derived fast paths, skipped polls, and other preserved-prior-state fallbacks do not advance evidence. Ready alone is not evidence for waiting; Ready plus an agreeing prompt pane records the pane observation.
- Record the timestamp and status atomically on `Instance` as `(codexStatusEvidenceSecond, codexStatusEvidenceStatus)` plus a monotonic in-memory `codexStatusEvidenceRevision`. Replace the pair and advance the revision when an accepted qualifying observation changes either the paired status or the Unix-second bucket. A repeated same-status observation in the same second leaves the pair/revision unchanged because it cannot change any hook-ordering result under the conservative tie rule. This avoids publishing on every active recheck while still publishing every representable evidence advance. The status tag prevents a later overlay from treating evidence for an earlier status as support for a subsequently copied/coarse value.
- A value `<= 0` means no live title/pane evidence. It is omitted on the wire and preserves current hook-overlay behavior; old decoders naturally produce zero. `MenuSession` carries only the evidence second because its own `Status` is the paired status. The internal `MenuSessionState` update copies them together.
- For Codex only, a hook may override a snapshot carrying live evidence only when `hook.UpdatedAt.Unix() > CodexStatusEvidenceAt`. An earlier bucket loses. Equality is deliberately conservative: the events are unorderable with the available schema, so the successful live title/pane observation wins. A same-second hook that physically occurred later may be ignored until another hook/title/pane observation; periodic reconciliation still guarantees convergence.
- A hook in a strictly later second retains the existing 20-second running/two-minute waiting freshness policy. Separately narrow `AllowStaleWaiting`: it remains the durable proxy for non-Codex hook tools, but stale Codex waiting falls through exactly like the instance path.

Apply this comparison in both Codex hook consumers, subject to the current-title gate: `Instance.UpdateStatus` first preserves the established rule that a currently strict Working/Ready title routes to live tmux evaluation regardless of hook time. Only when the current title is unknown does its hook fast path compare against `CodexStatusEvidenceAt`. The web overlay has no current-title sample, so `sessionstatus.Derive` compares directly against the published evidence. If a hook agrees with the live-evidence status, retaining the pair is harmless and preserves the newer pane observation for later contradictory files. If a strictly newer contradictory hook applies, the outward status no longer matches the pair and `CodexStatusEvidenceAt` is exported as zero until a later decisive title/pane observation establishes the new live pair. This preserves strict-title precedence while keeping detached/cold surfaces ordered by the evidence they actually possess.

Publishing tracks evidence, not only coarse status or title semantics. `Instance` exposes a lock-consistent `(status, evidenceSecond, evidenceRevision)` snapshot. The TUI status worker compares the evidence revision before and after `UpdateStatus` and calls `publishWebSessionStates` when either status or revision changes. `publishWebSessionStates`, `BuildMenuSnapshot`, and `MemoryMenuData.UpdateSessionStates` copy `Status` and `CodexStatusEvidenceAt` as one pair; the revision is internal change-detection metadata and is not serialized. Therefore these sequences are ordered correctly even though status is unchanged:

- Working title at second 0 -> waiting hook at second 1 -> busy pane reaffirmation at second 2 publishes `(running, 2)`, so the older waiting hook cannot overwrite it.
- Ready+prompt at second 0 -> running hook at second 1 -> prompt/no-busy reaffirmation at second 2 publishes `(waiting, 2)`, so the older running hook cannot overwrite it.
- A hook at second 3 is genuinely newer than either live evidence point and may apply within its existing freshness window.

At every outward snapshot, include `CodexStatusEvidenceAt` only when the evidence's paired status equals the outward `Status`; otherwise emit zero. The same rule applies when `Instance.UpdateStatus` returns through a fresh hook fast path: the hook owns that value, so previously recorded pane/title evidence for a different result is not exported beside it. When live tmux reconciliation later establishes the same or another status, it replaces the pair and advances the revision. This prevents evidence for running from shielding a later hook-derived waiting snapshot, or vice versa.

The timestamp, paired status, and revision are transient evidence, not persisted session state. A storage-backed/headless web load performs live `UpdateStatus`, so its freshly built snapshot receives the latest successful observation directly. A cold snapshot with zero evidence retains today's hook behavior. Tests model both physical same-second orders and assert the declared conservative result instead of relying on impossible sub-second reconstruction.

### 5. Precedence and convergence contract

For a live Codex-compatible session, the effective order is:

1. explicit stopped/dead-pane handling remains highest;
2. a strict, birth-checked Working/Ready title bypasses a contradictory fresh hook; Unknown title leaves the hook fast path authoritative for its bounded window;
3. a new, non-contradicted Working title edge promotes active immediately only when reconciliation is not due; a due/cold edge must complete pane reconciliation in the same public call;
4. a new Ready edge forces pane confirmation and suppresses stale spinner grace, but does not assign status directly; stable Ready is subsequently rate-limited;
5. ambiguous/missing title uses existing activity-triggered capture plus the ten-second Codex reconciliation deadline;
6. captured pane evidence uses existing error/busy/prompt/acknowledgment precedence; and
7. bounded prompt confirmation and synchronous Codex outer confirmation smooth demotion without depending on process-local state across public reads.

This contract resolves an explicit title/hook contradiction immediately and permits an ambiguous-title hook to hold state only for its existing freshness window. It prevents any title, hook, `lastStableStatus`, spinner grace, or unchanged `window_activity` value from keeping the result wrong indefinitely.

## State, API, and Compatibility Effects

- Add transient in-memory title/reconciliation state, paired status-evidence metadata/revision, and an optional whole-second `codexStatusEvidenceAt` field to the web menu snapshot. Nothing is persisted to session JSON or SQLite.
- Add no user configuration, migration, environment variable, hook schema, or required client behavior. The optional web JSON field is backward-compatible and may be omitted when unavailable.
- Continue using the existing pane-info and pane-content capture APIs. A confirmation uses `CapturePaneFresh` to obtain an independent second observation; no new subprocess command shape or watcher is introduced.
- Preserve existing pattern merging and custom tool names. Codex-compatible custom tools receive the compatibility-gated reconciliation without having their visible identity rewritten to `codex`.
- Preserve the shared status enum and all TUI/web/CLI rendering contracts; consumers that ignore the optional observation timestamp are unchanged.
- Restart/reconnect initializes the reconciliation timestamp to zero and semantic title state to Unknown, so the first post-hook-expiry status evaluation samples the pane rather than trusting persisted outer status.

Rollback removes the Codex title parser, compatibility marker, reconciliation timestamp/deadline checks, the Codex-specific stale-web-hook exception, and their tests. No persisted state requires repair.

## Failure Behavior

- Missing or expired pane-info cache: treat title as unknown and rely on periodic pane reconciliation.
- Malformed, user-controlled, or future title shape: treat it as unknown; never guess from substring presence.
- Stale cached title: snapshots before `Session.Created` are rejected; a due/cold Working edge is pane-checked before the same call returns, periodic capture and contradiction state prevent later renewal, and Ready requires pane confirmation before demotion.
- Pane capture timeout/error: preserve the prior stable state. A periodic or title-triggered failure is retried only by a later activity/fallback trigger, a later semantic title edge, or the next ten-second deadline; a failed same-call confirmation never causes a third immediate capture.
- Stale pane history: use the visible pane and existing recent-line/content classifier only; do not capture scrollback or parse transcripts. A copied exact Working UI row remains the already-accepted narrow pattern risk, while a current Ready title disables spinner grace and prompt confirmation bounds demotion.
- Tmux server degradation: existing command timeouts, singleflight, worker limits, and adaptive sweep backoff remain authoritative. A demotion candidate adds at most one fresh confirmation call; reconciliation adds no unbounded retry loop.
- Fresh but contradictory lifecycle hook: strict Working/Ready title evidence bypasses it; without strict title evidence it retains today's bounded hold and the next due pane evaluation converges afterward.

## Tests

### Title grammar and cache behavior

Extend `internal/tmux/title_detection_test.go` with table cases for:

- `<custom> | Working`, `<custom> | Working ⠼`, and each individual braille frame -> Working;
- `<custom> | Ready` -> Ready;
- multiple separators, with only the final suffix interpreted;
- no separator, empty prefix, empty title, lowercase state, `Not Ready`, `Ready for review`, `Working on x`, multiple braille runes, braille in the prefix, newline/control suffixes, and arbitrary prose -> Unknown;
- existing Claude spinner/done cases unchanged; and
- cached pane info older than four seconds cannot promote; after a known local restart, a snapshot predating the pane-generation boundary is also rejected, while cold reconnect with an unknown boundary accepts a fresh snapshot.

### Pure reconciliation policy

Extract the deadline/force decision into a small pure helper where practical and cover:

- a new Codex title Working edge promotes immediately before the deadline, while braille-frame changes do not create new semantic edges;
- Working at/after the deadline, including zero/cold state, forces pane capture before the same call returns; busy agrees, prompt/error contradicts, and failure preserves the pre-edge status;
- a new Ready edge forces capture, suppresses spinner grace, and counts as the first prompt-hysteresis observation; stable Ready is rate-limited after required outer confirmation;
- ambiguous/missing title captures only when the ten-second deadline is due;
- unchanged `window_activity` does not suppress a due Codex capture;
- every tabled capture trigger advances the deadline before its attempt, while pre-existing activity/fallback triggers remain eligible before the deadline;
- periodic/title capture failure advances the attempt timestamp and preserves state, and a failed forced confirmation performs no third immediate attempt;
- a forced outer confirmation is uncached, occurs at most once per candidate demotion, and agreement/disagreement/failure has the specified result;
- preserved/no-capture tmux statuses never create evidence or trigger the forced confirmation;
- non-Codex sessions never become due through this rule; and
- a restarted/reconnected Codex session with zero timestamp is immediately due after hook fallthrough.

### End-to-end status transitions

Add focused tmux/session tests using seeded pane titles and controlled pane content:

- stale `waiting` hook + unchanged activity + explicit Working title -> running immediately when the warm reconciliation deadline is not due; when due/cold, an agreeing busy pane returns running in the same call;
- stale `waiting` hook + missing/ambiguous title + unchanged activity + Working row -> running on the due reconciliation;
- process-equivalent cold wrappers + stale Working title + prompt/no busy -> each due Working edge is pane-checked in that same invocation and cannot publish a provisional running result; once prompt/outer confirmation settles, waiting is returned;
- process-equivalent cold wrappers + genuine Working title + strict busy row -> the same due capture agrees and returns running; a capture failure preserves the pre-edge persisted status rather than guessing either direction;
- stale `running` hook + explicit Ready + prompt/no busy -> waiting (or idle if acknowledged) in the same invocation because Ready plus one fresh agreeing pane sample satisfies bounded confirmation;
- fresh `waiting` hook + explicit Working title -> title bypasses the hook, then returns running immediately when warm/not-due or after an agreeing due pane check; fresh `running` hook + explicit Ready -> title bypasses the hook but still requires the agreeing pane sample;
- prior spinner busy + title Ready + prompt -> Ready suppresses the six-second spinner grace, while title+pane evidence prevents a title-only demotion;
- unknown title + persisted outer running + prompt/no busy -> existing tmux prompt holds, then one uncached agreeing confirmation accepts waiting in the same invocation; disagreement/failure holds running;
- repeated process-equivalent fresh `Instance`/`tmux.Session` wrappers over the same persisted running row converge for both Ready and unknown-title cases, proving no process-local pending marker is required across calls;
- stale Working title + prompt/no busy -> the periodic reconciliation contradicts the title and eventually demotes rather than renewing active forever, including across braille-frame changes;
- Ready title paired with a still-visible strict Working row -> active, proving pane busy evidence wins over a stale/new title race;
- title Ready plus capture timeout -> prior status preserved and retry bounded;
- fresh running and fresh waiting hooks with unknown/missing titles keep their current 20-second/2-minute behavior before reconciliation;
- stale Codex waiting in the web snapshot overlay no longer overwrites pane-derived running; an earlier-second hook loses to later live status evidence, while a hook in a strictly later second may still apply;
- both physical same-second orders (`hook -> live evidence` and `live evidence -> hook`) conservatively resolve to the title/pane-derived snapshot because the hook schema cannot order them; zero/missing evidence preserves current behavior, and non-Codex `AllowStaleWaiting` remains unchanged;
- Working edge second 0 -> waiting hook second 1 -> busy pane reconciliation second 2, with coarse status still running, advances the evidence revision and republishes `(running, codexStatusEvidenceAt=2)`; the overlay keeps running;
- Ready+prompt second 0 -> running hook second 1 -> later prompt/no-busy reconciliation second 2, with coarse status still waiting, republishes `(waiting, codexStatusEvidenceAt=2)`; the overlay keeps waiting;
- a hook genuinely newer than the latest paired title/pane evidence may apply within its existing freshness window;
- `codexStatusEvidenceAt` is omitted for zero, survives snapshot cloning and atomic incremental status/evidence updates when non-zero, and is ignored safely by clients that do not know the field;
- capture-budget instrumentation asserts no more than one added periodic attempt per Codex session per ten seconds, at most one fresh confirmation per candidate demotion, and no cadence change for existing or non-Codex triggers;
- shell, Claude, Gemini, OpenCode, and a non-Codex custom tool retain current title, capture cadence, hysteresis, and hook-overlay behavior.

Where a real tmux integration is needed, keep it narrow and reuse the package's existing test server/helpers. Pure helper tests should carry most edge cases so the suite is deterministic and does not depend on wall-clock sleeps beyond existing integration conventions.

## Validation

Minimum automated validation:

```text
go test ./internal/tmux -run 'Test(AnalyzeCodexPaneTitle|Codex.*Reconcile|Codex.*Ready|Codex.*Working|AnalyzePaneTitle)'
go test ./internal/sessionstatus -run 'TestDerive.*Codex'
go test ./internal/session -run 'Test.*Codex.*(Title|Pane|Hook|Status)|TestDebounceFlipFromRunning'
go test ./internal/web -run 'Test.*Hook.*Codex|Test.*CodexStatusEvidenceAt|TestSessionDataServiceRefreshStatuses'
go test ./internal/tmux ./internal/sessionstatus ./internal/session ./internal/web
```

Manual smoke validation with several live Codex sessions:

1. Observe a titled session transition from `| Ready` to `| Working <spinner>` and confirm Agent Deck becomes running immediately.
2. Let it finish and confirm `| Ready` plus one fresh agreeing prompt sample converges out of running in that status invocation.
3. In a session where `/title` is unavailable or disabled, submit and complete a turn while recording an unchanged `window_activity`; confirm the periodic pane path detects both directions.
4. Temporarily disable/untrust lifecycle hooks and repeat to prove title/pane fallback is independent.
5. Keep a title intentionally stale or ambiguous and confirm pane reconciliation corrects the state within the documented bound.
6. Observe process/tmux load with multiple Codex and non-Codex sessions; verify only Codex sessions add low-frequency captures and no per-session title subprocess appears.

## Risks and Mitigations

- **User title intentionally ends in `| Working` or `| Ready`:** only Codex-compatible sessions and exact canonical final suffixes qualify; semantic-edge tracking, contradiction state, and periodic pane confirmation prevent either value from remaining authoritative indefinitely. Ready never demotes without pane evidence.
- **Codex changes its title grammar:** unknown titles degrade to the bounded pane path. Do not loosen the grammar speculatively.
- **Title/pane update skew at turn completion:** Ready forces a fresh capture, but visible Working remains authoritative. Demotion occurs only when Ready and the current pane agree; an error candidate still requires a second fresh pane observation.
- **Stale Working title after completion:** a warm wrapper may delay demotion until the next ten-second reconciliation; a cold/due wrapper checks the pane before returning, so neither can renew active forever.
- **Historical Working text remains visible at Ready:** the current strict busy row could still match if Codex leaves it on screen. Ready disables only grace, not explicit busy evidence, to avoid false completion during rendering skew. A later visible-pane update/reconciliation must remove it; transcript parsing is outside scope.
- **Added tmux load:** one periodic attempt per live Codex-compatible session per ten seconds is the steady-state ceiling; a transition adds at most one Ready capture and one forced confirmation per candidate demotion. Existing demand-driven captures and non-Codex cadence are unchanged.
- **Cross-surface hook overlay reintroduces a stale result:** stale Codex waiting is excluded from the web's durable-waiting mode; whole-second live status evidence conservatively wins unorderable ties; and every representable status/evidence-bucket advance publishes even when coarse status is unchanged.

## Alternatives Rejected

### Trust title as the sole Codex state

Rejected because title availability is optional, the prefix is user-controlled, updates can be stale, and an exact copied suffix is possible. Title is valuable immediate evidence only when paired with bounded pane reconciliation.

### Ignore titles and capture every Codex pane every status tick

Rejected because it discards a free shared metadata signal and adds avoidable load. A ten-second fallback plus immediate Ready/Working handling guarantees convergence with a much lower steady-state capture rate.

### Widen or replace the existing Working-row regex

Rejected because the focused regex already passes and the failure occurs before it is evaluated. Loosening pane grammar would add false positives without fixing capture starvation.

### Use `window_activity`, content hashes, or process command as the missing authority

Rejected because the reported failure specifically includes unchanged `window_activity`; hashes require a capture first; and `pane_current_command` commonly reports a spawned shell/tool rather than Codex state.

### Let explicit Ready assign waiting immediately

Rejected because Ready does not encode acknowledgment, error state, or a title/pane redraw race. One fresh agreeing pane sample supplies those facts; Ready never assigns status without it.

### Persist or await outer confirmation across public reads

Rejected because persistence would expand the session/storage schema for a transient smoothing detail, while awaiting the next sweep still fails one-shot processes. Completing a bounded fresh confirmation inside the current Codex `UpdateStatus` invocation gives every public surface the same convergence guarantee without migration or caller-specific retry loops.

### Let fresh hooks always override explicit titles

Rejected because it preserves both reported wrong-state windows despite stronger current Codex evidence and contradicts the advisory-hook constraint. Unknown/missing titles still retain the existing bounded hook behavior; only strict Working/Ready suffixes route to tmux reconciliation.

### Change hook freshness windows or install more hook events

Rejected because the requested defect remains when hooks are absent or stale, and the existing windows/integration are explicit preservation constraints. More edge events do not guarantee steady-state convergence.

### Add a general status evidence framework

Rejected as disproportionate. A Codex title enum, one transient deadline, narrow poll-gate exceptions, and focused tests address the observed failures without moving ownership across packages.

## Implementation Scope

Expected implementation and test changes are limited to:

- `internal/tmux/title_detection.go` and `internal/tmux/title_detection_test.go` for strict Codex title suffix parsing;
- `internal/tmux/tmux.go` and focused tmux tests for compatibility gating, centralized capture accounting, fresh demotion confirmation, periodic reconciliation, Ready/Working behavior, and convergence;
- `internal/session/instance.go` plus focused session tests for Codex identity wiring, the idle-gate exception, synchronous Codex outer confirmation, and preservation of the non-Codex debounce;
- `internal/sessionstatus/sessionstatus.go`, the lightweight web menu status/evidence metadata and publisher, and focused tests for whole-second live-evidence-versus-hook ordering, evidence-only publication, and the stale-Codex-waiting exception; and
- the narrow TUI status-sweep gate in `internal/ui/home.go` plus a pure gate test if required to ensure due Codex sessions are not skipped by PipeManager idleness.

No hook command, config file, database schema, watcher, transcript, session anchor, or unrelated tool implementation is part of this change. The only wire addition is one optional timestamp on the existing web menu session DTO; no endpoint or status enum changes.
