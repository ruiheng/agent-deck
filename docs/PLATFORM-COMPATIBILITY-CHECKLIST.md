# Platform Compatibility Checklist

This document is intentionally more than a checklist. It is the portable memory
of the Windows/psmux port: if the Windows branch is lost or cannot be merged,
this file should be enough for another engineer or agent to re-implement native
Windows support on top of a newer upstream tree without repeating the same
failed experiments.

Use it before rebases, upstream release ports, or broad changes in
`internal/tmux`, `internal/session`, `internal/sessionbackend`, `internal/ui`,
`internal/web`, test infrastructure, or any code that launches subprocesses.

Native Windows currently uses `psmux` as the tmux-compatible substrate. Do not
assume every tmux behavior matches Unix tmux exactly.

## Reconstruction Goal

The goal is feature-equivalent native Windows behavior, not line-for-line
recreation of this branch.

Port in layers:

- Establish the Windows substrate first: every tmux subprocess must go through a
  socket-aware wrapper, strip inherited `PSMUX_SESSION`, and preserve real
  console handles for interactive attach.
- Then fix command launch semantics: command strings must carry their intended
  final shell (`PowerShell`, `cmd.exe`, or POSIX) instead of deriving syntax from
  `runtime.GOOS`.
- Then add degraded-but-usable status and preview: capture failures must be
  bounded and recoverable, and UI state must show diagnostics rather than
  permanent loading.
- Then restore agent-specific behavior: Codex/Claude/Gemini session IDs,
  restart/resume, `CODEX_HOME` handling, and built-in Codex `--no-alt-screen`.
- Then make tests portable: isolate HOME/profile state correctly, translate
  Unix fixtures into intent-based fixtures, and skip psmux load tests that only
  prove Unix tmux stress behavior.
- Finally verify web/static behavior and filesystem behavior. These look
  unrelated to tmux, but they are common Windows breakpoints.

Treat a passing Linux/macOS test suite as necessary but not sufficient. Most
regressions below passed Unix tests while failing only on native Windows.

## Merge Checklist

- If a change touches session launch, verify the final command is interpreted by
  the shell syntax it was built for. Native Windows local launches that contain
  `$env:...` assignments must run through PowerShell, not `bash -c`.
- If a change touches Windows non-local launches, verify SSH, sandbox, and
  wrapped commands still use POSIX syntax where the final execution shell is
  POSIX. `runtime.GOOS == "windows"` is not enough to choose PowerShell syntax.
- If a change adds a new launch wrapper, carry an explicit "command shell"
  signal through to `tmux.Session`. The tmux layer cannot infer whether a string
  is PowerShell or POSIX from the host OS.
- If a change touches tmux command construction, verify per-session socket
  selection is preserved. Attach, capture, send-keys, environment, status, and
  kill paths must route through `s.tmuxCmd*` or `tmux.Exec*` with the stored
  `TmuxSocketName`.
- If a change touches Windows tmux subprocesses, verify inherited psmux leader
  state is stripped. Do not let `PSMUX_SESSION` leak into agent-deck-managed tmux
  commands.
- If a change reads tmux environment values, parse the requested `KEY=value`
  line explicitly. psmux may append additional environment lines to
  `show-environment` output, so treating the whole output as one value can
  corrupt stored session metadata or cleanup filters.
- If a change adds tmux/psmux integration tests, do not reuse Unix tmux test
  isolation blindly on native Windows. psmux is tied to the user's Windows
  console/profile environment; tests should strip `TMUX*` and `PSMUX_SESSION`
  from tmux subprocesses, but should not rewrite `USERPROFILE` just to mimic a
  Unix `$HOME`-isolated socket server.
- If a live-pane integration test needs a command fixture, express the fixture's
  intent rather than hard-coding Unix tools. `/tmp`, `sleep`, `cat`, `.sh`
  scripts, and raw stdin echo loops are Unix fixtures, not cross-platform
  behavior. Native Windows tests should use PowerShell/cmd equivalents or send
  executable shell commands to an interactive pane and assert captured output.
