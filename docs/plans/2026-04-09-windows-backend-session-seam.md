# Windows Backend / Session Seam Design

> Generated: 2026-04-09
> Branch: `research/windows-psmux-v1.4.2`
> Context: native Windows port blocked on interactive attach

## Summary

The current codebase cannot deliver native Windows interactive attach by patching one or two call sites.

The blocker is architectural:

- `agent-deck` treats `tmux.Session` as both
  - a **process/session backend**, and
  - a **terminal attach backend**
- Windows support currently only reaches **compile closure** plus **fail-closed guards**
- there is no Windows-native backend that provides the runtime semantics now assumed from `tmux`

The smallest credible next step is to introduce a **minimal session backend seam** and then implement a Windows backend behind it.

This document intentionally focuses on the **smallest seam that can unblock attach**, not on a complete cross-platform rewrite.

## Problem Statement

Windows attach is currently blocked because the codebase assumes a backend with all of these capabilities:

- session existence checks
- session start / restart / kill
- attach to a live interactive terminal
- send input / send control keys
- capture current pane output / history
- environment sync
- activity / status signaling

Today those capabilities are provided by `internal/tmux.Session`.

That makes `tmux.Session` a hidden “god backend”. Windows currently has no equivalent implementation.

## Evidence From Current Code

### Direct attach coupling

- `cmd/agent-deck/session_cmd.go`
  - CLI attach calls `tmuxSession.Attach(...)`
- `internal/ui/home.go`
  - UI attach path uses `attachCmd`, `attachWindowCmd`
  - those call `tmux.Session.Attach(...)` / `AttachWindow(...)`
- `internal/web/terminal_bridge.go`
  - web bridge directly attaches to a tmux session via PTY

### Runtime lifecycle coupling

`internal/session/instance.go` directly depends on `tmuxSession` for:

- `Exists`
- `Start`
- `Kill`
- `RespawnPane`
- `SetEnvironment` / `GetEnvironment`
- `CapturePane` / `CapturePaneFresh` / `CaptureFullHistory`
- `SendKeys` / `SendKeysAndEnter` / `SendEnter`
- `GetStatus`
- `GetCachedWindowActivity`
- `DetectTool`
- acknowledgment state methods

### Status/event coupling

`internal/tmux/controlpipe.go` and `internal/tmux/pipemanager.go` assume tmux control-mode semantics for:

- pane activity
- async output
- low-subprocess status refresh

### Terminal model coupling

`internal/tmux/pty.go` assumes Unix PTY behavior:

- `creack/pty`
- raw PTY attach
- `SIGWINCH`
- Unix process-group semantics

## Design Goal

Create the **smallest interface seam** that:

1. lets Unix continue using the existing tmux backend
2. lets Windows add a backend with equivalent *user-visible* behavior
3. does **not** require rewriting every tmux-specific feature in one phase
4. unblocks native Windows attach as the next milestone

## Non-Goals

This seam should **not** try to solve everything at once.

Not goals for phase 1:

- web terminal bridge parity
- SSH helper parity
- MCP pooling
- conductor service parity
- complete elimination of tmux-specific internals
- full abstraction of every status optimization

## Proposed Seam

Introduce a new internal contract, tentatively:

- `internal/sessionbackend/backend.go`

### Proposed interfaces

```go
type SessionBackend interface {
    Name() string

    // lifecycle
    Exists() bool
    Start(command string) error
    Kill() error
    Restart(command string) error

    // interactive terminal
    Attach(ctx context.Context, detachByte ...byte) error

    // input
    SendKeys(keys string) error
    SendEnter() error
    SendKeysAndEnter(keys string) error
    SendCtrlC() error
    SendCtrlU() error

    // environment + metadata
    SetEnvironment(key, value string) error
    GetEnvironment(key string) (string, error)
    InvalidateEnvCache()

    // output + status support
    CapturePane() (string, error)
    CapturePaneFresh() (string, error)
    CaptureFullHistory() (string, error)
    GetWindowActivity() (int64, error)
    GetCachedWindowActivity() int64
    GetStatus() (string, error)
    DetectTool() string

    // optional UX helpers
    GetWorkDir() string
    EnsureConfigured()
    IsConfigured() bool
}
```

### Why this is the right cut

It covers exactly what current `Instance`, CLI attach, and UI attach need.

It is intentionally **not** a perfect abstraction of every tmux concept.

It does **not** include:

- pane/window enumeration
- full multi-window navigation semantics
- team runtime needs
- web bridge needs

Those can remain on tmux-specific paths for now or get their own later seams.

## Backend Types

### 1. `tmuxBackend` (phase 1: adapter wrapper)

Wrap the existing `*tmux.Session` behind `SessionBackend`.

Goal:

- preserve Unix behavior
- minimize changes
- avoid rewriting tmux internals immediately

This is mostly an adapter layer, not a reimplementation.

### 2. `windowsBackend` (phase 2: real implementation)

Backed by:

- Windows process/session runtime
- ConPTY or equivalent terminal attach path
- Windows-specific session metadata and status primitives

This backend must eventually supply user-visible equivalents for:

- attach
- send input
- capture output/history
- status/activity
- restart

### 3. Optional split: `AttachBackend`

If we discover lifecycle can stay tmux-like while attach needs a separate terminal layer, a second seam may help:

