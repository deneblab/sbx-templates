# sbx-templates

Deneblab sandbox templates for Claude Code development environments.

## Templates

| Image | Extras |
|-------|--------|
| [`pkudrel/sbx-claude-dotnet10`](https://hub.docker.com/r/pkudrel/sbx-claude-dotnet10) | .NET SDK 10.0 |
| [`pkudrel/sbx-claude-dotnet10-node24`](https://hub.docker.com/r/pkudrel/sbx-claude-dotnet10-node24) | .NET SDK 10.0, Node.js 24.x |
| [`pkudrel/sbx-claude-golang124-node24`](https://hub.docker.com/r/pkudrel/sbx-claude-golang124-node24) | Go 1.24.2, Node.js 24.x |
| [`pkudrel/sbx-claude-python-uv`](https://hub.docker.com/r/pkudrel/sbx-claude-python-uv) | Latest Python via [uv](https://docs.astral.sh/uv/) |
| `sbx-claude-dotnet10-python-uv` | .NET SDK 10.0, latest Python via [uv](https://docs.astral.sh/uv/) — release-only, no Docker Hub image |

All images extend `docker/sandbox-templates:claude-code`.

> **Docker Hub publishing is no longer automatic.** Templates are distributed as Dockerfiles via the
> `templates-v*` release and built locally by `sbxup` — see
> [Run without Docker Hub](#run-without-docker-hub). The images above still exist but are frozen at
> their last build; `build-push.yml` now runs only when triggered by hand.

## Run a sandbox

### Option A — sbxup (recommended)

`sbxup` is a single self-contained binary for Linux, macOS, and Windows. Install it with one command:

```bash
# Linux and macOS
curl -sSL https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.ps1 | iex
```

The installer picks the right binary for your platform, verifies its SHA-256 against the published
checksum, and installs to `~/.local/bin` (`%LOCALAPPDATA%\Programs\sbxup` on Windows). Set
`SBXUP_VERSION` to pin a release and `SBXUP_INSTALL_DIR` to choose the location.

Then from any project directory:

```bash
sbxup --init         # create .sbx/sbxup.config.yaml (pick a template)
sbxup                # launch sandbox
sbxup --dry-run      # preview command without running
sbxup --clone        # run on a private in-container git clone
sbxup --exec         # open shell in existing sandbox
sbxup --status       # list sandboxes for current project
sbxup --stop         # stop the sandbox
sbxup --self-update  # update to the latest release
sbxup --help         # show all options
```

To upgrade later, run `sbxup --self-update` or re-run the install command — both are idempotent.

> The old PowerShell function (`shells/sbx-runner.ps1`) has been **removed**. If your
> `$PROFILE.CurrentUserAllHosts` still dot-sources it, delete that line — `sbxup` replaces it.

### Option B — direct sbx command

```bash
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude --clone
```

### sbxup.yaml

Place in `.sbx/sbxup.config.yaml` of any project:

```
project/
├── .sbx/
│   └── sbxup.config.yaml
```

This is the **only** location `sbxup` reads — there is no search order. A project still holding an
older `.agents/sbxup.yaml`, `.agents/sbx-runner.yaml` or root-level `sbxup.yaml` needs it renamed;
`sbxup` names the stale file and tells you what to rename it to.

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false        # optional: true => run on a private in-container git clone
cache: .sbx-cache   # optional: mount local cache dir into sandbox
build:              # optional: build the template locally instead of pulling it
  name: dotnet10
  release: deneblab/sbx-templates@latest
```

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace in the sandbox. Package caches (NuGet, npm, Go modules) stored under `/workspace/.sbx-cache/` will persist across sandbox runs.

## Run without Docker Hub

Every push that touches `src/` publishes a **`templates-v{version}` GitHub Release** carrying the
Dockerfiles themselves — a `manifest.json` catalogue and a `templates-{version}.tar.gz` of the whole
`src/` tree, each with a `.sha256`. `sbxup` builds from those directly, so no image is ever pulled
from a registry:

```bash
sbxup --init          # lists the templates in the latest release, you pick one
sbxup                 # builds it locally the first time, then reuses the image
```

`--init` writes a config with a `build:` block, which is what marks the template as locally built:

```yaml
template: sbx-claude-dotnet10
agent: claude
clone: false
build:
  name: sbx-claude-dotnet10
  release: deneblab/sbx-templates@latest
```

**No version appears anywhere**, which is the point: you name a template, not a release tag.

### Choosing a release

`release:` takes `<owner>/<repo>@<version|tag|latest>`, and the short forms mean the default
repository:

```yaml
release: deneblab/sbx-templates@latest         # newest release, re-checked periodically
release: deneblab/sbx-templates@0.1.4          # frozen
release: 0.1.4                                 # same, default repository
release: templates-v0.1.4                      # same, full tag
release:                                       # omitted entirely = latest
```

A value that is none of these is an error listing the accepted forms — a mistyped pin never
silently degrades to "latest".

Naming another repository builds that fork's templates instead; its images are tagged
`<owner>-<template>:<version>` so they cannot overwrite the canonical ones in the sandbox image
store. `sbxup --self-update` always updates from `deneblab/sbx-templates`, whatever a project config
says.

With `@latest`, the resolved release tag is remembered for 180 hours, so ordinary runs make no
network call and still work offline; `--refresh` re-checks immediately. The tag in use is printed on
every run (`Template: dotnet10 0.2.6 (templates-v0.2.6)`).

Useful flags:

```bash
sbxup --build --template dotnet10   # build a template without editing the config first
sbxup --rebuild                     # rebuild even though the image exists
sbxup --update-claude               # rebuild only the Claude Code layer
sbxup --refresh                     # re-check for a newer release, re-download its assets
sbxup --init --template dotnet10    # non-interactive; no prompt
```

`sbxup` downloads the manifest and the tarball, then extracts `src/` from it — one download makes
every template in the release available, so switching templates later needs no network at all.

Every downloaded asset is checksum-verified before it is written or built — a Dockerfile becomes the
agent's execution environment, so a mismatch aborts and nothing is built. Extraction is equally
wary: only regular files under `src/`, and any symlink or path escaping the destination aborts it.
Downloads are cached under `~/.cache/sbxup/templates/<owner>-<repo>/<release>/` (`%LocalAppData%` on
Windows); the source repository is part of the path because two repositories can publish the same
release tag.

**Docker Desktop is only needed to build.** The sandbox runtime has its own image store, so once a
template is registered, `sbxup` reuses it without touching Docker or the network — `sbx template ls`
is checked first, and it answers with Docker Desktop closed. You need Docker running for a first
build, `--rebuild`, or `--update-claude`; if it is not, sbxup says so instead of failing inside the
builder.

`--init` degrades rather than fails: with no network, no release, or a non-interactive stdin and no
`--template`, it writes the standard registry config instead.

**This is not an air-gapped build.** Nothing of *ours* is pulled from Docker Hub, but the base image
`docker/sandbox-templates:claude-code`, apt, and the Claude Code install script are still fetched.

To reproduce what CI would publish:

```bash
task templates:manifest   # print manifest.json
task templates:stage      # stage the full asset set into ./staging
```

## Build and push

Requires [Task](https://taskfile.dev). Works on Linux, macOS, and Windows.

```bash
task version              # show computed semver (default image)
task build                # build default image locally
task push                 # build and push default image

task build:dotnet10       # build sbx-claude-dotnet10
task build:dotnet10-node24  # build sbx-claude-dotnet10-node24
task build:golang124-node24    # build sbx-claude-golang124-node24
task build:python-uv           # build sbx-claude-python-uv
task build:dotnet10-python-uv  # build sbx-claude-dotnet10-python-uv
```

Docker Hub secrets required: `DOCKER_USERNAME`, `DOCKER_TOKEN`.

`.github/workflows/build-push.yml` is **manual-only** — it no longer runs on push to `main`. Publish
with "Run workflow" in the Actions tab when you actually want to refresh the Docker Hub images, or
re-add a `push:` trigger to restore the old behaviour. Note it only ever built
`sbx-claude-dotnet10`; the other four images were never published by CI.

## Updating Claude Code locally

Each Dockerfile has two stages: `deps` (runtimes) and `claude` (Claude Code install). This lets you update Claude Code without rebuilding the slow dependency layers.

**First build** (full, loads into local Docker daemon):

```bash
task build:dotnet10
```

**Update Claude Code only** (skips `deps` layer cache, takes seconds):

```bash
task update-claude:dotnet10          # .NET 10
task update-claude:dotnet10-node24   # .NET 10 + Node 24
task update-claude:golang124-node24  # Go + Node 24
task update-claude:python-uv         # Python + uv
task update-claude:dotnet10-python-uv  # .NET 10 + Python/uv
task update-claude                   # default image
```

**Use the locally built image** — set `template` in `.sbx\sbxup.config.yaml` to the local tag:

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false
```

The image is served from the local Docker daemon — no registry push needed.

## Build sbxup from source

Requires Go (see `go.mod` for the version).

```bash
task sbxup:test      # run the Go tests
task sbxup:build     # build ./bin/sbxup for the current platform
task sbxup:version   # show the computed sbxup version
go run ./cmd/sbxup --dry-run
```

Releases are cut by `.github/workflows/release-sbxup.yml` on pushes to `main` that touch
`cmd/sbxup/**`. It cross-compiles six targets (linux/darwin/windows × amd64/arm64), publishes each
with a `.sha256` sidecar, and tags the release `sbxup-v{version}`.

## Versioning

Everything is versioned with [AbcVersion](https://github.com/deneblab/AbcVersion). A version is
`BaseVersion` plus the number of commits touching a subtree, so a release is cut only when the code
behind it actually changed:

```bash
abcversion -p semversion --scope src                       # the templates-v* release
abcversion -p semversion --scope src/sbx-claude-dotnet10   # one template's image tag
abcversion -p semversion --project sbxup                   # the sbxup-v* release
```

`--scope` narrows the count to a directory without any configuration, so each template is versioned
by its own commits — an unchanged template keeps its version across a release and `sbxup` reuses the
image you already built. Adding a template is adding a directory; there is nothing to register.

`.abcversion.json` therefore holds only the base version and the one stream that isn't derived from
a directory:

```json
{
  "BaseVersion": "0.2.0",
  "Projects": {
    "sbxup": { "Name": "sbxup", "Path": "cmd/sbxup", "BaseVersion": "0.2.0" }
  }
}
```

`abcversion` **1.2.18+** (which introduced `--scope`) needs to be on `PATH` for `task build:*`,
`task version:*` and the release scripts — grab the native binary from
[releases](https://github.com/deneblab/abcversion/releases/latest).

## Merge agent changes

After a sandbox session, the agent's work is on an isolated branch under `.sbx/`:

```bash
git worktree list                  # find the branch name
git diff main <agent-branch>       # review changes
git merge <agent-branch>           # accept
git worktree remove .sbx/<name>    # clean up
git branch -d <agent-branch>
```

Add `.sbx/` to `.gitignore` to avoid tracking worktree directories.
