---
issueId: "SBXT-012-ProblemWithTemplates"
humanTitle: "Problem with templates"
issueUrl: ""
createdAt: "2026-06-28T00:00:00Z"
tags: []
---

# Problem with templates

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user‑agent communication and planning)

Running `sbx-runner` fails:

```
❯ sbx-runner
Config: .agents\sbx-runner.yaml
Branch (auto-detected): production
ERROR: --branch is no longer supported; use --clone instead
```

## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Make `sbx-runner` work again with the current `sbx` CLI, which removed `--branch` in favor of a boolean `--clone` switch.
- Root cause: `shells/sbx-runner.ps1` line 227 appends `--branch $Branch` to the `sbx run` args. `sbx run` no longer accepts `--branch`; it now exposes `--clone` (a boolean — "run the agent on a private in-container clone of the host Git repository; must be set at sandbox creation time"). There is no branch *name* concept anymore, so resolving `branch: auto` to a git branch name is obsolete.
- Scope: `shells/sbx-runner.ps1` (arg building, config parsing, help text, `--branch` reparse). Docs: `CLAUDE.md`, `shells` header comments, `sbx-runner.yaml` examples, and `--init` template. Possibly the `_ValidateConfig` known-keys list.
- Design decision (RESOLVED 2026-06-28):
  - Replace the `branch:` config key with a boolean `clone: true|false`. Pass `--clone` to `sbx run` only when `clone` is true. Drop branch auto-detection (`branch: auto`) and the "Branch (auto-detected)" output entirely.
  - Default when `clone` is unset/empty: clone OFF — run `sbx run` without `--clone` (agent works on the live mounted workspace, matching plain `sbx run`).
  - `--branch`/`-Branch` runner param and reparse handling: remove. Add a `--clone`/`--no-clone` (or `-Clone` switch) CLI override.
  - Migration: existing configs still containing `branch:` should not hard-fail. `_ValidateConfig` should warn (not error) and ideally print a hint to rename `branch:` → `clone:`. Remove `branch` from the known-keys list and add `clone`.
- Constraints: Cross-platform parity — `shells/sbx-runner.ps1` is the PowerShell entry; confirm no `.sh` twin needs the same change. Avoid breaking existing `.agents/sbx-runner.yaml` files in user repos.
- Success criteria: `sbx-runner` (and `--dry-run`) builds a valid `sbx run` command with no `--branch`; clone behavior is configurable; help text and docs match; existing configs don't error out.

# ChangeLog
- 2026-06-28 — Issue created
- 2026-06-28 — Diagnosed: `sbx-runner.ps1` passes removed `--branch` flag; `sbx run` now uses boolean `--clone`. Added root cause, scope, and design options to Agent Summary.
- 2026-06-28 — Design decided with user: replace `branch:` with boolean `clone:` key; clone OFF by default when unset; warn (not error) on legacy `branch:` key.
- 2026-06-28 — Implemented: `shells/sbx-runner.ps1` now passes `--clone` (driven by `clone:` config + `--clone`/`--no-clone` overrides), drops `--branch`/auto-detect, warns on legacy `branch:` key. Updated docs (`CLAUDE.md`, `README.md`, `shells/README.md`) and `.agents/sbx-runner.yaml`. Verified via `--dry-run`: legacy config warns + no `--branch`; `clone:true`/`--clone` emit `--clone`; `--no-clone` overrides. Status: DONE.
</content>
</invoke>
