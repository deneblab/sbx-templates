---
issueId: "SBXT-008-Naming"
humanTitle: "Naming"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [naming, convention]
---

# Naming

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user-agent communication and planning)

Naming

there is

sbx-claude-dotnet10-node

wth not
sbx-claude-dotnet10-node24
?

Change all. Update docs


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Establish a consistent naming convention for Docker image templates that includes version numbers for all major runtimes, not just the primary one.
- Scope: Rename directories, Dockerfile references, Taskfile entries, Docker Hub image names, README, and CLAUDE.md. Current names vs proposed:
  - `sbx-claude-dotnet10-node` → `sbx-claude-dotnet10-node24`
  - `sbx-claude-golang-node` → `sbx-claude-golang124-node24`
  - `sbx-claude-dotnet10` — stays as-is (single runtime, version already present)
- Constraints: Renaming Docker Hub images is a breaking change for anyone using the old tags. Version format: major version only for Node (`node24`), major+minor for Go (`golang124` = Go 1.24).
- Success criteria: All image names consistently include version numbers for every runtime they bundle. Docs, Taskfile, build scripts, and directory names all match.

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Added Agent Summary: user wants explicit version numbers in all image names (e.g., node24 not just node)
- 2026-04-09 — User confirmed format: node24 (major), golang124 (major.minor = Go 1.24)
- 2026-04-09 — Triggered scenario via /issue