```go
type AttachBackend interface {
    Attach(ctx context.Context, detachByte ...byte) error
}
```

But phase 1 should avoid splitting prematurely. Start with one seam.

## Minimal Migration Plan

### Phase A — Introduce seam without behavior change

1. Add `SessionBackend` interface.
2. Make `Instance` depend on `SessionBackend` rather than directly on `*tmux.Session` in hot paths.
3. Provide a `tmuxBackend` wrapper around existing `*tmux.Session`.
4. Keep stored metadata, title/status behavior, and Unix execution unchanged.

Primary touched files:

- `internal/session/instance.go`
- `internal/session/storage.go`
- `internal/tmux/tmux.go` (adapter construction only, not rewrite)
- new `internal/sessionbackend/*`

### Phase B — Move attach entry points onto the seam

Primary touched files:

- `cmd/agent-deck/session_cmd.go`
- `internal/ui/home.go`

Change:

- attach flows should call `SessionBackend.Attach(...)`
- not `tmux.Session.Attach(...)` directly

This is the first place the seam pays off.

### Phase C — Move status/input/capture hot paths onto the seam

Primary touched files:

- `internal/session/instance.go`

Replace direct tmux calls in:

- start / restart / kill
- send message
- status polling
- history capture
- env sync

### Phase D — Add Windows backend skeleton

Create:

- `internal/sessionbackend/windows_backend.go`

Initial phase may still fail-close some behavior, but only **behind the seam**, not in shared business logic.

### Phase E — Implement native Windows attach backend

This is the real blocker-removal phase.

Possible implementations:

- ConPTY-backed session process
- `psmux` + Windows-native attach/runtime APIs
- hybrid backend if `psmux` can act as lifecycle/session manager while ConPTY handles attach semantics

## What Must Move Behind the Seam First

These are the highest-value consumers to decouple first:

### Tier 1 — Must move first

1. `internal/session/instance.go`
   - session lifecycle
   - input sending
   - status polling
   - output capture
2. `cmd/agent-deck/session_cmd.go`
   - CLI attach
3. `internal/ui/home.go`
   - TUI attach

These three define whether Windows v1 can meet the user-visible operator loop.

### Tier 2 — Can wait

1. `internal/web/terminal_bridge.go`
2. `internal/session/ssh.go`
3. tmux control-mode optimizations that are not required for first Windows parity

## Recommended File Plan

### New files

- `internal/sessionbackend/backend.go`
- `internal/sessionbackend/tmux_backend.go`
- `internal/sessionbackend/windows_backend.go`
- optionally `internal/sessionbackend/factory.go`

### First wave edits

- `internal/session/instance.go`
- `internal/session/storage.go`
- `cmd/agent-deck/session_cmd.go`
- `internal/ui/home.go`

### Later wave edits

- `internal/web/terminal_bridge.go`
- `internal/session/ssh.go`
- `internal/tmux/*` only as needed for wrapper construction

## Backend Factory

Use a factory rather than scattered platform branching:

```go
func NewBackend(cfg BackendConfig) SessionBackend
```

Resolution:

- Unix/macOS/Linux/WSL: `tmuxBackend`
- Windows: `windowsBackend`

This keeps platform logic centralized.

## Why `psmux` Alone Is Not Enough

Even if `psmux` supports many tmux-style commands, the codebase still assumes:

- one specific backend object type (`*tmux.Session`)
- one specific output/status model
- one specific attach model

`psmux` can be the **Windows runtime substrate**, but without the seam there is nowhere to plug it in safely.

## Acceptance Criteria for the Seam Itself

The seam phase is successful when:

1. Unix behavior is unchanged.
2. `Instance` no longer hardcodes `*tmux.Session` across its core operator loop.
3. CLI attach and UI attach call backend interfaces, not tmux directly.
4. A Windows backend can be instantiated without editing higher-level business logic.
5. The attach blocker is reduced from “architecture missing” to “backend implementation missing”.

## Success Criteria for the Next Windows Phase

Once the seam exists, the next execution phase can target:

- native Windows backend start/exists/kill/restart
- native Windows attach
- input sending
- status/capture parity sufficient for Codex flow

## Risks

### Risk 1: Interface is too large

Mitigation:

- keep the first seam focused on the operator loop only

### Risk 2: Interface is too small

Mitigation:

- start with Tier 1 consumers
- add methods only when a concrete Windows backend need appears

### Risk 3: team/web/ssh continue to assume tmux

Mitigation:

- explicitly defer them from phase 1

### Risk 4: `psmux` lifecycle and ConPTY attach end up needing separate backends

Mitigation:

- allow a second seam later (`AttachBackend`) if forced by implementation reality

## Recommended Next Execution Order

1. **Implement `SessionBackend` + `tmuxBackend` adapter**
2. **Move `Instance` hot paths to the seam**
3. **Move CLI/TUI attach entry points to the seam**
4. **Stub in `windowsBackend`**
5. **Implement native Windows attach/runtime**
6. **Resume Windows v1 execution against the new seam**

## Bottom Line

The next phase should not be “keep hacking on `pty_windows.go`”.

It should be:

**create the smallest backend/session seam that lets Windows have a real backend at all.**

Without that seam, native Windows attach remains a structural impossibility rather than an implementation task.
