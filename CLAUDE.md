# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`sbx-templates` is a Deneblab repository for Claude Code sandbox Docker templates. It contains:

- **`src/sbx-claude-dotnet10/Dockerfile`** — sandbox image extending `docker/sandbox-templates:claude-code` with .NET SDK 10.0.4xx; NuGet packages cached at `/workspace/.sbx-cache/nuget/packages`.
- **`src/sbx-claude-dotnet10-node24/Dockerfile`** — .NET SDK 10.0.4xx + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/nuget/packages` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-golang124-node24/Dockerfile`** — Go 1.24.2 + Node.js 24.x (active LTS); caches at `/workspace/.sbx-cache/go/` and `/workspace/.sbx-cache/npm`.
- **`src/sbx-claude-python-uv/Dockerfile`** — latest CPython managed by [uv](https://docs.astral.sh/uv/); `uv`/`uvx` copied from `ghcr.io/astral-sh/uv`, uv cache at `/workspace/.sbx-cache/uv`.
- **`src/sbx-claude-dotnet10-python-uv/Dockerfile`** — .NET SDK 10.0.4xx + latest CPython via uv; caches at `/workspace/.sbx-cache/nuget/packages` and `/workspace/.sbx-cache/uv`.
- **`.abcversion.json`** — one AbcVersion project per versioned path (`cmd/sbxup`, `src`, and each `src/sbx-claude-*`); see "Versioning".
- **`scripts/build/build-push.sh` / `build-push.ps1`** — build and push the Docker image with version labels.
- **`cmd/sbxup/`** — Go source for `sbxup`, the cross-platform CLI that reads `.sbx/sbxup.config.yaml` and calls `sbx run`. Single package; versioned by AbcVersion via `.abcversion.json`.
- **`scripts/release/manifest.sh`** — assembles the `templates-v*` release manifest and stages its assets; called by both CI and `task templates:*`.
- **`src/*/template.yaml`** — per-template metadata (name, short alias, description) that feeds `manifest.json`.
- **`install.sh` / `install.ps1`** — one-line installers that fetch a checksum-verified `sbxup` binary from GitHub Releases.
- **`Taskfile.yml`** — cross-platform task runner (`version`, `build`, `push`, `sbxup:*`).
- **`.agents/`** — issue tracking and agent task system. Project short ID: `SBXT`.

### The .NET SDK comes from `dotnet-install.sh`, not apt

The three .NET templates pin the SDK with `ARG DOTNET_SDK_VERSION` (currently **10.0.401**) and
install it via the official `dotnet-install.sh` into `/usr/lib/dotnet`. A reader would reasonably
assume apt, so the reason matters:

**Both apt sources cap at the 10.0.1xx feature band.** Ubuntu 26.04's own archive ships
`dotnet-sdk-10.0` at `10.0.112`, and `ppa:dotnet/backports` publishes the same 1xx band. A project
whose `global.json` pins a 4xx SDK (`10.0.400`) is therefore **unsatisfiable from apt by
construction** — `rollForward: latestPatch` stays inside the pinned band and rejects `10.0.112`.
No amount of rebuilding or apt-pinning changes that; only the install script offers 4xx.

Consequences worth keeping in mind when touching these Dockerfiles:

- **`apt-get install dotnet-runtime-10.0` is there for its native dependencies**, not for the
  runtime itself. `dotnet-install.sh` unpacks a tarball and installs no native libraries; letting
  dpkg own that closure (`libicu` above all) is what keeps the image working across base-image
  upgrades. Hand-listing `libicu78` instead would silently break the day Ubuntu renames it.
- **`ppa:dotnet/backports` and `software-properties-common` are gone** from all three templates.
  The PPA offered nothing the 26.04 archive lacks, and nothing now installs the SDK from apt.
- **The `ARG` is the cache key.** An unpinned `apt-get install` sits in a cached layer that can
  never invalidate itself, so the SDK silently froze at whatever was current when the layer was
  first built. Bumping `DOTNET_SDK_VERSION` necessarily rebuilds that layer.
- **Each `deps` stage asserts `dotnet --version` equals the pin**, so a silent fall back to another
  band fails the build instead of surfacing in a user's sandbox.
- **`build-push.sh --dotnet-sdk-version X.Y.Z`** (or `DOTNET_SDK_VERSION` in the environment)
  overrides the pin for one build; `build-push.ps1` takes `-DotnetSdkVersion`. The flag is passed to
  `docker` **only when set**, because `golang124-node24` and `python-uv` declare no such `ARG` and
  would otherwise warn about an unconsumed build-arg. `sbxup` never passes it, so the Dockerfile
  default governs there.

### Managed settings switch off Claude attribution

Every template's `deps` stage writes `/etc/claude-code/managed-settings.json`
(`{"attribution":{"commit":"","pr":""}}`, mode `0644`), so Claude Code adds no `Co-Authored-By`
trailer to commits and no "Generated with Claude Code" line to PRs. Consequences worth keeping in mind:

- **It is inlined, not shared.** `sbxup` and `build-push.*` give each build only `src/<name>/` as its
  context, so a file under a shared `src/` directory is unreachable. The same one-line `RUN` sits in all
  five Dockerfiles, just before the first `USER agent`; keep them identical when editing one.
- **`/etc`, not `~/.claude`.** `~/.claude/settings.json` inside a sandbox is written at sandbox start and
  does not survive `sbx rm`, so baking it into the image is unreliable. sbx does not touch `/etc`.
- **It lives in `deps`**, so `--update-claude` (which re-runs only the `claude` stage) keeps it.
- **Managed settings outrank user and project settings.** That is intended; a user who wants attribution
  back must rebuild the template without the block.
- **Verify** with `docker run --rm <image> cat /etc/claude-code/managed-settings.json`, and by making a
  test commit with Claude in a sandbox built from the image.

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
template: dotnet10   # a template from the templates-v* release, built locally on first use
version: 0.2.8       # optional: pin the release (default: latest); a fork: owner/repo@0.1.4
agent: claude        # optional (default: claude)
clone: false         # optional: true => run on a private in-container git clone
cache: .sbx-cache    # optional: mount local cache dir into sandbox
```

The minimal config is `template: dotnet10`. **There is no registry mode**: `template` is always a name
from the release and is always built locally, so a value that looks like an image reference
(`docker.io/...`, `name:tag`, `name@digest`) is a hard error from `checkTemplateName`, which names the
replacement (`sbx-claude-dotnet10`). The same check covers `--template`. Consequences worth keeping in
mind when touching this code:

- **The same fact is stated once.** The old shape repeated the name and version in `template`,
  `build.name` and `build.release`, and `warnTemplateMismatch` existed only to catch them drifting apart.
  With one `template` and one `version` there is nothing to cross-check, so that function is gone.
- **`build:` is deprecated, not removed.** `loadConfig` folds `build.name` into `Template` and
  `build.release` into `Version` and warns. A `template:` beside `build:` is ignored and not validated
  (it named the image tag, not the template); `version` and `build.release` must agree if both are set.
- **Defaults are not written down.** `agent` falls back to `defaultAgent` (`claude`) in `run()`, `clone`
  defaults off, and `--init` writes only `template:` plus a *commented* example pin. The example uses the
  release version, not the image's: `version` pins the release (`templates-v...`), and the two counters
  are independent, so `version: 0.2.8` can build an image `0.2.5` (the start-up line prints both).
- **`--init` needs no network to be valid.** Its fallback body is `template: dotnet10`, because a template
  is just a name resolved on first run.
- **`--build` is still parsed** (and ignored) so an old script does not leak it through to `sbx run`.
- **Older sbxup cannot read the new shape.** It would try to pull an image called `dotnet10`; mention it
  in the release notes when this ships.

`.sbx/sbxup.config.yaml` is the **only** path read — there is no search order, so there is never a question of which of several files won. `legacyConfigPaths` in `config.go` lists the previously supported names; they are probed only to turn "config not found" into a rename instruction, never loaded.

When `clone: true` (or `--clone`), `sbxup` passes `--clone` to `sbx run` so the agent works on a private in-container git clone of the host repo. Default is off; `--no-clone` forces it off. The removed `branch` key now warns with a hint to rename it to `clone`.

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace. If not set, no cache mounting occurs.

### `mounts:` — extra host directories (`mounts.go`)

A list of `path` or `path:ro`, the syntax `sbx run` already takes for an extra workspace; `buildRunArgs`
receives the cache and the mounts as one `workspaces` list and always emits `.` first when it is not
empty (otherwise the first extra directory becomes the primary workspace). Things worth keeping in mind:

- **sbxup adds care, not translation.** The config lives in the repository and the agent runs with
  permissions off, so a project can arrive with someone else's mounts. Layer 1, `checkMountAllowed`, is a
  hard refusal (with or without `:ro`, since a read leaks) for a path that is or contains the home directory
  (which takes in a filesystem root) or that equals, contains or lies inside a `credentialDirs` entry. It is
  about the home directory itself: `~/shared-libs` passes. It judges the path alone, after
  `resolveExisting` has followed symlinks, so a missing directory and `~/link -> ~/.ssh` are both caught.
  Layer 2, `approveMounts`, asks once for every mount outside the project.
- **The approval is stored in the user cache, never in the repo** (`sbxup/mounts/<key>.json`; the file's
  existence is the approval), keyed by project and the exact set of `path|mode` entries, so a repository
  cannot approve itself and switching `:ro` to writable is a new question. `:ro` does not waive it.
- **No is not an error, having no way to answer is.** Declining drops the outside mounts and starts anyway;
  no terminal, or input that ends before anything is typed, refuses. Nothing is asked when an existing
  sandbox is resumed (its mounts are fixed and the run args are dropped, so `prepareMounts` prints a note
  about `sbx rm`) or under `--dry-run`. Checks and approval run before a template is built, so a refusal
  costs no minutes.
- **The suffix rule** (`parseMountSpec`): the text after the last `:` when it holds no path separator and
  the colon is not a drive-letter colon. `:ro` is passed on, `:rw` is accepted and dropped (sbx knows no
  such suffix), anything else is an error, so `:r0` cannot mount a directory writable. `D:\data` and
  `D:\data:ro` both parse.
- **Write access is the default**, which assumes an extra directory without `:ro` is writable in sbx. The
  docs only say "append `:ro` to make it read-only"; this has not been verified on a real sbx.
- **The mounted path is the symlink-resolved one**, so what is checked is what is mounted.

## Local Templates Without Docker Hub

Pushes touching `src/**` publish a **`templates-v{version}`** release (`.github/workflows/release-templates.yml`)
carrying two assets and a `.sha256` for each: a `manifest.json` catalogue built from
`src/*/template.yaml`, and a `templates-{version}.tar.gz` of `src/`. This stream is separate from
`sbxup-v*`; `latestRelease(client, ref, prefix)` selects between them.

```bash
sbxup --init                        # pick a template from the latest release
sbxup                               # builds locally on first run, reuses the image after
sbxup --template dotnet10           # use (and build, if missing) a template without editing the config
sbxup --update                      # check for a newer template version and build it, without asking
sbxup --rebuild                     # force a rebuild
sbxup --update-claude               # rebuild only the claude stage
sbxup --refresh                     # re-check for a newer release, re-download its assets

task templates:manifest             # print the manifest CI would publish
task templates:stage                # stage the full asset set into ./staging
```

Assets are checksum-verified before use and cached at
`os.UserCacheDir()/sbxup/templates/<owner>-<repo>/<release>/`.
`buildTemplate` uses the same `VERSION` / `SHORT_SHA` / `BUILD_DATE` build-arg contract as
`build-push.sh --no-push`, so a locally built image carries the same OCI labels as a published one.

### `version:` names a source repository, not just a tag

`parseReleaseRef` turns a `version` value (formerly `build.release`) into a `releaseRef{Owner, Repo, Tag}`:
`deneblab/sbx-templates@0.1.4`, `@latest`, `latest`, a bare `0.1.4` / `templates-v0.1.4` for the default
repository, or empty for its newest release. **An unrecognised value is a hard error**, never a
fallback to latest — the key exists to make a build reproducible, so a typo like `lastest` must fail
rather than quietly float. `owner/repo` is validated before use because it becomes both a URL path
and a cache directory segment.

Consequences worth keeping in mind when touching this code:

- **`selfUpdate()` is pinned to `defaultRef("")`.** A config may point template builds at a fork; where
  sbxup replaces its own executable must never follow project configuration.
- **`repoAPI` / `repoWeb` remain the canonical endpoints** and are what `releaseRef.apiURL()` returns
  for the default source, so this indirection cannot change where existing installs fetch from.
  `githubAPI` / `githubWeb` are used only for other repositories.
- **The cache and the local image tag are both namespaced by source.** Two repositories can publish
  `templates-v0.1.4`, and the sandbox runtime has a single image store: a fork's build is tagged
  `<owner>-<template>:<version>` so it cannot overwrite the canonical image. The default source keeps
  its bare tag, so no existing tag changed.
- **A resolved `latest` is cached for `latestTTL` (180 h) in `latest.json`.** Order: pin → fresh
  record → network → stale record with a warning. This is what lets a config carry no version at all
  and still start offline; without it, dropping the pin would trade a version number for a hard
  network dependency on every run.

### A new template version is offered, never built unasked

`update.go`. When the release carries a template version that is not built, `ensureLocalTemplate` calls
`offerUpdate` unless the release is pinned or the user consented (`--update`, `--rebuild`,
`--update-claude`). Consequences worth keeping in mind:

- **"Update" is told from "first start" by `sbx template ls`.** `sbxTemplateTags(repo)` lists the
  built versions of `entry.LocalRepo(ref)`; no numeric version means a first start, which builds without
  asking. It needs no Docker daemon, which is what lets an update be offered while Docker is closed.
  Matching is on the canonical repository, so the frozen `docker.io/pkudrel/...` image is not mistaken
  for a built version.
- **Nothing is fetched before consent.** Detecting an update costs one `manifest.json`; the tarball and
  the build wait for a "yes". Running the installed version needs only its registered tag.
- **A release that is not ahead of what is installed** (another project already built a newer one) just
  runs the newest installed version; there is no downgrade offer.
- **The default answer is no, and the question has three refusals.** No terminal, Docker unreachable and
  `--dry-run` all print the same "running X, `sbxup --update` builds Y" line without asking. Input that
  ends before anything is typed (`< /dev/null`, which `interactive()` counts as a terminal) is *no
  answer*, not a "no", so it is not remembered.
- **A "no" lives in `latest.json`, in `latestRecord.Declined`.** It keeps the record's `ResolvedAt`, so
  the usual 180 h check still happens, and that check writes a fresh record, which clears the list. So a
  "no" means "ask again after the next check" with no state file of its own. `--refresh` and `--update`
  both re-check the network.
- **Everything here is global per user**: the cache, the built images and `latest.json` are not per
  project. A "yes" in one project makes the new tag available to new sandboxes everywhere; a "no" silences
  the question everywhere. A pin (`version:`) is the per-project control.
- **A sandbox keeps the image it was created with**, and `run()` resumes it by name with no run args.
  After a build, `ensureLocalTemplate` says so and names `sbx rm`; it never runs it, since that deletes
  the sandbox's state.

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
task build:dotnet10-python-uv  # build sbx-claude-dotnet10-python-uv
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
task update-claude:dotnet10-python-uv  # .NET 10 + Python/uv
task update-claude                   # default image (dotnet10)
```

This runs `docker build --no-cache-filter claude ...`, re-running only the `claude` stage while keeping all other layers cached. The result is loaded into the local Docker daemon — no push required.

Direct script usage:
```bash
bash scripts/build/build-push.sh --image sbx-claude-dotnet10 --no-push --update-claude
.\scripts\build\build-push.ps1 -ImageName sbx-claude-dotnet10 -NoPush -UpdateClaude
```

### Step 3 — Run the local image

`sbxup` does not take an image reference (`template` is a name; see "sbxup.config.yaml"), so an image
built by `task build:*` is run with `sbx` directly:

```bash
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude
```

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