- If a test launches a generated Windows executable through tmux/psmux, the
  binary path must end in `.exe`. A suffix-less file that is executable on Unix
  can trigger Windows file-association UI instead of process execution.
- If a test writes config/TOML containing native Windows paths, quote those
  paths with a TOML-safe encoder such as `%q`. Raw double-quoted strings like
  `"C:\Users\..."` are invalid TOML escapes, not product behavior.
- If a smoke test intentionally runs the Agent Deck TUI inside tmux/psmux, set
  the explicit nested-TUI test override (`AGENT_DECK_ALLOW_OUTER_TMUX=1`).
  Otherwise the product guard correctly exits with the "run outside tmux"
  message, and the test will misdiagnose that as a render/capture failure.
- Do not treat passive update/status text as a modal prompt in TUI tests. Sending
  `n` for a non-modal `Update available` label is just normal keyboard input and
  can open the New Session dialog.
- If a change touches attach, verify Windows attach uses real console handles.
  Do not capture stdout/stderr for interactive `tmux attach-session` on Windows;
  psmux can fail with `incorrect function` when Go pipes replace the console.
- If a change touches attach success handling, do not treat every Windows exit
  code 1 as success. psmux may return 1 for normal detach, but fast exit-1
  attach failures must remain failures.
- If a change touches preview or status capture, verify `capture-pane -S ...`
  has a fallback. psmux may fail full-history capture while ordinary pane
  capture or control-pipe capture still works.
- If a change touches preview fetching, never leave the UI in an indefinite
  loading state. Cache a diagnostic for failed fetches and make subprocess
  capture calls bounded by timeout.
- If a change touches background watchers or renewal loops, distinguish real
  health failures from normal shutdown. A context-canceled renewal during
  teardown must not poison later health state.
- If a change touches Codex session detection, verify new Windows sessions do
  not inherit stale rollout IDs from older sessions in the same project. Disk
  scans must be scoped by project and start time, and live-process probes must
  not claim another instance's session ID.
- If a change touches native Windows built-in Codex launch or restart, verify
  the command still disables Codex's alternate screen with `--no-alt-screen`.
  psmux capture/preview can lose track of Codex when it switches buffers,
  leaving only the parent PowerShell prompt visible.
- If a change touches native Windows built-in Codex restart, verify the live
  tmux pane does not become a shell after restart. On psmux, `respawn-pane` with
  `pwsh -EncodedCommand` has been observed to report success while leaving the
  pane at a PowerShell prompt. The supported path for local unwrapped built-in
  Codex is kill/recreate + initial-process launch through `cmd.exe /d /s /c`.
- If a change touches stored status, distinguish conversation state from runtime
  state. A stored `waiting`/`connected` conversation can still have no live tmux
  session.
- If a change touches startup/restart, verify both fresh create and restart
  paths set `RunCommandAsInitialProcess`, `LaunchAs`, `LaunchInUserScope`,
  `SocketName`, and option overrides consistently.
- If a change touches web terminal attach, verify its environment filtering and
  session lookup follow the same rules as TUI/CLI attach.
- If a change touches web terminal attach on native Windows, do not assume a
  Unix PTY library can attach to psmux. `creack/pty` returns unsupported on
  Windows; use a psmux-compatible fallback such as bounded `capture-pane`
  polling plus `send-keys` input forwarding.
- If a change touches web static assets, verify browser module scripts are served
  with JavaScript MIME types. Do not rely on host MIME registration for `.mjs`;
  Windows machines can map it to `text/plain`, and browsers will reject
  `<script type="module">` imports before the app boots.

## Shell And Command Semantics

Command strings in this repo are not portable by default. They are fragments for
a specific final shell.

The most important design rule is to separate "where the program is running"
from "which shell will interpret this exact string." Native Windows can still
launch POSIX commands through SSH, sandbox wrappers, or user wrappers. Native
Windows can also need `cmd.exe` instead of PowerShell for specific psmux
compatibility paths.

Native Windows local PowerShell fragments look like:

