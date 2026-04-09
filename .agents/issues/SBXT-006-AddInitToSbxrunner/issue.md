---
issueId: "SBXT-006-AddInitToSbxrunner"
humanTitle: "Add --init to sbxrunner"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [sbx-runner, cli, dx]
---

# Add --init to sbxrunner

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user-agent communication and planning)

Add --init to sbxrunner
--init create default config
i prefer --init (linxu default) not -init. Chnage to all args
Add info about --init if config not present



## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Add an `--init` flag to `sbx-runner` that generates a default `sbx-runner.yaml` config file, so users can bootstrap a new project without writing YAML by hand.
- Scope: Modify `shells/sbx-runner.ps1` — add `--init` support (Linux-style double-dash, user preference) that creates `.agents/sbx-runner.yaml` (preferred location) with sensible defaults (`template`, `agent`, `branch: auto`). Should respect `-Config` path if provided. Skip creation if the file already exists (warn instead of overwrite). Additionally, when no config file is found during normal run, the existing error messages should mention `--init` as a way to create one (e.g., "Run 'sbx-runner --init' to create a default config."). Note: PowerShell natively maps `--init` to a `-Init` switch parameter, so both calling conventions work.
- Constraints: Must not break existing parameter sets or behavior. Config search order stays the same. PowerShell 5.1 compatible. User explicitly prefers Linux-style double-dash convention for **all** arguments in help text, documentation, and user-facing messages (e.g., `--init`, `--help`, `--branch`, `--config`, `--template`, `--agent`, `--exec`, `--status`, `--stop`, `--dry-run`). PowerShell natively accepts `--param` and maps it to `-Param`, so no code change is needed for the parameter definitions — only the help output, comments, and error messages need updating.
- Success criteria: Running `sbx-runner --init` in a project without a config creates `.agents/sbx-runner.yaml` with a valid default template. Running it again warns that the file already exists. All help text and error messages use `--double-dash` convention consistently. Existing commands remain functionally unaffected.

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Added Agent Summary after analyzing user edits and existing sbx-runner.ps1
- 2026-04-09 — Updated Agent Summary to reflect user preference for --init (Linux-style double-dash) over -Init
- 2026-04-09 — Updated scope: error messages when config is missing should suggest --init
- 2026-04-09 — Updated constraints: all args in help/docs/errors must use Linux-style --double-dash convention
- 2026-04-09 — Triggered scenario via /issue
