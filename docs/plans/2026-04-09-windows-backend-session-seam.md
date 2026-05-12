# Windows psmux compatibility notes

> Updated: 2026-05-12
> Scope: native Windows `psmux` behavior observed while porting Agent Deck v1.7.69.

## Current conclusion

Native Windows support does not require a separate `WindowsBackend` when the runtime still delegates to `tmux.Session`/`psmux`. A platform-specific backend wrapper that only forwards every method to `tmux.Session` adds branching without changing semantics and should not be treated as a valid compatibility layer.

The useful seam is much smaller: whenever an `Instance` replaces its `tmux.Session`, all cached runtime handles derived from that session must be rebuilt in the same step. In this branch that is handled by `Instance.setTmuxSession`.

## Validated psmux differences

- `psmux` is tmux-compatible enough for the existing `tmux.Session` object, but not identical at process-launch boundaries.
- Native unwrapped Windows Codex launches must run through `cmd.exe`, not through POSIX shell wrapping and not through a PowerShell outer wrapper.
- Native unwrapped Windows Codex restart should recreate the managed session instead of using `respawn-pane`.
- Codex should be launched with `--no-alt-screen` on this path so preview/attach behavior remains stable.
- Some psmux existence checks can be transiently stale immediately after launch/restart. Retry confirmation is acceptable only at the Windows psmux boundary; it should not silently change Unix tmux semantics.

## Launch command shape

For native Windows built-in Codex with the default command:

```text
cmd.exe /d /s /c "<env setup> && codex --no-alt-screen [resume <session-id>]"
```

Do not route this through:

- `bash -c ...`
- `pwsh -EncodedCommand ...` as the outer tool process
- `tmux respawn-pane ...`

Those paths can start the wrong shell shape, lose the intended interactive program identity, or leave Agent Deck attached to stale runtime state.

## Environment handling

The `cmd.exe` path still needs the same configured environment sources as other launches:

- `[shell].env_files`
- `[shell].init_script`
- tool environment files
- tool and conductor inline environment
- `AGENTDECK_INSTANCE_ID`, `AGENTDECK_TITLE`, and `AGENTDECK_TOOL`

Literal `%` and `"` characters in environment values must be preserved. Values that are unsafe to embed directly in `cmd` should be bridged through PowerShell as data, then applied to the `cmd` environment without expanding `%VAR%` expressions.

PowerShell init scripts are valid Windows configuration. The `cmd.exe` Codex launch path may use PowerShell internally to evaluate those scripts, but the final Codex process should still be launched by `cmd.exe`.

## What not to preserve from earlier attempts

- Do not repair persisted `tool = "shell"` rows by guessing from the command at load time. That changes legitimate shell sessions and is not the restart root cause.
- Do not add a Windows backend that only duplicates the tmux backend method-for-method.
- Do not globally mask `StatusError` in the UI based on recent hook or session-id data. Fix the runtime state instead.
- Do not globally treat tmux start errors as success just because a later existence probe passes. Limit that tolerance to the Windows psmux behavior that motivated it.
- Do not change non-Windows dead-pane semantics while working around psmux-specific pane detection.

## Development note

When testing Windows builds locally, use a user-level Go cache such as:

```powershell
$cacheRoot = Join-Path $env:LOCALAPPDATA 'agent-deck\go'
$env:GOCACHE = Join-Path $cacheRoot 'build'
$env:GOMODCACHE = Join-Path $cacheRoot 'mod'
go build -o build\agent-deck.exe ./cmd/agent-deck
```
