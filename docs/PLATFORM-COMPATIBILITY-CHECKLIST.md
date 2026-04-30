# Platform Compatibility Checklist

This checklist captures the non-obvious Windows/Unix compatibility traps in
agent-deck. Use it before rebases, upstream release ports, or broad changes in
`internal/tmux`, `internal/session`, `internal/sessionbackend`, `internal/ui`, and
`internal/web`.

Native Windows currently uses `psmux` as the tmux-compatible substrate. Do not
assume every tmux behavior matches Unix tmux exactly.

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
- If a change touches Codex session detection, verify new Windows sessions do
  not inherit stale rollout IDs from older sessions in the same project. Disk
  scans must be scoped by project and start time, and live-process probes must
  not claim another instance's session ID.
- If a change touches stored status, distinguish conversation state from runtime
  state. A stored `waiting`/`connected` conversation can still have no live tmux
  session.
- If a change touches startup/restart, verify both fresh create and restart
  paths set `RunCommandAsInitialProcess`, `LaunchAs`, `LaunchInUserScope`,
  `SocketName`, and option overrides consistently.
- If a change touches web terminal attach, verify its environment filtering and
  session lookup follow the same rules as TUI/CLI attach.

## Shell And Command Semantics

Command strings in this repo are not portable by default. They are fragments for
a specific final shell.

Native Windows local PowerShell fragments look like:

```powershell
$env:AGENTDECK_INSTANCE_ID='...'; $env:AGENTDECK_TOOL='codex'; codex
```

POSIX fragments look like:

```sh
export AGENTDECK_INSTANCE_ID=...; unset TELEGRAM_STATE_DIR; exec claude
```

Rules:

- Choose env assignment syntax based on the final execution shell, not just the
  host OS.
- Native Windows local initial-process launch should wrap the command as
  `pwsh -NoLogo -EncodedCommand ...`.
- `respawn-pane` is also a launch path. It must use the same shell marker as
  `new-session`; otherwise Windows local restarts can fail while fresh starts
  appear correct.
- The PowerShell shell marker means the command string was generated with
  PowerShell syntax. Set it for native Windows built-in builders that emit
  `$env:...` assignments, including Claude even when Claude uses the send-keys
  launch path. Do not set it for raw shell sessions or generic custom tools
  just because the host is Windows; they may intentionally contain POSIX shell
  fragments.
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

## tmux And psmux Differences

The code should treat psmux as tmux-like, not tmux-identical.

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
- Direct `tmux` commands outside `s.tmuxCmd*` often lose socket isolation and
  can query the wrong server.

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

Use repo-local caches to avoid default Go cache permission issues in sandboxed
Windows environments:

```powershell
$goCacheRoot = Join-Path (Get-Location) '.tmp-go'
$env:GOCACHE = Join-Path $goCacheRoot 'build'
$env:GOMODCACHE = Join-Path $goCacheRoot 'mod'
```

The `.tmp-go/` tree is ignored by git and by repo-wide source lints; do not use
separate top-level `.tmp-gocache/` or `.tmp-gomodcache/` directories.

Recommended targeted checks after Windows/session changes:

```powershell
go test ./internal/tmux -run "TestWindowsAttach|TestStartCommandSpec|TestWindowsDetachKeyName|TestCapture" -count=1
go test ./internal/session -run "TestShouldRunCommandAsInitialProcess|TestBuildCodexCommand|TestBuildClaudeCommand_Windows|TestPrepareCommand_Windows|TestInstance_UpdateCodexSession|TestIssue680|TestS8" -count=1
go test ./internal/ui -run "TestPreviewFetchedMsgUpdatesCacheTimeOnError|TestShouldAttachExistingSession|TestShouldRenderMissingTmuxError|TestEffectiveDisplayStatus" -count=1
.\dev.ps1 build
```

Live smoke checks on native Windows:

- Create a shell session. It should show preview content and attach successfully.
- Attach the shell session, then `Ctrl+Q` should return to the list.
- Create a fresh Codex session. It should not immediately become
  `no tmux session running`.
- Select the Codex session. Preview should show content or a diagnostic, never
  permanent loading.
- Restart a Windows session. It should preserve socket targeting and reconnect
  control pipe/capture.
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
