---
issueId: "SBXT-005-BetterSbxRunner"
humanTitle: "Better sbx-runner"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [sbx-runner, powershell, dx]
---

# Better sbx-runner

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user-agent communication and planning)

Better sbx-runner
Proposal to make sbx-runner more user friendly
Make Prioryties

## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Improve the sbx-runner PowerShell function to be more user-friendly and ergonomic
- Scope: `shells/sbx-runner.ps1` and associated config format (`.agents/sbx-runner.yaml`)
- Constraints: Must remain backward-compatible with existing sbx-runner.yaml configs; must stay as a single dot-sourceable .ps1 file
- Success criteria: Reduced friction for common workflows; clearer error messages; better discoverability of available options

### Current state analysis
The existing `sbx-runner.ps1` (96 lines) provides:
- YAML config lookup (`.agents/sbx-runner.yaml` or `sbx-runner.yaml`)
- Parameters: `-Config`, `-Template`, `-Agent`, `-Branch`, `-Exec`, `-DryRun`, plus pass-through `$ExtraArgs`
- Auto-resume when a sandbox already exists
- Basic dry-run support

### Prioritized improvement areas

**Priority 1 — High (core usability)**
1. **Auto-branch detection** — `branch: auto` in YAML is read but never resolved to the current git branch. This is a broken promise in the config and should work out of the box.
2. **Config validation** — silent failures if YAML keys are misspelled or missing. Users get cryptic errors instead of actionable messages.
3. **Help / discoverability** — no built-in help text or usage examples; users must read source code to learn usage.

**Priority 2 — Medium (sandbox lifecycle)**
4. **Status / list** — no way to see running sandboxes for the current project without manually running `sbx` commands.
5. **Stop / destroy** — no shorthand to stop or remove an existing sandbox (e.g., `sbx-runner -Stop`).

**Priority 3 — Nice to have (polish)**
6. **Colored output** — status messages could use color for clarity (info, warning, error).
7. **Tab completion** — argument completers for `-Template`, `-Agent`, `-Branch`.
8. **Multiple configs** — named profiles support (e.g., `sbx-runner -Profile dev`).

### Rationale
- P1 items fix things that are broken or confusing today — they remove friction for every invocation.
- P2 items extend the tool to cover the full sandbox lifecycle (run → check → stop), reducing context switches to raw `sbx` CLI.
- P3 items are ergonomic polish that add value but aren't blocking daily use.

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Added Agent Summary after analyzing current sbx-runner.ps1; identified 8 potential improvement areas
- 2026-04-09 — Established priorities: P1 (auto-branch, config validation, help), P2 (status/list, stop/destroy), P3 (color, tab completion, profiles)
