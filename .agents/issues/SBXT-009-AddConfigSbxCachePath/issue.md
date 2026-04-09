---
issueId: "SBXT-009-AddConfigSbxCachePath"
humanTitle: "Add to config ability to set .sbx-cache"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [docker, config, cache]
---

# Add to config ability to set .sbx-cache

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user-agent communication and planning)

Add to config ability to set .sbx-cache

Example:
/ - project root where .git exists
/.sbx-cache - cache from workspace 

Sbx docs: https://docs.docker.com/ai/sandboxes/ 
in config sbx-runner.yaml
if not  set - not use;
Add to docs


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Allow configuring a local `.sbx-cache` directory in `sbx-runner.yaml` that maps to `/workspace/.sbx-cache` inside the sandbox, so package caches (NuGet, npm, Go modules) persist across sandbox runs.
- Scope: Add an optional `cache` key to `sbx-runner.yaml` (e.g., `cache: .sbx-cache`). Modify `shells/sbx-runner.ps1` to read this key, resolve it relative to the project root, create the directory if needed, and pass it to `sbx run` via the appropriate volume/mount flag. Update `_ValidateConfig` to accept the new key. Do NOT include `cache` in `--init` default config — only used when explicitly set. Update help text, README, and CLAUDE.md. Reference: https://docs.docker.com/ai/sandboxes/
- Constraints: Path should be relative to the project root (where `.git` lives). If `cache` is not set in config, skip volume mounting entirely (no default). Must work with `sbx run` volume mounting — need to check sbx docs for the correct flag.
- Success criteria: Running `sbx-runner` with `cache: .sbx-cache` in config mounts `<project>/.sbx-cache` to `/workspace/.sbx-cache` in the sandbox. Package caches survive sandbox stop/start cycles.

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Added Agent Summary after user described .sbx-cache at project root, configurable in sbx-runner.yaml
- 2026-04-09 — Updated: cache is optional (if not set, not used), added sbx docs reference, add to docs
- 2026-04-09 — Triggered scenario via /issue
