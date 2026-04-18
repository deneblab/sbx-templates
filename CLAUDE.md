# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`sbx-templates` is a Deneblab repository for Claude Code sandbox Docker templates. It contains:

- **`src/sbx-claude-dotnet10/Dockerfile`** — sandbox image extending `docker/sandbox-templates:claude-code` with .NET SDK 10.0; NuGet packages cached at `/workspace/.sbx-cache/nuget/packages`.
- **`src/sbx-claude-dotnet10-node24/Dockerfile`** — .NET SDK 10.0 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/nuget/packages` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-golang124-node24/Dockerfile`** — Go 1.24.2 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/go/` and `/workspace/.sbx-cache/npm`.
- **`scripts/version/version.sh` / `version.ps1`** — compute semver from `version.yaml` + git commit count.
- **`scripts/build/build-push.sh` / `build-push.ps1`** — build and push the Docker image with version labels.
- **`shells/sbx-runner.ps1`** — PowerShell function (dot-sourced into profile) that reads `.agents/sbx-runner.yaml` and calls `sbx run`.
- **`Taskfile.yml`** — cross-platform task runner (`version`, `build`, `push`).
- **`.agents/`** — issue tracking and agent task system. Project short ID: `SBXT`.

## Running a Sandbox

```powershell
sbx-runner               # reads .agents\sbx-runner.yaml, launches sandbox
sbx-runner --init        # create default .agents\sbx-runner.yaml
sbx-runner --dry-run     # preview without running
sbx-runner --branch feat # named branch
```

Requires `shells/sbx-runner.ps1` dot-sourced in `$PROFILE.CurrentUserAllHosts`.

### sbx-runner.yaml

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto
cache: .sbx-cache   # optional: mount local cache dir into sandbox
```

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace. If not set, no cache mounting occurs.

## Local Docker Build (Taskfile)

Requires [Task](https://taskfile.dev). Dispatches to `.sh` (Linux/macOS) or `.ps1` (Windows) automatically.

```bash
task version              # print computed semver (default image)
task build                # build default image locally (no push)
task push                 # build and push default image

task build:dotnet10       # build sbx-claude-dotnet10
task build:dotnet10-node24  # build sbx-claude-dotnet10-node24
task build:golang124-node24    # build sbx-claude-golang124-node24
```

Direct script usage:
```bash
bash scripts/build/build-push.sh --no-push    # build only (sh)
bash scripts/build/build-push.sh --dry-run    # preview (sh)
.\scripts\build\build-push.ps1 -NoPush        # build only (PowerShell)
.\scripts\build\build-push.ps1 -DryRun        # preview (PowerShell)
```

## Updating Claude Code Without Rebuilding the Whole Image

Each Dockerfile uses a two-stage build:
- **`deps` stage** — installs runtimes (.NET, Go, Node). Cached; only rebuilt when dependencies change.
- **`claude` stage** — installs Claude Code via the official install script. Rebuilt independently to update Claude Code.

### Step 1 — Build the local image (first time or after dep changes)

```bash
task build:dotnet10          # full build, loads as docker.io/pkudrel/sbx-claude-dotnet10:latest
```

### Step 2 — Update Claude Code only (fast, skips dep layers)

```bash
task update-claude:dotnet10          # dotnet10
task update-claude:dotnet10-node24   # dotnet10 + Node 24
task update-claude:golang124-node24  # Go + Node 24
task update-claude                   # default image (dotnet10)
```

This runs `docker build --no-cache-filter claude ...`, re-running only the `claude` stage while keeping all other layers cached. The result is loaded into the local Docker daemon — no push required.

Direct script usage:
```bash
bash scripts/build/build-push.sh --image sbx-claude-dotnet10 --no-push --update-claude
.\scripts\build\build-push.ps1 -ImageName sbx-claude-dotnet10 -NoPush -UpdateClaude
```

### Step 3 — Point sbx-runner.yaml at the local image

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto
```

The image tag is the same whether built locally or pushed — `sbx-runner` picks it up from the local daemon automatically.

## Versioning

```bash
scripts/version/version.sh --version-only        # e.g. 0.1.5
pwsh scripts/version/version.ps1 -VersionOnly
```

`version.yaml` format:
```yaml
baseVersion: 0.1.0
baseCommitSha: abc1234   # optional — count commits after this SHA
```

## Agent Task System

Issues tracked in `.agents/issues/{ISSUE_ID}/`. Each issue has `issue.md`, `plan.md`, `state.json`. Active implementation plans in `.agents/ralph/IMPLEMENTATION_PLAN.md`.

Read `IMPLEMENTATION_PLAN.md` before picking up agent work to understand task dependencies and status.

## Sandbox Environment

The sandbox runs with `DOTNET_CLI_TELEMETRY_OPTOUT=1`, `DOTNET_NOLOGO=1`, and `NUGET_PACKAGES=/workspace/.sbx-cache/nuget/packages` pre-set. See the project-level CLAUDE.md (inherited from the sandbox harness) for environment persistence rules and shell completion warnings.