```powershell
$env:AGENTDECK_INSTANCE_ID='...'; $env:AGENTDECK_TOOL='claude'; claude --session-id <id>
```

POSIX fragments look like:

```sh
export AGENTDECK_INSTANCE_ID=...; unset TELEGRAM_STATE_DIR; exec claude
```

Native Windows local cmd fragments used for unwrapped built-in Codex look like:

```cmd
cmd.exe /d /s /c "set ""AGENTDECK_INSTANCE_ID=..."" && set ""AGENTDECK_TOOL=codex"" && codex --no-alt-screen resume <id>"
```

Rules:

- Choose env assignment syntax based on the final execution shell, not just the
  host OS.
- Native Windows local PowerShell initial-process launch should wrap the command
  as `pwsh -NoLogo -EncodedCommand ...`.
- Native Windows local, unwrapped, built-in Codex is a psmux compatibility
  exception: build a cmd-compatible command and launch it as the pane's initial
  process. Do not route this path through PowerShell or POSIX shell wrappers.
- `respawn-pane` is also a launch path. It must use the same shell marker as
  `new-session`; otherwise Windows local restarts can fail while fresh starts
  appear correct.
- Do not use `respawn-pane` for native Windows local, unwrapped, built-in Codex.
  The observed failure mode is especially misleading: `respawn-pane` succeeds,
  but the pane remains at `PS ...>` and agent-deck preview/attach sees a shell.
- The PowerShell shell marker means the command string was generated with
  PowerShell syntax. Set it for native Windows built-in builders that emit
  `$env:...` assignments, including Claude even when Claude uses the send-keys
  launch path. Do not set it for built-in Codex when it is using the cmd
  compatibility path, raw shell sessions, or generic custom tools just because
  the host is Windows; they may intentionally contain POSIX shell fragments.
- The cmd shell marker means the command string was generated for `cmd.exe`.
  Carry this marker through `tmux.Session` just like the PowerShell marker so
  `new-session`, send-keys fallback, and respawn wrapping do not reinterpret it.
- Windows SSH, sandbox, and wrapper launches may still be initial-process
  launches, but they must not be PowerShell encoded if the command string is
  POSIX. This includes commands containing POSIX single-quote escaping such as
  `'\''`.
- Wrapped built-in tools on Windows must generate POSIX env/source prefixes,
  because wrapper substitution routes the final command through the POSIX/bash
  wrapper path.
- POSIX wrappers, SSH remote commands, and sandbox commands must keep POSIX
  `export`, `unset`, `source`, `exec`, and `bash -c` semantics.
- Do not use `unset ...` in PowerShell. Use `Remove-Item Env:NAME -ErrorAction SilentlyContinue`.
- Do not use `Remove-Item Env:...` in POSIX paths.
- A wrapper template can move the command across shell boundaries. Re-check shell
  semantics after wrapper substitution, not before.

Failure signatures:

- Seeing `bash -c '$env:FOO=...'` means PowerShell syntax leaked into a POSIX
  shell.
- Seeing `unset FOO` in a Windows local PowerShell launch means POSIX cleanup
  syntax leaked into PowerShell.
- Seeing a pane at `PS <path>>` after a Codex restart usually means the restart
  shell succeeded but did not exec Codex.

## tmux And psmux Differences

The code should treat psmux as tmux-like, not tmux-identical.

Implementation rule: centralize all tmux process creation. A scattered
`exec.Command("tmux", ...)` port will regress socket targeting, psmux leader
environment cleanup, or Windows console behavior. Use package-level wrappers for
global probes and per-session wrappers for session-scoped operations.

Important differences observed on Windows:

- `attach-session` must inherit stdin/stdout/stderr directly from the console.
  Capturing output through Go pipes can produce `incorrect function`.
- `attach-session` can return exit code 1 for a normal interactive detach.
  Duration alone is not enough: quick successful attach+detach is valid when
  the managed session is still visible after attach exits.
- `has-session` and cached existence checks can be false negatives around attach
  and startup. Attach itself is the authoritative operation on Windows.
