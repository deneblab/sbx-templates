# Plan — SBXT-011-UpdateApi

## Goal
Update `sbx-runner` to the current `sbx` CLI so it stops emitting the deprecation warning:
`sbx run <name> is deprecated; use sbx run --name <name> instead`.

## Analysis
The deprecation is specific to the `sbx run` subcommand, which now expects an existing
sandbox name via the `--name` flag instead of a bare positional argument.

Audited every `sbx` invocation in `shells/sbx-runner.ps1`:

| Line | Invocation | Verdict |
|------|------------|---------|
| 177  | `sbx list` | OK — no name argument |
| 187  | `sbx stop $sandboxName` | OK — positional name still supported (cf. `sbx ports <name>`, `sbx secret set <name>` in CLAUDE.md); no `run` deprecation applies |
| 195  | `sbx exec -it $sandboxName bash` | OK — positional name still supported |
| 226  | `sbx run --template $Template $Agent ...` (create) | OK — no name passed; sbx derives the name from agent+folder. Did not emit a warning historically |
| 237  | `sbx list` | OK |
| 240  | `sbx run $sandboxName` (resume) | **DEPRECATED** — bare positional name. Fix → `sbx run --name $sandboxName` |

Only line 240 triggers the observed warning. The fix is surgical.

## Components / Changes
1. `shells/sbx-runner.ps1` (line ~240) — change `& sbx run $sandboxName` to
   `& sbx run --name $sandboxName`.

## Out of scope
- stop/exec/list/create invocations — not deprecated; left unchanged to preserve behavior.
- Help text / docs — no documented invocation form changes.

## Verification
- `sbx` CLI is not available inside this Linux sandbox; verification is by inspection.
- Manual check: `sbx-runner` resuming an existing sandbox should no longer print the
  deprecation warning (run on the Windows host).

## Creation order
1. Edit `shells/sbx-runner.ps1`.
2. Update state.json.
