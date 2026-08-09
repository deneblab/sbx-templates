# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`sbx-templates` is a Deneblab repository for Claude Code sandbox Docker templates. It contains:

- **`src/sbx-claude-dotnet10/Dockerfile`** — sandbox image extending `docker/sandbox-templates:claude-code` with .NET SDK 10.0; NuGet packages cached at `/workspace/.sbx-cache/nuget/packages`.
- **`src/sbx-claude-dotnet10-node24/Dockerfile`** — .NET SDK 10.0 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/nuget/packages` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-golang124-node24/Dockerfile`** — Go 1.24.2 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/go/` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-python-uv/Dockerfile`** — latest CPython managed by [uv](https://docs.astral.sh/uv/); `uv`/`uvx` copied from `ghcr.io/astral-sh/uv`, uv cache at `/workspace/.sbx-cache/uv`.
- **`.abcversion.json`** — one AbcVersion project per versioned path (`cmd/sbxup`, `src`, and each `src/sbx-claude-*`); see "Versioning".
- **`scripts/build/build-push.sh` / `build-push.ps1`** — build and push the Docker image with version labels.
- **`cmd/sbxup/`** — Go source for `sbxup`, the cross-platform CLI that reads `.sbx/sbxup.config.yaml` and calls `sbx run`. Single package; versioned by AbcVersion via `.abcversion.json`.
- **`scripts/release/manifest.sh`** — assembles the `templates-v*` release manifest and stages its assets; called by both CI and `task templates:*`.
- **`src/*/template.yaml`** — per-template metadata (name, short alias, description) that feeds `manifest.json`.
- **`install.sh` / `install.ps1`** — one-line installers that fetch a checksum-verified `sbxup` binary from GitHub Releases.
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

`.sbx/sbxup.config.yaml` is the **only** path read — there is no search order, so there is never a question of which of several files won. `legacyConfigPaths` in `config.go` lists the previously supported names; they are probed only to turn "config not found" into a rename instruction, never loaded.

When `clone: true` (or `--clone`), `sbxup` passes `--clone` to `sbx run` so the agent works on a private in-container git clone of the host repo. Default is off; `--no-clone` forces it off. The removed `branch` key now warns with a hint to rename it to `clone`.

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace. If not set, no cache mounting occurs.

## Local Templates Without Docker Hub

Pushes touching `src/**` publish a **`templates-v{version}`** release (`.github/workflows/release-templates.yml`)
carrying two assets and a `.sha256` for each: a `manifest.json` catalogue built from
`src/*/template.yaml`, and a `templates-{version}.tar.gz` of `src/`. This stream is separate from
`sbxup-v*`; `latestRelease(client, prefix)` selects between them.

```bash
sbxup --init                        # pick a template from the latest release
sbxup                               # builds locally on first run, reuses the image after
sbxup --build --template dotnet10   # build without editing the config
sbxup --rebuild                     # force a rebuild
sbxup --update-claude               # rebuild only the claude stage
sbxup --refresh                     # re-download manifest + tarball

task templates:manifest             # print the manifest CI would publish
task templates:stage                # stage the full asset set into ./staging
```

Assets are checksum-verified before use and cached at `os.UserCacheDir()/sbxup/templates/<release>/`.
`buildTemplate` uses the same `VERSION` / `SHORT_SHA` / `BUILD_DATE` build-arg contract as
`build-push.sh --no-push`, so a locally built image carries the same OCI labels as a published one.

### The release tarball is what sbxup extracts

`fetchDockerfile` takes `templates-{version}.tar.gz`, not the individual `<name>.Dockerfile`
assets: one verified download populates `<cache>/<release>/src/`, so every template in the release
is then available with no further network access, and each build gets `src/<name>/` as its context —
the same directory `build-push.sh` passes locally.

`extractTarGz` is deliberately strict, because the archive is a remote artifact that becomes
`docker build` input: only directories and regular files, only under `src/`, and a symlink, hard
link or path escaping the destination aborts the extraction rather than being sanitised. Extraction
lands in a sibling temp directory that is swapped in by rename, so an interrupted run cannot leave a
half-populated tree for a later run to build from.

**`schemaVersion` is the compatibility signal.** Schema 1 releases also shipped each
`<name>.Dockerfile` as its own asset; schema 2 ships them only inside the tarball and omits the
per-entry `dockerfile` field. An `sbxup` older than 0.2.6 fetches that asset, so it must not
silently 404 on a schema-2 release — `parseManifest` refuses any `schemaVersion` above what the
build understands and tells the user to run `sbxup --self-update`. Bump `manifestSchema` in
lockstep whenever the published manifest changes shape. Schema 1 releases still work unchanged —
every one of them also published a tarball, so the same code path serves them and their unused
`dockerfile` field is simply ignored.

**The sandbox runtime keeps its own image store, separate from the host Docker daemon.** Confirmed on
Windows: `sbx` runs sandboxes with Docker Desktop closed, while `docker build` fails against
`npipe:////./pipe/dockerDesktopLinuxEngine`. So a locally built image must be imported —
`ensureTemplate` does `docker image save` + `sbx template load` when `sbx template ls` does not
already list the tag.

Consequence for the order of checks: **`sbx template ls` is asked first, always.** It answers without
a Docker daemon, so an already-registered template needs neither Docker nor the network. Asking
`docker image inspect` first would make a stopped Docker Desktop look like "never built" and trigger a
rebuild that cannot succeed. Docker is required only to build or update a template
(`--rebuild`, `--update-claude`, or a first run); `dockerAvailable()` is checked before building so
the user gets an actionable message instead of a raw npipe/socket error.

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

Everything is versioned by [AbcVersion](https://github.com/deneblab/AbcVersion) — one system, one
binary, same behaviour on every OS. A version is `BaseVersion` (`0.2.0`, from `.abcversion.json`)
plus the number of commits touching a subtree:

```bash
abcversion -p semversion --scope src                       # task templates:version
abcversion -p semversion --scope src/sbx-claude-dotnet10   # task version:dotnet10
abcversion -p semversion --project sbxup                   # task sbxup:version
```

| Scope | Versions |
|---|---|
| `src` | the `templates-v*` release and its tarball |
| `src/sbx-claude-*` | that one template's image tag and manifest entry |
| `--project sbxup` (`cmd/sbxup`) | the `sbxup-v*` release |

**`--scope` needs no configuration**, so adding a template is still adding a directory — nothing
in `.abcversion.json` to update. Only `sbxup` is a named project, because it is a release stream
with an identity of its own rather than a number derived from a directory.

Two AbcVersion flags are easy to confuse: `--path` is a *locator* (which repository to read;
naming a subdirectory still versions the whole repo), while `--scope` is the *filter*. They cannot
be combined with `--project`, and a scope matching no commits is a hard error — so a typo fails
the build instead of quietly producing a repo-wide number.

Per-directory scoping is what keeps an unchanged template's tag stable across a release, so `sbxup`
reuses the image users already built instead of rebuilding it.

`abcversion` **1.2.18+** (the release that added `--scope`) must be on `PATH` for `task build:*`,
`task version:*`, and the release scripts, which check the version and say so if it is too old; CI
installs the native binary directly (no .NET SDK). Get it from
[releases](https://github.com/deneblab/abcversion/releases/latest).

If a single template ever needs its own `BaseVersion` — a deliberate minor bump for one image —
give that one a `Projects` entry and point its call at `--project` instead; the two styles coexist.

`docs/abcversion-path-vs-project.md` compares the three scoping flags, records the measurements
behind the choice, and lists the failure modes (all of which exit non-zero).

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