- UI attach/restart decisions must not use a Windows `Exists()` false negative
  to restart an ordinary live session. For non-stopped sessions, prefer attach
  unless the runtime is confirmed dead by stronger evidence.
- Full-history capture with `capture-pane -S -2000` can exit 1 even when the
  session is alive and ordinary capture/control-pipe capture works.
- A psmux-hosted leader pane can set `PSMUX_SESSION`; inherited leader context
  can confuse nested tmux commands. Strip it from agent-deck subprocesses.
- psmux `show-environment` output can contain more than the requested variable,
  including psmux-internal keys. Environment lookup and cleanup code must select
  the exact requested key and ignore unrelated lines.
- psmux integration tests should use Windows console semantics as the oracle.
  Unix raw-stdin byte reads are not equivalent: ordinary Windows console input
  is cooked/event-based, while Agent Deck observes keys through Bubble Tea's
  `tea.KeyMsg` layer. For native Windows input tests, drive `tmux send-keys`
  into a psmux pane and assert the resulting Bubble Tea key event rather than
  expecting immediate `os.Stdin.ReadByte` escape bytes.
- psmux is much easier to destabilize with high-concurrency live-session stress
  tests than Unix tmux. Keep native Windows integration tests focused on
  functional equivalence; do not run Unix tmux load/cleanup stress suites in the
  same package order if they leave later send/capture tests flaky.
- psmux does not reliably support every Unix tmux control/status feature used by
  historical tests, including control mode, `status-left`, terminal-title hooks,
  and some full-history capture forms. Prefer pure parser/builder tests or
  functional psmux equivalents over asserting Unix option plumbing directly.
- Direct `tmux` commands outside `s.tmuxCmd*` often lose socket isolation and
  can query the wrong server.
- Native Windows built-in Codex should be launched and resumed with
  `--no-alt-screen`. Codex's alternate screen can interact badly with psmux
  capture/preview after `Ctrl+C` or restart: the live pane may show the
  PowerShell parent prompt even though the stored session is still a Codex
  session. Agent-deck's auto wrapper for built-in Codex extra args
  (`{command} ...`) should preserve this flag. Do not apply it blindly to SSH,
  sandbox, tool-config wrappers, or custom Codex-compatible commands; their
  final shell and CLI flags are owned by that command path.
- Native Windows local, unwrapped, built-in Codex restart should not use
  `respawn-pane`. Preserve `CodexSessionID`, recreate the tmux/psmux session,
  rebuild the session backend, and launch `codex --no-alt-screen resume <id>` as
  the new pane's initial process through `cmd.exe /d /s /c`.
- `respawn-pane` success is not proof that the intended agent process is alive.
  For Codex on psmux, verify by pane content or attach behavior: seeing
  `PS <path>>` means restart produced a shell, regardless of stored
  `CodexSessionID`.

Do not chase these by adding arbitrary sleeps. Prefer a stronger readiness
signal, bounded retry around the operation that actually matters, or a Windows
skip when the test is only Unix tmux stress coverage.

## Socket Isolation Rules

Socket isolation is per session and immutable after creation.

Checklist:

- New sessions must store the socket name in SQLite.
- Every later operation on that session must target the stored socket name.
- Do not rebuild socket selection from current config for existing sessions.
- Do not call raw `exec.Command("tmux", ...)` for a session-scoped operation.
- Attach paths are session-scoped operations and must preserve `-L <socket>`.
- Web bridge, UI, CLI, restart, preview, environment, status, and kill paths
  must all agree on the same socket.

## Filesystem Portability

Do not assume Unix symlink privileges are available.

Checklist:

- If a runtime feature only needs equivalent file visibility, prefer a
  symlink-first, copy-fallback mirror. This preserves the common Unix fast path
  without making Windows developer-mode or admin privileges a product
  requirement.
- A copy fallback must behave like a mirror, not a one-time snapshot. On every
  reuse, keep live symlinks, refresh copied files/directories that still exist
  in the source, and prune copied entries that were deleted or renamed from the
  source. Otherwise a reused scratch/profile dir can load stale configuration
  that Unix symlinks would not expose.
