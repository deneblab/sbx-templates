# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository (`sbx-templates`) is a Deneblab sandbox template for Claude Code .NET 10 development environments. It contains:

- **`src/sbx-claude-dotnet10/Dockerfile`** — Docker sandbox image based on `docker/sandbox-templates:claude-code`, adds .NET SDK 10.0, and caches NuGet packages under `/workspace/.sbx-cache/nuget/packages`.
- **`scripts/build/version.sh` / `version.ps1`** — Semantic versioning scripts that compute a version from a `version.yaml` file (`baseVersion`, optional `baseCommitSha`) plus a git commit count increment.
- **`.agents/`** — Agent-driven task tracking system. Project short ID: `SBXT`. Task plans and issue specs live under `.agents/ralph/`.

## Environment

The sandbox runs with `DOTNET_CLI_TELEMETRY_OPTOUT=1`, `DOTNET_NOLOGO=1`, and `NUGET_PACKAGES=/workspace/.sbx-cache/nuget/packages` pre-set.

## Build & Test (.NET)

```bash
dotnet build src/StashLock.sln
dotnet build src/StashLock.Cli2/StashLock.Cli2.csproj

dotnet test src/StashLock.Tests
dotnet test src/StashLock.Tests --filter "FullyQualifiedName~TestMethodName"
```

## Versioning Scripts

```bash
# Bash — outputs key=value pairs to stdout (or $GITHUB_OUTPUT)
scripts/build/version.sh --file path/to/version.yaml

# Version only
scripts/build/version.sh --version-only

# Override major.minor
scripts/build/version.sh --override 1.2

# PowerShell equivalent
pwsh scripts/build/version.ps1
```

`version.yaml` format:
```yaml
baseVersion: 0.1.0
baseCommitSha: abc1234   # optional — count commits after this SHA
```

## Agent Task System

Active plans are tracked in `.agents/ralph/IMPLEMENTATION_PLAN.md`. Each task has a detailed spec in `.agents/ralph/issues/{taskId}.issue.md`. The agent execution model follows five phases: Resume → Pattern Discovery → Issue Planning → Implementation → Completion.

When picking up agent work, read `IMPLEMENTATION_PLAN.md` first to understand task dependencies and current status before touching any issue files.

## Project Instructions

See the top-level guidance in the `CLAUDE.md` section on **Environment Persistence** for how `/etc/sandbox-persistent.sh` is sourced and the critical rules about never adding shell completion scripts to it.
