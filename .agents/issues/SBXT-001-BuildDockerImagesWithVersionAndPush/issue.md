---
issueId: "SBXT-001-BuildDockerImagesWithVersionAndPush"
humanTitle: "Build Docker Images With Version And Push"
issueUrl: ""
createdAt: "2026-04-08T00:00:00Z"
tags: [docker, ci, versioning]
---

# Build Docker Images With Version And Push

Build the sandbox Docker images using the semantic version computed by the versioning scripts, tag them accordingly, and push to a container registry.

Deatailed plan.

version (same for sh version)
❯ .\scripts\build\version.ps1
version=0.1.1
major=0
minor=1
patch=1
tag=v0.1.1
sha=f4096cfd70162b9acf58b4993530acb8f578ae0d
short_sha=f4096cf
branch=main
build_date_utc=2026-04-08T09:11:40Z


Study: https://docs.docker.com/ai/sandboxes/agents/custom-environments/


My docer aoucnt: https://app.docker.com/accounts/pkudrel

## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Automate building of `sbx-claude-dotnet10` Docker image tagged with the computed semantic version (from `scripts/build/version.sh` / `version.ps1`) and push it to Docker Hub (OCI-compatible registry) so it can be referenced as a custom sandbox template.
- Scope: CI/CD pipeline (GitHub Actions) or local build script that: (1) invokes the versioning script and parses `tag=` / `version=` / `short_sha=` from key=value output, (2) runs `docker build --push -t docker.io/pkudrel/sbx-claude-dotnet10:{tag}`, (3) also tags as `latest`. Single image `src/sbx-claude-dotnet10/Dockerfile` (already extends `docker/sandbox-templates:claude-code`). When referencing the image in sandbox configs, must use explicit `docker.io/` domain prefix.
- Constraints: Version from `version.yaml` + git commit count (output format confirmed: `tag=v0.1.1`, `short_sha=f4096cf`). Registry = Docker Hub (OCI). Image must extend `docker/sandbox-templates:claude-code` (already satisfied). Dockerfile must keep `USER root` for apt installs then switch back to `USER agent`. Registry credentials injected via secrets (not hardcoded).
- Success criteria: Image `docker.io/pkudrel/sbx-claude-dotnet10:v0.1.1` and `:latest` visible in Docker Hub after pipeline run; image usable as a custom sandbox template referenced via its full `docker.io/` path; build fails if versioning script errors; `version` and `short_sha` available as image labels.

# ChangeLog
- 2026-04-08 — Issue created
- 2026-04-08 — Added Agent Summary after user edit ("Deatailed plan." note)
- 2026-04-08 — Refined Agent Summary with confirmed version script output format (tag=v0.1.1, key=value pairs, both .sh and .ps1)
- 2026-04-08 — Updated scope/constraints with Docker sandbox docs findings: registry=Docker Hub, must use docker.io/ prefix, --push flag, base image requirement already satisfied
- 2026-04-08 — Corrected Docker Hub namespace from deneblab to pkudrel (confirmed from user's Docker account)
- 2026-04-08 — Triggered scenario via /issue