- If a test specifically validates symlink identity, skip on platforms or
  configurations that cannot create symlinks. If the behavior under test is
  file visibility, test the fallback result instead of testing the mechanism.

Common case: worker scratch `CLAUDE_CONFIG_DIR` mirrors the user's Claude
profile while mutating `settings.json`. If symlinks fail on Windows, the copied
mirror must still track added, changed, and removed top-level profile entries
across restarts.

## Test Environment And Home Isolation

Windows has two different "home" problems. Do not solve them with one helper.

Checklist:

- For pure config/unit tests, isolate the home directory consistently:
  `HOME`, `USERPROFILE`, `HOMEDRIVE`, and `HOMEPATH` may all matter because
  Go's `os.UserHomeDir` and application code can consult different variables on
  Windows. Clear any process-wide config cache after changing them.
- For live psmux/tmux integration tests, do not blindly point `USERPROFILE` at a
  temporary Unix-style home. psmux is tied to the user's Windows console/profile
  environment, and over-isolating it can create failures that users will never
  see. Prefer isolating Agent Deck's own profile/data while preserving the
  Windows console environment psmux needs.
- Always strip inherited `TMUX*` and `PSMUX_SESSION` from tmux subprocesses in
  tests. A test launched from inside a psmux pane can otherwise target or inherit
  the wrong leader session.
- Generated Windows binaries launched through tmux/psmux must end in `.exe`.
  This applies to smoke-test binaries and helper CLIs, not just production
  builds.
- TUI smoke tests intentionally nesting Agent Deck inside psmux must opt in with
  `AGENT_DECK_ALLOW_OUTER_TMUX=1`; otherwise the production "run outside tmux"
  guard is doing the right thing.
- When a test only validates Unix tmux load behavior, skip or redesign it for
  native Windows. psmux can become unstable under many concurrent live sessions,
  and that instability can leak into later tests in the same package.

## Attach And Detach Rules

User-facing Windows behavior should match agent-deck expectations, not just tmux
defaults.

Checklist:

- `Ctrl+Q` should detach back to the agent-deck list when possible.
- `Ctrl+b d` may still work, but it is not the intended primary habit.
- On Windows, bind a tmux-level fallback detach key for cases where terminal flow
  control intercepts input.
- Do not add output capture to interactive attach unless it is proven to preserve
  console handles on psmux.
- If failure detection needs output, prefer separate non-interactive probes or
  bounded diagnostics; do not compromise the interactive attach handle path.

## Preview And Status Capture

Preview and status are allowed to be degraded, not blocking.

Checklist:

- Use control pipe capture when available.
- Subprocess capture must have a timeout.
- Full-history preview must fall back to ordinary capture if history capture
  fails.
- UI preview fetch errors must become visible diagnostics, not permanent
  `Loading preview...`.
- Background status loops must tolerate capture failures without marking every
  session dead solely because one capture form failed.
- Slow capture should be logged with enough context: session name, socket name,
  command form, and error.

## Web Static Assets

The web UI must work from embedded assets on native Windows without relying on
the user's registry or browser leniency.

Checklist:

- Register `.mjs` explicitly as `application/javascript; charset=utf-8` in the
  Go web process before serving embedded files. `http.FileServer` uses Go's MIME
  lookup, which can consult host mappings; on some Windows machines `.mjs`
  resolves to `text/plain`.
- Add a regression test that requests every vendored `.mjs` module used by the
  import map (`preact`, `htm`, signals, xterm, addons) and asserts the response
  `Content-Type` starts with `application/javascript`.
- Treat this as a boot-blocking compatibility issue, not a cosmetic warning. A
  browser enforcing strict module MIME checks will refuse to load the first bad
  module, leaving the web UI blank even though `/` returns `200`.
- For PWA/mobile metadata, include the standards-track
  `mobile-web-app-capable` meta tag alongside any Apple-specific legacy tag.

Failure signature:

```text
Failed to load module script: Expected a JavaScript-or-Wasm module script but
the server responded with a MIME type of "text/plain".
```

