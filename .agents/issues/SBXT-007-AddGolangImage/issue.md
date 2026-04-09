---
issueId: "SBXT-007-AddGolangImage"
humanTitle: "Add golang image"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [docker, golang, template]
---

# Add golang image

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user-agent communication and planning)

Add golang image similar to sbx-claude-dotnet10-node 
LTS


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Create a new sandbox Docker template with Go (LTS) installed, following the same pattern as `sbx-claude-dotnet10-node`.
- Scope: Create `src/sbx-claude-golang/Dockerfile` extending `docker/sandbox-templates:claude-code`. Install Go LTS via official tarball or PPA. Set up `GOPATH`/`GOMODCACHE` under `/workspace/.sbx-cache/go/` for persistent caching. Add build support in `Taskfile.yml` and `scripts/build/`. Add version labels matching existing convention.
- Constraints: Follow the same Dockerfile structure as `sbx-claude-dotnet10-node` (base image, root install, user switch, env vars, cache dirs, version labels). Use Go LTS release.
- Success criteria: `docker build` succeeds for the new image. `go version` works inside the container. Module cache persists at `/workspace/.sbx-cache/go/`. Build/push scripts and Taskfile support the new image.

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Added Agent Summary after user specified "similar to sbx-claude-dotnet10-node, LTS"
- 2026-04-09 — Triggered scenario via /issue
