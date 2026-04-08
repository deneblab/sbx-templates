# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`sbx-templates` is a Deneblab repository for Claude Code sandbox Docker templates. It contains:

- **`src/sbx-claude-dotnet10/Dockerfile`** — sandbox image extending `docker/sandbox-templates:claude-code` with .NET SDK 10.0; NuGet packages cached at `/workspace/.sbx-cache/nuget/packages`.
- **`scripts/build/version.sh` / `version.ps1`** — compute semver from `version.yaml` + git commit count.
- **`scripts/build/build-push.sh` / `build-push.ps1`** — build and push the Docker image with version labels.
- **`shells/sbx-runner.ps1`** — PowerShell function (dot-sourced into profile) that reads `.agents/sbx-runner.yaml` and calls `sbx run`.
- **`Taskfile.yml`** — cross-platform task runner (`version`, `build`, `push`).
- **`.agents/`** — issue tracking and agent task system. Project short ID: `SBXT`.

## Running a Sandbox

```powershell
sbx-runner               # reads .agents\sbx-runner.yaml, launches sandbox
sbx-runner -DryRun       # preview without running
sbx-runner -Branch feat  # named branch
```

Requires `shells/sbx-runner.ps1` dot-sourced in `$PROFILE.CurrentUserAllHosts`.

## Local Docker Build (Taskfile)

Requires [Task](https://taskfile.dev). Dispatches to `.sh` (Linux/macOS) or `.ps1` (Windows) automatically.

```bash
task version   # print computed semver
task build     # build image, load into local Docker daemon (no push)
task push      # build and push to docker.io/pkudrel/sbx-claude-dotnet10
```

Direct script usage:
```bash
bash scripts/build/build-push.sh --no-push    # build only (sh)
bash scripts/build/build-push.sh --dry-run    # preview (sh)
.\scripts\build\build-push.ps1 -NoPush        # build only (PowerShell)
.\scripts\build\build-push.ps1 -DryRun        # preview (PowerShell)
```

## Versioning

```bash
scripts/build/version.sh --version-only        # e.g. 0.1.5
pwsh scripts/build/version.ps1 -VersionOnly
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
