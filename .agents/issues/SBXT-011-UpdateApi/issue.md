---
issueId: "SBXT-011-UpdateApi"
humanTitle: "Update API"
issueUrl: ""
createdAt: "2026-06-24T09:06:34Z"
tags: []
---

# Update API

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user‑agent communication and planning)


❯ sbx-runner >> text
Config: .agents\sbx-runner.yaml
Branch (auto-detected): main
Resuming existing sandbox: claude-sbx-templates
Warning: `sbx run claude-sbx-templates` is deprecated; use `sbx run --name claude-sbx-templates` instead.
Starting claude agent in sandbox 'claude-sbx-templates'...
Workspace: W:\DenebLab\sbx-templates


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Update `sbx-runner` to the current `sbx` CLI so it stops emitting the deprecation warning `sbx run <name> is deprecated; use sbx run --name <name> instead`.
- Scope:
  - `shells/sbx-runner.ps1` — resume path at line 240 uses the deprecated positional form `& sbx run $sandboxName`; change to `& sbx run --name $sandboxName`.
  - Audit the rest of the script for other positional `sbx run/stop/exec/list` invocations that may also be deprecated (e.g. the create path `sbx @sbxArgs` builds `run --template <img> <agent>`, the `stop`/`exec` paths pass the sandbox name positionally).
  - Update docs/help text if any documented invocation form changes.
- Constraints:
  - Planning-only at this stage; no code changes until handoff.
  - PowerShell-only runner (Windows host); cannot exercise `sbx` directly inside this Linux sandbox, so verification is by inspection / dry-run.
  - Preserve existing behavior and flags; this is an API-syntax migration, not a redesign.
- Success criteria:
  - `sbx-runner` resumes an existing sandbox with no deprecation warning.
  - All `sbx` sub-invocations use the supported flag-based syntax.
  - `--dry-run` output reflects the updated commands.

# ChangeLog
- 2026-06-24 — Issue created
- 2026-06-24 — Resume: added Agent Summary; traced deprecation warning to shells/sbx-runner.ps1:240 (`sbx run <name>` → `sbx run --name <name>`); flagged full-script audit of positional sbx invocations.
- 2026-06-24 — Resume: re-verified analysis against current sbx-runner.ps1 (resume:240, create:226, stop:187, exec:195, cache paths:228). Confirmed `sbx` CLI is absent in the Linux sandbox, so verification stays by inspection/dry-run. Summary unchanged and accurate.
- 2026-06-24 — Triggered scenario via /issue.
- 2026-06-24 — Implemented: shells/sbx-runner.ps1 resume path changed `sbx run $sandboxName` → `sbx run --name $sandboxName`. Audited all other sbx invocations (stop/exec/list/create) — not deprecated, left unchanged. Created plan.md + state.json.
</content>