If that appears for `htm.mjs`, `preact.mjs`, xterm, or another import-map
module, fix the server MIME mapping first. Rebuilding frontend assets will not
help.

## Web Terminal Bridge

The browser terminal has a different constraint from CLI/TUI attach: the server
process must bridge bytes over WebSocket. On Unix this can be implemented by
starting `tmux attach-session` under a PTY. Native Windows psmux cannot use that
same path.

Checklist:

- Do not call `pty.Start(tmux attach-session ...)` on native Windows.
  `github.com/creack/pty` reports `unsupported`, which surfaces in the browser
  as `TERMINAL_ATTACH_FAILED`.
- Use the session's stored socket name for every bridge operation:
  `has-session`, `capture-pane`, `resize-window`, and `send-keys` must target
  the same server as TUI/CLI operations.
- Strip `TMUX*` and `PSMUX_SESSION` from bridge subprocesses just like other
  tmux subprocesses. A web server launched from inside psmux can otherwise
  inherit the wrong leader context.
- A Windows fallback can be degraded but must be usable: poll
  `capture-pane -p -e` on a bounded interval, send changed content to xterm.js,
  and forward browser input with `send-keys`. Map common control bytes
  explicitly (`Enter`, `Backspace`, `Ctrl+C`, `Ctrl+D`) instead of sending them
  as plain text.
- When polling, account for full-screen alternate-screen applications. If tmux
  reports alternate screen active, capture with `capture-pane -a` first; plain
  `capture-pane -p -e` can show stale main-screen history while Codex, vim, or
  another TUI is actually alive on the alternate screen.
- The polling fallback is snapshot-based, so pane width and browser xterm width
  must stay aligned. If the browser panel is narrower than the tmux pane, long
  lines wrap differently in xterm.js and cursor placement appears wrong even
  when psmux reports the correct `#{cursor_x},#{cursor_y}`. Send resize events
  after attach and prefer testing with a wide enough terminal panel. This is
  different from the Unix PTY attach path, where the web client participates in
  normal tmux client-size negotiation and tmux effectively renders to the
  smallest attached client; the polling fallback must actively resize the pane
  to the browser's xterm dimensions.
- Keep Unix and Windows bridge tests separate. Unix PTY integration tests should
  remain `!windows`; Windows tests should validate command construction,
  environment filtering, MIME/static boot, and the polling/send fallback.

Failure signature:

```text
Connecting to terminal...

[error:TERMINAL_ATTACH_FAILED] failed to attach terminal bridge
```

If logs include `start tmux pty: unsupported`, the fix is not session lookup or
auth. Replace the PTY attach path for native Windows.

## Session IDs And Runtime State

Agent conversation IDs and live runtime sessions are separate facts.

Checklist:

- A stored Claude/Codex/Gemini session ID does not prove a tmux/psmux runtime is
  alive.
- A live tmux/psmux session does not prove the agent conversation ID is current.
- Codex disk scans must not assign a stale rollout file to a newly created
  session just because it is the most recently modified file in the project.
- Codex rollout-file validation is only host-authoritative for local,
  unwrapped built-in `codex` execution. SSH, sandbox, wrapper, and custom
  Codex-compatible command launches may store rollout JSONL under a
  remote/container/wrapper-owned `CODEX_HOME`, so a missing host file must not
  clear the stored resume ID.
- For local Codex, rollout-file validation must use the session launch
  environment's `CODEX_HOME` when it is configured through env_files or
  init_script or tool/conductor inline env. If init_script is opaque or
  any sourced value for `CODEX_HOME` is dynamic or cannot be fully resolved
  before launch, including references like `$BASE/.codex` or
  `$env:LOCALAPPDATA`, do not clear the stored resume ID based on the
  agent-deck process environment.
- Do not use glob patterns over user-controlled Codex paths. `CODEX_HOME` can
  legally contain characters such as `[` and `]`; walk the sessions tree and
  match rollout file names instead.
