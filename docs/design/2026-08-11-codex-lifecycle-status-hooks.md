# Codex Lifecycle Status Hooks

## Status

Accepted design specification for task `codex-lifecycle-hooks`.

## Problem

Agent Deck's current `codex-hooks install` command only installs Codex's legacy/general notification command:

```toml
notify = ["agent-deck", "codex-notify"]
```

That notification remains useful for completion compatibility, but observed local records contain completion events and no reliable turn-start events. As a result, Agent Deck can miss the start of a Codex turn and must wait for pane/process heuristics to recognize that Codex is working.

Codex 0.147.0 exposes stable lifecycle hooks, including `UserPromptSubmit` and `Stop`. These can provide useful start and stop edges, but they are advisory rather than authoritative:

- all matching hooks from all sources run;
- handlers for the same event run concurrently;
- another `UserPromptSubmit` hook can block a prompt after Agent Deck's handler has already run; and
- another `Stop` hook can request automatic continuation after Agent Deck's handler has already observed the stop.

The integration therefore must improve the common case without claiming a perfect state machine or weakening the existing notify, pane, process, status-title, and freshness fallbacks.

Official behavior references:

- [Codex hooks](https://learn.chatgpt.com/docs/hooks)
- [Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference)

## Goals

- Add a prompt-start edge that normally marks a managed Codex session `running` as soon as Codex accepts a user prompt for processing.
- Add a turn-stop edge that normally marks the session `waiting` when Codex reaches `Stop`.
- Preserve `notify = ["agent-deck", "codex-notify"]` as an independent completion-compatible source.
- Keep the existing Codex hook freshness policy: `running` is authoritative for 20 seconds, `waiting` for 2 minutes, and stale/missing hooks fall through naturally.
- Preserve pane/process/status-title detection, including the Codex working-row fix at commit `890a505c`.
- Install and uninstall only Agent Deck-owned lifecycle handlers while preserving unrelated user hooks and all plugin/managed/project hooks.
- Make repeated install, upgrade, status, and uninstall operations deterministic and idempotent.
- Keep Agent Deck's lifecycle hook process observational: it must never block a prompt, rewrite input, add model context, or request a continued turn.
- Keep the implementation deliberately small and centered in the existing Codex command and tests.
- Bound lifecycle payload input by reusing the existing main-package limit, without adding shared infrastructure.

## Non-Goals

- A fully authoritative Codex state machine.
- Generalizing all tool integrations behind a new cross-tool hook framework.
- Installing speculative events such as `PreToolUse`, `PostToolUse`, `PermissionRequest`, `SessionStart`, `SessionEnd`, or subagent hooks.
- Changing the generic Claude/Cursor/Hermes event mappings.
- Changing the 20-second/2-minute Codex freshness windows.
- Replacing pane, process, prompt, status-title, or notify detection.
- Automatically trusting hooks, bypassing Codex's trust review, or forcing `[features].hooks = true`.
- Fixing unrelated Codex config-directory resolution behavior.
- Parsing Codex transcripts or adding Claude-style done-sentinel behavior.
- Changing hook-status or session-anchor writers, readers, schemas, temporary-file behavior, sandbox scoping, watcher precedence, or UI/session binding.
- Adding hook-state transactions, file locks, config locks, symlink hardening, cross-process serialization, or adversarial sandbox guarantees.
- Perfect detection, perfect atomicity, or recovery from every concurrent/crash interleaving.

## Existing Components and Boundaries

### Notify bridge

`cmd/agent-deck/codex_hooks_cmd.go` already provides the internal `agent-deck codex-notify` command. It accepts Codex notification payloads from argv or stdin, extracts an event and session/thread id, maps recognized turn events to `running` or `waiting`, and writes the shared Agent Deck hook-status file.

This bridge is already deliberately narrow: it does not print hook decisions or model-visible output. It is therefore the correct executable for the new Codex lifecycle handlers after its parser is extended to recognize lifecycle payload fields.

### Generic hook handler

`cmd/agent-deck/hook_handler.go` must not be used as the Codex lifecycle command. Although it parses the common `hook_event_name` payload shape, it also contains Claude-oriented behavior outside this task's scope, including title/cwd synchronization, children-context output, done-sentinel scanning, permission decisions, and Stop-time inbox draining that can emit `{ "decision": "block" }`.

Using `agent-deck hook-handler` for Codex would risk changing prompt or Stop behavior and would also reuse the generic `PostToolUse -> waiting` mapping that is incorrect for Codex's multi-tool turn loop.

### Status fallback

`internal/session/instance.go` already treats fresh Codex hook files as a fast path and uses these state-specific windows:

- `running`: 20 seconds;
- `waiting`: 2 minutes.

Once an edge ages out, existing tmux pane content, foreground process, prompt, status-title, and session synchronization logic resumes control. No status-state-machine change is required for this design.

### Existing hook-state writers

`writeHookStatusWithScan` currently writes through one fixed `<instance>.json.tmp`, and `session.WriteHookSessionAnchor` writes through one fixed `<instance>.sid.tmp`. The existing notify command and the new lifecycle handlers can overlap, and Codex may also launch matching hook definitions from multiple sources concurrently. Fixed temp names allow one process to overwrite another process's in-progress temp file or make a later rename fail.

This design deliberately accepts those existing best-effort semantics. It does not add unique temps, transactions, locks, source-directory metadata, anchor coordination, watcher changes, or sandbox-specific state handling. A racing lifecycle/notify process may lose an edge or leave status and anchor temporarily inconsistent; the existing freshness expiry, pane/process/status-title detection, session ownership guards, and later hook events remain the fallback.

## Chosen Event Set

Install exactly two lifecycle events:

| Codex event | Agent Deck status | Purpose |
| --- | --- | --- |
| `UserPromptSubmit` | `running` | Earliest broadly useful root-turn start edge. |
| `Stop` | `waiting` | Root-turn stop edge; indicates Codex is normally returning to the prompt. |

Do not configure a matcher for either event. Official Codex behavior currently ignores matchers for `UserPromptSubmit` and `Stop`, so omitting it avoids a misleading configuration.

### Why no additional events

- `PostToolUse` is explicitly excluded. It is a mid-turn tool boundary; Codex may immediately reason or call another tool, so mapping it to `waiting` would create repeated false idle flips.
- `PreToolUse` could refresh a long turn but is not a turn-start edge and would broaden the event surface. After the 20-second start edge expires, the existing pane/process path is the intended fallback.
- `PermissionRequest` would need a paired, reliable resume edge to avoid leaving the session `waiting` mid-turn.
- `SessionStart` is not a user-turn start. Initial prompt state is already covered by notify and pane/process detection.
- `SessionEnd` and subagent events do not materially improve the requested root-turn start/stop accuracy.

## Lifecycle Payload Parsing and Mapping

Extend the existing Codex bridge rather than adding a second behaviorally overlapping executable.

### Payload fields

Add one decoded field to the existing Codex payload structure:

```go
HookEventName string `json:"hook_event_name"`
```

The parser continues returning only `event` and `sessionID`.

Event precedence is:

1. non-empty `hook_event_name`;
2. existing `type`;
3. existing `event`;
4. existing `method`;
5. existing nested `params`/`payload` fallbacks.

Session-id precedence remains compatible with the current notify path: `session_id`, `thread_id`, `thread-id`, then nested equivalents. `CODEX_SESSION_ID` remains the final environment fallback.

### Mapping

Before the existing fuzzy notification aliases, recognize normalized lifecycle names exactly:

```text
UserPromptSubmit -> running
Stop             -> waiting
```

Existing notify mappings such as `agent-turn-complete`, `turn/completed`, failures, aborts, cancellations, and recognized turn-start aliases remain unchanged.

Unknown lifecycle events must remain a no-op. In particular, a `PostToolUse` payload must not create or update a hook-status file.

### Input bound

Reuse the existing `maxHookPayloadSize` constant (`1 << 20`, 1 MiB) from the main package. `UserPromptSubmit` includes the full prompt, so the current unbounded `io.ReadAll(os.Stdin)` is not acceptable once lifecycle hooks invoke this path.

- For stdin, read through `io.LimitReader(os.Stdin, maxHookPayloadSize+1)`.
- If the resulting payload exceeds `maxHookPayloadSize`, return silently before JSON parsing or any file write.
- Apply the same byte limit to a JSON payload supplied in argv; do not allow the compatibility argv path to bypass the bound.
- A plain non-JSON event argument may be accepted only when its byte length is within the same limit.

Oversized input must produce no status file, no session-anchor update, and no stdout. Do not truncate and parse a prefix, because that could turn one oversized prompt-bearing object into a different valid event.

### Safety and output contract

The bridge must write nothing to stdout for all lifecycle events. Exit code 0 with no output is a successful no-op in Codex's hook contract. This guarantees that Agent Deck itself cannot:

- block `UserPromptSubmit`;
- add prompt context;
- return `continue: false`; or
- return `decision: block` from `Stop` and trigger automatic continuation.

Do not persist `turn_id`; no current status consumer needs it, and adding a turn-correlation state machine is outside scope.

## Existing Best-Effort Hook-State Boundary

Lifecycle events reuse `writeHookStatus` exactly as the existing notify path does. No `internal/session` or generic hook-writer code changes are part of this design.

This intentionally leaves the current fixed temp names, independently updated `.json` and `.sid` files, flat/scoped behavior, and concurrent writer races unchanged. The new lifecycle edges are opportunistic hints, not a correctness-critical transaction. A missed, overwritten, stale, or temporarily mismatched edge is accepted because the existing 20-second/2-minute freshness expiry and pane/process/status-title/notify paths remain authoritative fallbacks over time.

## Handling Concurrent Block and Continuation Semantics

This design intentionally treats both lifecycle edges as observations, not final decisions.

### Blocked `UserPromptSubmit`

Agent Deck's handler can record `running` even when a different concurrently launched hook later blocks the prompt. Matching handlers cannot observe one another's result, so the installer must not imply otherwise.

The bounded behavior is accepted:

- Agent Deck's own handler never blocks or modifies the prompt;
- a false `running` edge is authoritative for at most the existing 20-second Codex running window; and
- pane/process detection resumes after expiry.

No sleep, delayed child process, inter-hook lock, or speculative prompt-result protocol should be introduced.

### Continued `Stop`

Agent Deck can record `waiting` even when a different concurrent `Stop` hook requests another continuation. Agent Deck must not attempt to participate in or override that decision.

The bounded behavior is likewise accepted:

- if Codex emits the continuation through a subsequent `UserPromptSubmit`, the newer `running` edge replaces the `Stop` edge;
- otherwise the existing 2-minute waiting freshness expires and pane/process detection resumes; and
- the separate Codex notify integration remains installed as the existing completion-compatible signal.

This limitation must be documented in code comments and CLI/help text as a pragmatic status improvement, not authoritative execution state.

## Configuration Source

### Choose user-level `hooks.json`

Install lifecycle handlers in:

```text
<Codex home>/hooks.json
```

where `<Codex home>` is the directory containing the existing `getCodexConfigPath()` result. With the default setup this is `~/.codex/hooks.json`; when `CODEX_HOME` is set it is `$CODEX_HOME/hooks.json`.

Add one file-local path helper, `getCodexHooksPath()`, implemented as `filepath.Join(filepath.Dir(getCodexConfigPath()), "hooks.json")`; do not add another Codex-home resolver.

Use a dedicated JSON source rather than adding inline TOML hook tables because:

- the lifecycle schema is naturally represented and structurally merged as JSON;
- the installer can identify and remove exact Agent Deck handler objects without editing unrelated TOML tables;
- it avoids table-scope hazards from appending/prepending `[[hooks...]]` blocks around arbitrary existing root keys; and
- it keeps the existing marker-managed notify setting separate from lifecycle handler ownership.

Do not dynamically switch between JSON and TOML. Supporting two mutation formats would double the install/uninstall/test surface and make ownership recovery ambiguous.

### Existing inline hooks

Official Codex behavior merges user `hooks.json` and inline `[hooks]` from the same layer and may warn when both representations are present. Leave inline user hooks untouched and let Codex surface any mixed-representation warning; do not add TOML hook parsing or migration logic to this command.

### Plugin, project, and managed hooks

Do not inspect or modify plugin, project, system, MDM, cloud, or `requirements.toml` hook sources. Codex loads those sources alongside the user source. This preserves their independent lifecycle and trust policy.

## Canonical Hook Definitions

The newly created portion of `hooks.json` is equivalent to:

```json
{
  "description": "Agent Deck Codex lifecycle status hooks.",
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "agent-deck codex-notify",
            "timeout": 3
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "agent-deck codex-notify",
            "timeout": 3
          }
        ]
      }
    ]
  }
}
```

The 3-second timeout bounds a missing/hung `agent-deck` executable while leaving ample time for the existing local status write. Use the PATH-resolved `agent-deck` command, matching the existing notify configuration; do not write an absolute binary path that becomes stale after upgrades or moves.

## JSON Preservation Algorithm

Implement Codex-specific helpers in `cmd/agent-deck/codex_hooks_cmd.go`; do not introduce a generic hooks framework.

Parse the document progressively with `json.RawMessage`:

1. Parse the top level as `map[string]json.RawMessage`. Invalid JSON or a non-object is an error; never overwrite it.
2. Parse `hooks`, when present, as `map[string]json.RawMessage`. A non-object is an error.
3. For `UserPromptSubmit` and `Stop`, parse the event value as an array of raw matcher-group objects. A malformed configured event is an error.
4. Each owned-event group must be an object; a present `hooks` field must be an array. Decode only handler `type` and `command` for ownership checks. Preserve non-owned/raw handlers, groups without a `hooks` field, every other group field, unrelated events, and top-level fields.
5. An event is installed when any handler has `type == "command"` and exact trimmed command `agent-deck codex-notify`.
6. On install, append one canonical matcher group only when that event lacks the handler. Do not add duplicates and do not rewrite the file at all when both handlers are already present.
7. On uninstall, remove every exact Agent Deck handler from the two owned events. Remove an empty canonical group; preserve mixed groups and all remaining handlers. Delete an event key only when no groups remain.

When creating a new file, include the Agent Deck description. When modifying an existing file, preserve its existing `description` and all other top-level keys. On uninstall, remove the exact Agent Deck-created description only when no Agent Deck handlers remain and it is the current description; retain the valid residual JSON document even if it becomes `{}` or contains an empty hooks object. Never delete `hooks.json`, because file provenance is ambiguous once a user has had an opportunity to edit it.

When a lifecycle document changes, marshal it with deterministic indentation and write it with the same direct `os.WriteFile(..., 0644)` style the existing Codex notify installer uses. This follows an existing final symlink and normally preserves an existing target mode, but it is not an atomic read-modify-write and adds no new symlink or crash hardening. That limitation is accepted for this minimal change.

## Configuration Mutation Boundary

Do not change `internal/session/codex_trust.go` or add/reuse any config lock. The command may use small pure planning helpers inside `codex_hooks_cmd.go` so it can validate both files before its own writes, but concurrent trust, install, uninstall, or external edits can still race and lose updates. That existing class of limitation is explicitly accepted.

Keep the helper surface private and Codex-specific: one notify install/uninstall planner over TOML text, one lifecycle document inspect/mutate helper over `[]byte`, and small component-state enums used by install/status output. Do not create a reusable config transaction or generic hook-document package.

## Install, Uninstall, and Status Flow

### Install

Refactor the current notify manipulation into a pure planning helper so both target files can be validated before either is written.

1. Resolve `config.toml` and sibling `hooks.json`.
2. Read and preflight the current notify content using today's compatibility rules:
   - upgrade Agent Deck's marker block or legacy `[notify] program = ...` form;
   - accept the exact current notify array as already installed;
   - reject a conflicting custom notify setting without overwriting it.
3. Read and preflight `hooks.json` with the preservation algorithm above.
4. If either preflight fails, write neither file.
5. Write a changed notify config first with the existing direct-write behavior.
6. Write changed lifecycle JSON second with direct `os.WriteFile`.
7. Report each component as installed/already installed, both file paths, and the trust-review instruction.

The two files cannot be committed atomically together. A filesystem failure after the first write may leave notify-only partial state; this is safe, explicitly reportable, and repaired by an idempotent retry.

### Uninstall

Read and preflight both files before mutation. Remove Agent Deck lifecycle handlers first, then remove only the existing Agent Deck notify marker/exact/legacy form using the command's existing direct-write behavior. This ordering preserves the known notify fallback if the second write fails. A custom notify setting is left unchanged while owned lifecycle handlers are still removed; never remove unrelated lifecycle handlers.

### Status

Report the aggregate and components separately:

```text
Status: INSTALLED | PARTIAL | NOT INSTALLED | ERROR
Notify: INSTALLED | LEGACY | CUSTOM | NOT INSTALLED
Lifecycle: INSTALLED | PARTIAL | NOT INSTALLED | INVALID
Config: <.../config.toml>
Hooks: <.../hooks.json>
```

`INSTALLED` requires both the exact Agent Deck notify integration and both lifecycle event handlers. A pre-existing notify-only setup becomes `PARTIAL` until the user reruns install.

Status must not claim that lifecycle hooks are trusted, enabled, or currently executing. When configured, print that Codex trust is reviewed through `/hooks` and cannot be inferred from these files.

Update CLI help wording from "Codex notify hook integration" to "Codex notify and lifecycle hook integration."

## Trust and Feature Compatibility

Non-managed command hooks require Codex trust review of the exact definition. Installation must not edit Codex's trust store or use `--dangerously-bypass-hook-trust`.

After adding or changing lifecycle handlers, print an instruction equivalent to:

```text
Open /hooks in Codex and trust the Agent Deck UserPromptSubmit and Stop hooks.
Until trusted and enabled, Agent Deck continues using notify and pane/process fallback.
```

If `[features].hooks = false`, `allow_managed_hooks_only = true`, an older Codex build ignores the source, or the command cannot run, the integration simply produces no lifecycle edge. Notify and pane/process detection remain intact. Do not modify these settings automatically.

## Compatibility and Failure Handling

- Existing `agent-turn-complete` and other notify aliases continue to map exactly as before.
- Duplicate notify and lifecycle stop writes converge on the same `waiting` status file; existing status/transition deduplication remains responsible for duplicate observations.
- A missing `AGENTDECK_INSTANCE_ID` remains a silent no-op, so global user hooks do not create unmanaged status files.
- Malformed payload JSON, missing/unknown events, and unsupported lifecycle events are silent handler no-ops.
- Lifecycle or notify payloads larger than 1 MiB are rejected without truncation, output, status write, or anchor write.
- Invalid user `hooks.json` is an install/uninstall error and is never replaced.
- A custom notify configuration remains a concrete conflict and is never overwritten.
- Existing hook-status and anchor write races, fixed temp names, flat/scoped sidecar behavior, and crash windows remain unchanged and may lose or mismatch an advisory edge.
- Config and lifecycle files are not locked or committed together. A failed second write or concurrent external/trust mutation can leave partial state or lose an update; status output and idempotent reruns are the recovery path for Agent Deck-owned entries.
- Direct `os.WriteFile` behavior remains in use; no new atomic-file, permission, or symlink guarantees are introduced.
- No changes are required to the existing Codex freshness constants or the pane/process/status-title detector.

## Tests

Add focused tests in `cmd/agent-deck/codex_hooks_cmd_test.go`.

### Parser and mapping

- `UserPromptSubmit` lifecycle JSON from stdin writes `running`, the exact event, and the existing session-id fields.
- `Stop` lifecycle JSON writes `waiting`.
- `PostToolUse` and other uninstalled/unknown lifecycle events write nothing.
- The lifecycle path writes no stdout for both `UserPromptSubmit` and `Stop`.
- Existing argv/stdin notify payload tests and all current turn-complete/start aliases remain unchanged.
- Missing instance id and malformed payloads remain no-ops.
- An stdin payload of `maxHookPayloadSize+1` bytes and an oversized argv JSON payload produce no stdout, status file, or anchor update.

### Install and upgrade

- Fresh install writes the notify setting and the two canonical JSON handlers.
- Installing twice does not change bytes or add duplicate handlers.
- Notify-only legacy/current installations are upgraded to the full two-component state.
- Existing unrelated top-level JSON fields, unrelated events, matcher fields, sibling handlers, and handler-specific fields remain semantically unchanged.
- Existing Agent Deck handlers under one event cause only the missing event to be appended.
- Invalid JSON, non-object `hooks`, and malformed owned event arrays fail without writing either file.
- Existing inline TOML hooks are not modified.

### Uninstall and status

- Uninstall removes only exact Agent Deck handlers and the Agent Deck notify integration.
- A sibling handler in the same event survives.
- An unrelated event and plugin/user metadata survive.
- Uninstall removes the exact Agent Deck-created description when appropriate but never deletes `hooks.json`; residual user documents remain valid.
- Status distinguishes installed, notify-only partial, lifecycle-only partial, custom notify, invalid lifecycle JSON, and absent state.

### Accepted limitation coverage

- Keep existing hook-status/session-anchor tests unchanged; do not add concurrency, transaction, sandbox-source, watcher, or rollback tests for this feature.
- Use planner/helper tests to prove invalid lifecycle JSON fails before the command's own writes and that idempotent reruns repair notify-only or lifecycle-only partial states.
- No test should imply protection from concurrent external edits, trust mutations, process crashes, or overlapping hook writers.

Existing status-freshness and Codex transition tests should continue to pass unchanged. Add a narrow assertion only if needed to prove that lifecycle `Stop` remains an ordinary waiting edge and that no change was made to the 20-second/2-minute fallback policy.

## Validation

Minimum targeted validation:

```text
go test ./cmd/agent-deck -run 'Test(MapCodexNotify|HandleCodexNotify|CodexHooks)'
go test ./cmd/agent-deck
```

Manual smoke validation with Codex 0.147+:

1. Run `agent-deck codex-hooks install` in a temporary `CODEX_HOME`.
2. Confirm existing unrelated hooks remain in `hooks.json`.
3. Start Codex, open `/hooks`, and trust only the new Agent Deck definitions.
4. Submit a prompt and observe a `UserPromptSubmit -> running` status record.
5. Let the turn finish and observe `Stop -> waiting` and/or the existing notify completion edge.
6. Run a multi-tool turn and confirm `PostToolUse` does not flip the session to waiting.
7. Disable or untrust the lifecycle hooks and confirm notify plus pane/process detection still function.
8. Run uninstall and confirm unrelated hooks and config remain.

## Risks and Mitigations

- **Blocked prompt produces a false start:** unavoidable under concurrent hook semantics; bounded by the 20-second running freshness and fallback.
- **Continued Stop produces a false wait:** unavoidable without participating in Codex's decision protocol; a later start edge can overwrite it, otherwise the 2-minute waiting window expires.
- **Mixed inline and JSON sources produce a Codex warning:** leave inline hooks untouched and rely on Codex to surface the warning; do not add a second mutation strategy.
- **User hook file contains unknown/future fields:** preserve raw unrelated objects and fail closed on malformed owned structures.
- **Agent Deck command is unavailable in hook PATH:** the 3-second timeout bounds impact; notify/pane fallback continues and Codex exposes hook failure through `/hooks`.
- **Trust not granted:** expected safe state; installation is configuration only and status output must not claim execution.
- **Two-file partial write:** preflight both, write notify first on install and lifecycle first on uninstall, report component status, and rely on an idempotent rerun. No cross-file transaction is added.
- **Concurrent notify/lifecycle writers:** accepted. Existing fixed temps and independent status/anchor updates can lose or mismatch an edge; freshness expiry and fallback detection bound the impact.
- **Concurrent config/trust/external edits:** accepted. No config lock or atomic read-modify-write is added, so a stale writer can overwrite another update.
- **Crash, symlink, permission, or adversarial sandbox edge cases:** retain existing direct-write and hook-state behavior; no new guarantee is claimed.
- **Prompt-bearing lifecycle payload is oversized:** enforce the shared 1 MiB limit before parsing and silently reject rather than truncate.

## Alternatives Rejected

### Reuse `agent-deck hook-handler`

Rejected because it contains Claude-specific output and side effects, including Stop continuation decisions, and its generic `PostToolUse -> waiting` mapping is wrong for Codex.

### Install `PreToolUse` and `PostToolUse`

Rejected because this expands a two-edge design into noisy mid-turn tracking. In particular, `PostToolUse` is not a turn completion boundary.

### Use only `PreToolUse` for start

Rejected because turns without tools would still have no start edge, and the first tool may occur well after the prompt was accepted.

### Add inline TOML lifecycle blocks

Rejected because safe preservation and removal around arbitrary existing TOML is more fragile, especially with table scoping, and would couple lifecycle ownership to the notify marker block.

### Dynamically choose JSON or TOML

Rejected because it requires two mutation/uninstall engines, complex recovery rules, and a much larger compatibility matrix for little status benefit.

### Delay or debounce hook subprocesses to discover sibling decisions

Rejected because concurrently launched hooks cannot directly observe final aggregate decisions, and background/delayed reconciliation would add processes and state without becoming authoritative.

### Add hook-state transactions, locks, or sandbox hardening

Rejected by the explicit minimal-change requirement. Fixed temp collisions, independent status/anchor updates, scoped-source inconsistencies, and adversarial sandbox mutation remain known limitations of the existing best-effort signal path.

### Add config locking or atomic/symlink-preserving writes

Rejected for this upstream-sized change. The command retains its current direct-write style and accepts partial two-file state and lost-update races rather than modifying `internal/session` trust-lock or `internal/atomicfile` architecture.

### Replace notify with lifecycle `Stop`

Rejected because `Stop` can be continued and requires trust. Notify is an existing independent compatibility path and must remain installed.

## Implementation Scope

Expected implementation changes are limited to:

- `cmd/agent-deck/codex_hooks_cmd.go`
  - add `hook_event_name` parsing, exact lifecycle mapping, and the small reused payload bound;
  - add Codex-specific `json.RawMessage` install/remove/status helpers for user `hooks.json`;
  - refactor the existing notify mutation into narrow local planning helpers and orchestrate the two direct file writes;
  - print component status and trust/fallback guidance.
- `cmd/agent-deck/codex_hooks_cmd_test.go`
  - lifecycle parser/mapping/output/bound tests plus JSON ownership, preservation, idempotency, partial-state, uninstall, and status coverage.
- `cmd/agent-deck/main.go`
  - update the two global help lines so `codex-hooks` describes notify plus lifecycle management.

No implementation change is allowed in `cmd/agent-deck/hook_handler.go`, `internal/session`, `internal/atomicfile`, watcher/UI code, hook-status/anchor schemas or writers, Codex freshness constants, tmux detection, transition semantics, Git state, or unrelated configuration code.
