---
issueId: "SBXT-010-NewVersionClaudoCode"
humanTitle: "New version claudo code"
issueUrl: ""
createdAt: "2026-04-18T00:00:00Z"
tags: [docker, claude-code, versioning]
---

# New version claudo code

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user‑agent communication and planning)

New version claudo code


This repo provides docker images with claude code.
But version of claud code is part of cloude image- to sow if i want update claud code -i havto update the whole image


Deatiled proposal; 

read: https://andrewlock.net/running-ai-agents-with-customized-templates-in-docker-sandbox/

After resolving thsi issue - update docs; detiled plan how use it 


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Decouple the Claude Code version from the Docker image so it can be updated independently without rebuilding and pushing the entire image from scratch.
- Scope: Refactor each Dockerfile in `src/` to use a multi-stage build where Claude Code installation is isolated in a dedicated final stage. Add local-build tasks to `Taskfile.yml`; `build-push.sh` / `build-push.ps1` unchanged (still used for registry pushes when needed).
- Approach (from reference article): Split the Dockerfile into stages — `deps` stage installs runtimes, `claude` final stage runs the Claude Code install script. To update Claude Code, rebuild only the `claude` stage via `docker build --no-cache-filter claude ...`, keeping all other layers cached.
- Primary use case — local only: build produces a local image tag (e.g. `sbx-claude-dotnet10:local`); `sbx-runner.yaml` points to that local tag. No registry push required for Claude Code updates.
- Constraints: Must not break existing sandbox behavior. `CLAUDE_ENV_FILE` env var must remain intact. Compatible with `sbx run` / `sbx-runner` workflow and all three existing images (dotnet10, dotnet10-node24, golang124-node24).
- Gotcha — proxy env vars: The reference article sets `ENV NO_PROXY=localhost,127.0.0.1,::1,172.17.0.0/16` and `ENV no_proxy=...` in the Dockerfile. These may already be provided by the base image (`docker/sandbox-templates:claude-code`). Build without them first; if `curl` fails in the `claude` stage with a proxy error, add them to the `deps` stage so all stages inherit them.
- Success criteria: `task update-claude:dotnet10` (and equivalents) rebuilds only the Claude Code layer and produces a usable local image. User updates `sbx-runner.yaml` template to the local tag and runs `sbx-runner` as normal.
- Docs: After implementation, update README (or CLAUDE.md) with a step-by-step guide covering: how to build locally, how to update Claude Code only, and how to point `sbx-runner.yaml` at the local image.
- Reference: https://andrewlock.net/running-ai-agents-with-customized-templates-in-docker-sandbox/

# ChangeLog
- 2026-04-18 — Issue created
- 2026-04-18 — Added Agent Summary after user edits describing the core problem
- 2026-04-18 — User added "Detailed proposal" placeholder; awaiting content
- 2026-04-18 — Fetched reference article; updated Agent Summary with multi-stage Dockerfile approach
- 2026-04-18 — Confirmed local-only build as primary use case; no registry push needed for Claude Code updates
- 2026-04-18 — Added docs requirement: step-by-step usage guide after implementation
- 2026-04-18 — Triggered scenario via /issue
- 2026-04-18 — Added proxy env var gotcha to constraints