- When multiple instances share a project path, session-ID exclusion matters.
- Restart should only resume a stored ID when that is the intended behavior for
  that tool and state.
- For debugging, inspect both SQLite state and runtime logs; either alone can be
  misleading.

## Windows Validation Commands

Use a user-level Go cache to avoid workspace churn and sandbox permission
surprises in Windows environments:

```powershell
$goCacheRoot = Join-Path $env:LOCALAPPDATA 'agent-deck\go'
$env:GOCACHE = Join-Path $goCacheRoot 'build'
$env:GOMODCACHE = Join-Path $goCacheRoot 'mod'
```

Do not introduce repo-local `.gocache`, `.tmp-gocache`, or `.tmp-gomodcache`
directories unless a specific task explicitly requires isolated caches.

Recommended targeted checks after Windows/session changes:

```powershell
go test ./internal/tmux -run "TestWindowsAttach|TestStartCommandSpec|TestWindowsDetachKeyName|TestCapture" -count=1
go test ./internal/session -run "TestShouldRunCommandAsInitialProcess|TestBuildCodexCommand|TestBuildClaudeCommand_Windows|TestPrepareCommand_Windows|TestInstance_UpdateCodexSession|TestIssue680|TestS8" -count=1
go test ./internal/tmux -run "TestStartCommandSpec_WindowsCmdWhenMarked|TestSessionWrapRespawnCommand_WindowsCmdWhenMarked|TestSessionWrapRespawnCommand_WindowsPowerShellWhenMarked" -count=1
go test ./internal/session -run "TestRecreateTmuxSession|TestShouldUsePowerShellCommandShell|TestBuildCodexCommand_WindowsNativeCodexUsesCmd" -count=1
go test ./internal/session -run "TestEnsureWorkerScratchConfigDir|TestMirrorProfileEntries|TestBuildClaudeCommand_UsesWorkerScratchConfigDir" -count=1
go test ./internal/ui -run "TestPreviewFetchedMsgUpdatesCacheTimeOnError|TestShouldAttachExistingSession|TestShouldRenderMissingTmuxError|TestEffectiveDisplayStatus" -count=1
go test ./internal/web -run "TestVendorModuleFilesUseJavaScriptMimeType|TestVendorFilesServed|TestIndex" -count=1
.\dev.ps1 build
```

Live smoke checks on native Windows:

- Create a shell session. It should show preview content and attach successfully.
- Attach the shell session, then `Ctrl+Q` should return to the list.
- Create a fresh Codex session. It should not immediately become
  `no tmux session running`.
- Select the Codex session. Preview should show content or a diagnostic, never
  permanent loading.
- In a native Windows Codex session, press `Ctrl+C` so the pane returns to
  PowerShell, then restart/resume the session. The restored command should use
  `cmd.exe /d /s /c ... codex --no-alt-screen resume <id>` and should return to
  the Codex transcript, not stay as a shell.
- Restart a Windows session. It should preserve socket targeting and reconnect
  control pipe/capture.
- Start `agent-deck web`, open the browser console, and reload `/`. The app must
  boot without `.mjs` strict MIME errors. Directly requesting
  `/static/vendor/htm.mjs` should return `application/javascript`, not
  `text/plain`.
- If using the installed binary, ensure `C:\Users\<user>\.local\bin\agent-deck.exe`
  was actually overwritten; a running process can keep the old binary locked.

## Review Questions For Risky Patches

Ask these explicitly in review:

- Which shell interprets this exact command string on Windows local, Windows SSH,
  sandbox, wrapper, WSL, Linux, and macOS?
- Does this tmux operation target the session's stored socket, or the current
  default server?
- Does this subprocess inherit `PSMUX_SESSION`, `TMUX`, or other leader-session
  state that can change behavior?
- If this operation fails on psmux but the session is still alive, what fallback
  keeps the UI usable?
- If this goroutine blocks, what timeout releases the UI state?
- Does this status value describe the agent conversation, the tmux runtime, or a
  cached UI projection?
- Does the test prove behavior on the final shell/backend, or only test string
  construction on the current OS?
