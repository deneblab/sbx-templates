# sbx-templates

Deneblab sandbox templates for Claude Code development environments.

## Templates

| Image | Extras |
|-------|--------|
| [`pkudrel/sbx-claude-dotnet10`](https://hub.docker.com/r/pkudrel/sbx-claude-dotnet10) | .NET SDK 10.0 |
| [`pkudrel/sbx-claude-dotnet10-node24`](https://hub.docker.com/r/pkudrel/sbx-claude-dotnet10-node24) | .NET SDK 10.0, Node.js 24.x |
| [`pkudrel/sbx-claude-golang124-node24`](https://hub.docker.com/r/pkudrel/sbx-claude-golang124-node24) | Go 1.24.2, Node.js 24.x |

All images extend `docker/sandbox-templates:claude-code`.

## Run a sandbox

### Option A — sbx-runner (recommended)

Dot-source `shells/sbx-runner.ps1` once in `$PROFILE.CurrentUserAllHosts`:

```powershell
. C:\path\to\sbx-templates\shells\sbx-runner.ps1
```

Then from any project directory:

```powershell
sbx-runner --init        # create default .agents\sbx-runner.yaml
sbx-runner               # launch sandbox, auto branch
sbx-runner --dry-run     # preview command without running
sbx-runner --branch feat # named branch
sbx-runner --exec        # open shell in existing sandbox
sbx-runner --status      # list sandboxes for current project
sbx-runner --stop        # stop the sandbox
sbx-runner --help        # show all options
```

### Option B — direct sbx command

```bash
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude --branch auto
```

### sbx-runner.yaml

Place in `.agents\sbx-runner.yaml` of any project:

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto
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
task update-claude                   # default image
```

**Use the locally built image** — set `template` in `.agents\sbx-runner.yaml` to the local tag:

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto
```

The image is served from the local Docker daemon — no registry push needed.

## Versioning

Version is computed from `version.yaml` + git commit count:

```yaml
# version.yaml
baseVersion: 0.1.0
baseCommitSha: abc1234   # optional — count commits only after this SHA
```

```bash
bash scripts/version/version.sh --version-only   # e.g. 0.1.5
pwsh scripts/version/version.ps1 -VersionOnly
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
