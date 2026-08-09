# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`sbx-templates` is a Deneblab repository for Claude Code sandbox Docker templates. It contains:

- **`src/sbx-claude-dotnet10/Dockerfile`** — sandbox image extending `docker/sandbox-templates:claude-code` with .NET SDK 10.0; NuGet packages cached at `/workspace/.sbx-cache/nuget/packages`.
- **`src/sbx-claude-dotnet10-node24/Dockerfile`** — .NET SDK 10.0 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/nuget/packages` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-golang124-node24/Dockerfile`** — Go 1.24.2 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/go/` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-python-uv/Dockerfile`** — latest CPython managed by [uv](https://docs.astral.sh/uv/); `uv`/`uvx` copied from `ghcr.io/astral-sh/uv`, uv cache at `/workspace/.sbx-cache/uv`.
- **`scripts/version/version.sh` / `version.ps1`** — compute semver from `version.yaml` + git commit count.
- **`scripts/build/build-push.sh` / `build-push.ps1`** — build and push the Docker image with version labels.
- **`cmd/sbxup/`** — Go source for `sbxup`, the cross-platform CLI that reads `.sbx/sbxup.config.yaml` and calls `sbx run`. Single package; versioned by AbcVersion via `.abcversion.json`.
- **`scripts/release/manifest.sh`** — assembles the `templates-v*` release manifest and stages its assets; called by both CI and `task templates:*`.
- **`src/*/template.yaml`** — per-template metadata (name, short alias, description) that feeds `manifest.json`.
- **`install.sh` / `install.ps1`** — one-line installers that fetch a checksum-verified `sbxup` binary from GitHub Releases.
- **`shells/sbx-runner.ps1`** — **deprecated** PowerShell predecessor of `sbxup`; kept so existing setups keep working.
- **`Taskfile.yml`** — cross-platform task runner (`version`, `build`, `push`, `sbxup:*`).
- **`.agents/`** — issue tracking and agent task system. Project short ID: `SBXT`.

## Running a Sandbox

`sbxup` is a single binary for Linux, macOS, and Windows — no profile edits, no dot-sourcing.

```bash
sbxup                # reads .sbx/sbxup.config.yaml, launches sandbox
sbxup --init         # create .sbx/sbxup.config.yaml (picks a template from the latest release)
sbxup --dry-run      # preview without running
sbxup --clone        # run on a private in-container git clone
sbxup --self-update  # update to the latest release
```

Install (or upgrade — both are idempotent):

```bash
curl -sSL https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.sh | sh   # Linux/macOS
```
```powershell
irm https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.ps1 | iex       # Windows
```

### sbxup.config.yaml

Lives at `.sbx/sbxup.config.yaml`:

```
project/
├── .sbx/
│   └── sbxup.config.yaml
```

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false        # optional: true => run on a private in-container git clone
cache: .sbx-cache   # optional: mount local cache dir into sandbox
build:              # optional: build the template locally instead of pulling it
  name: dotnet10
  release: templates-v0.1.3
```

Config search order: `.sbx/sbxup.config.yaml` → `.sbx/sbxup.yaml` → `.agents/sbxup.yaml` → `.agents/sbx-runner.yaml` → `sbxup.yaml` → `sbx-runner.yaml`. The `.agents/` and `sbx-runner` names are kept so projects set up for earlier versions — including the old PowerShell tool — keep working unchanged.

When `clone: true` (or `--clone`), `sbxup` passes `--clone` to `sbx run` so the agent works on a private in-container git clone of the host repo. Default is off; `--no-clone` forces it off. The removed `branch` key now warns with a hint to rename it to `clone`.

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace. If not set, no cache mounting occurs.

## Local Templates Without Docker Hub

Pushes touching `src/**` publish a **`templates-v{version}`** release (`.github/workflows/release-templates.yml`)
carrying each `<name>.Dockerfile`, a `manifest.json` catalogue built from `src/*/template.yaml`, a
`templates-{version}.tar.gz` of `src/`, and a `.sha256` per asset. This stream is separate from
`sbxup-v*`; `latestRelease(client, prefix)` selects between them.

```bash
sbxup --init                        # pick a template from the latest release
sbxup                               # builds locally on first run, reuses the image after
sbxup --build --template dotnet10   # build without editing the config
sbxup --rebuild                     # force a rebuild
sbxup --update-claude               # rebuild only the claude stage
sbxup --refresh                     # re-download manifest + Dockerfile

task templates:manifest             # print the manifest CI would publish
task templates:stage                # stage the full asset set into ./staging
```

Assets are checksum-verified before use and cached at `os.UserCacheDir()/sbxup/templates/<release>/`.
`buildTemplate` uses the same `VERSION` / `SHORT_SHA` / `BUILD_DATE` build-arg contract as
`build-push.sh --no-push`, so a locally built image carries the same OCI labels as a published one.

Whether `sbx run --template` can see a host-daemon image directly is undocumented and may vary by
platform, so `ensureTemplate` asks `sbx template ls` first and only falls back to
`docker image save` + `sbx template load` when the tag is absent. **Not yet verified on a host —
`sbx` is not installed in the dev sandbox.**

## Local Docker Build (Taskfile)

Requires [Task](https://taskfile.dev). Dispatches to `.sh` (Linux/macOS) or `.ps1` (Windows) automatically.

```bash
task version              # print computed semver (default image)
task build                # build default image locally (no push)
task push                 # build and push default image

task build:dotnet10       # build sbx-claude-dotnet10
task build:dotnet10-node24  # build sbx-claude-dotnet10-node24
task build:golang124-node24    # build sbx-claude-golang124-node24
task build:python-uv           # build sbx-claude-python-uv
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
task update-claude:python-uv         # Python + uv
task update-claude                   # default image (dotnet10)
```

This runs `docker build --no-cache-filter claude ...`, re-running only the `claude` stage while keeping all other layers cached. The result is loaded into the local Docker daemon — no push required.

Direct script usage:
```bash
bash scripts/build/build-push.sh --image sbx-claude-dotnet10 --no-push --update-claude
.\scripts\build\build-push.ps1 -ImageName sbx-claude-dotnet10 -NoPush -UpdateClaude
```

### Step 3 — Point sbxup.yaml at the local image

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false
```

The image tag is the same whether built locally or pushed — `sbxup` picks it up from the local daemon automatically.

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

Two versioning systems coexist deliberately:

- **Docker images** — `version.yaml` + `scripts/version/version.sh` (above).
- **`sbxup`** — [AbcVersion](https://github.com/deneblab/AbcVersion) via `.abcversion.json`, scoped to the `cmd/sbxup` path so a release is cut only when runner source changes:

  ```bash
  abcversion -p semversion --project sbxup   # or: task sbxup:version
  ```

## Building sbxup

```bash
task sbxup:test      # go test ./cmd/sbxup/
task sbxup:build     # build ./bin/sbxup, version stamped via -ldflags
go run ./cmd/sbxup --dry-run
```

`.github/workflows/release-sbxup.yml` cross-compiles six targets (linux/darwin/windows × amd64/arm64) with `CGO_ENABLED=0`, publishes each with a `.sha256` sidecar, and tags the release `sbxup-v{version}`. Statically linked, so the Linux binaries run on musl (Alpine) as well as glibc.

## Agent Task System

Issues tracked in `.agents/issues/{ISSUE_ID}/`. Each issue has `issue.md`, `plan.md`, `state.json`. Active implementation plans in `.agents/ralph/IMPLEMENTATION_PLAN.md`.

Read `IMPLEMENTATION_PLAN.md` before picking up agent work to understand task dependencies and status.

## Sandbox Environment

The sandbox runs with `DOTNET_CLI_TELEMETRY_OPTOUT=1`, `DOTNET_NOLOGO=1`, and `NUGET_PACKAGES=/workspace/.sbx-cache/nuget/packages` pre-set. See the project-level CLAUDE.md (inherited from the sandbox harness) for environment persistence rules and shell completion warnings.
