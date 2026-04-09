---
issueId: "SBXT-003-NewTemplateNet10NodejsLts"
humanTitle: "New template: .NET 10 + Node.js (LTS)"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [docker, dotnet, nodejs, template]
---

# New template: .NET 10 + Node.js (LTS)

Create a new sandbox Docker template that includes both .NET SDK 10.0 and Node.js LTS. This extends the existing template pattern to support projects that require both runtimes in a single sandbox environment.

## Detailed Plan

### 1. New Dockerfile (`src/sbx-claude-dotnet10-node/Dockerfile`)
- Base: `docker/sandbox-templates:claude-code`
- Install system deps (same as dotnet10: ca-certificates, curl, git, zsh, jq, unzip, procps, software-properties-common)
- Install .NET SDK 10.0 via `ppa:dotnet/backports`
- Install Node.js LTS (22.x) via NodeSource setup script — lightweight, no nvm overhead, stays in system PATH
- Create cache dirs: NuGet at `/workspace/.sbx-cache/nuget/packages`, npm at `/workspace/.sbx-cache/npm`
- Set env vars: `DOTNET_CLI_TELEMETRY_OPTOUT=1`, `DOTNET_NOLOGO=1`, `NUGET_PACKAGES`, `npm_config_cache`
- Verify: `dotnet --info` and `node --version`
- Apply OCI labels (VERSION, SHORT_SHA, BUILD_DATE build args)

### 2. Build Infrastructure — Parameterize for Multi-Image
- Refactor `scripts/build/build-push.sh` and `build-push.ps1` to accept an image name parameter (default: `sbx-claude-dotnet10` for backward compat)
- Each image dir (`src/<name>/`) contains its own `version.yaml`
- Update `Taskfile.yml` with tasks per image (e.g., `build:dotnet10`, `build:dotnet10-node`) or a parameterized approach
- Image registry path: `docker.io/pkudrel/<image-name>`

### 3. Verification
- Local build via `task build:dotnet10-node` (or equivalent)
- Container smoke test: `dotnet --info`, `node --version`, `npm --version`
- Push to Docker Hub

## Agent Summary
*(updated by agent on resume — user text above remains untouched)*
- Goal: Create a new sandbox Docker template `sbx-claude-dotnet10-node` that ships both .NET 10 SDK and Node.js LTS
- Scope: New Dockerfile + parameterize build infra for multi-image support
- Constraints: Follow existing patterns; Node.js LTS 22.x via NodeSource; base image `docker/sandbox-templates:claude-code`
- Success criteria: Buildable, pushable image with both `dotnet --info` and `node --version` working
- Decisions made:
  - Node.js installation: **NodeSource PPA** (simple, system-level, no nvm overhead)
  - Build infra: **Parameterize** scripts rather than duplicate (avoids divergence)
  - Versioning: **Per-image `version.yaml`** in each `src/<name>/` directory

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Agent resume: refined scope after codebase analysis; identified build-infra decisions needed
- 2026-04-09 — Agent resume: added detailed implementation plan per user request; resolved open questions (NodeSource, parameterize scripts, per-image versioning)
- 2026-04-09 — Triggered scenario via /issue
