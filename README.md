# sbx-templates

Deneblab sandbox templates for Claude Code development environments.

## Templates

| Image | Extras |
|-------|--------|
| [`pkudrel/sbx-claude-dotnet10`](https://hub.docker.com/r/pkudrel/sbx-claude-dotnet10) | .NET SDK 10.0 |
| [`pkudrel/sbx-claude-dotnet10-node24`](https://hub.docker.com/r/pkudrel/sbx-claude-dotnet10-node24) | .NET SDK 10.0, Node.js 24.x |
| [`pkudrel/sbx-claude-golang124-node24`](https://hub.docker.com/r/pkudrel/sbx-claude-golang124-node24) | Go 1.24.2, Node.js 24.x |
| [`pkudrel/sbx-claude-python-uv`](https://hub.docker.com/r/pkudrel/sbx-claude-python-uv) | Latest Python via [uv](https://docs.astral.sh/uv/) |

All images extend `docker/sandbox-templates:claude-code`.

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
sbxup --init         # create default .agents/sbxup.yaml
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

> The previous PowerShell function (`shells/sbx-runner.ps1`) is **deprecated** but still works.
> After installing `sbxup`, remove the `. C:\path\to\sbx-templates\shells\sbx-runner.ps1` line from
> your `$PROFILE.CurrentUserAllHosts`.

### Option B — direct sbx command

```bash
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude --clone
```

### sbxup.yaml

Place in `.agents/sbxup.yaml` of any project. Existing `.agents/sbx-runner.yaml` files keep working —
`sbxup` searches `.agents/sbxup.yaml`, `.agents/sbx-runner.yaml`, `sbxup.yaml`, then `sbx-runner.yaml`:

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false        # optional: true => run on a private in-container git clone
cache: .sbx-cache   # optional: mount local cache dir into sandbox
```

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace in the sandbox. Package caches (NuGet, npm, Go modules) stored under `/workspace/.sbx-cache/` will persist across sandbox runs.

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
```

Docker Hub secrets required: `DOCKER_USERNAME`, `DOCKER_TOKEN`.

GitHub Actions workflow (`.github/workflows/build-push.yml`) triggers on push to `main`.

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
task update-claude                   # default image
```

**Use the locally built image** — set `template` in `.agents\sbx-runner.yaml` to the local tag:

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

**Docker images** are versioned from `version.yaml` + git commit count:

```yaml
# version.yaml
baseVersion: 0.1.0
baseCommitSha: abc1234   # optional — count commits only after this SHA
```

```bash
bash scripts/version/version.sh --version-only   # e.g. 0.1.5
pwsh scripts/version/version.ps1 -VersionOnly
```

**sbxup** is versioned with [AbcVersion](https://github.com/deneblab/AbcVersion) from
`.abcversion.json`, scoped to the `cmd/sbxup` path so a runner release is cut only when runner
source changes:

```bash
abcversion -p semversion --project sbxup
```

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
