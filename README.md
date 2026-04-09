# sbx-templates

Deneblab sandbox templates for Claude Code development environments.

## Templates

| Image | Base | Extras |
|-------|------|--------|
| `docker.io/pkudrel/sbx-claude-dotnet10` | `docker/sandbox-templates:claude-code` | .NET SDK 10.0 |
| `docker.io/pkudrel/sbx-claude-dotnet10-node` | `docker/sandbox-templates:claude-code` | .NET SDK 10.0, Node.js 24.x |
| `docker.io/pkudrel/sbx-claude-golang-node` | `docker/sandbox-templates:claude-code` | Go 1.24.2, Node.js 24.x |

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
```

## Build and push

Requires [Task](https://taskfile.dev). Works on Linux, macOS, and Windows.

```bash
task version              # show computed semver (default image)
task build                # build default image locally
task push                 # build and push default image

task build:dotnet10       # build sbx-claude-dotnet10
task build:dotnet10-node  # build sbx-claude-dotnet10-node
task build:golang-node    # build sbx-claude-golang-node
```

Docker Hub secrets required: `DOCKER_USERNAME`, `DOCKER_TOKEN`.

GitHub Actions workflow (`.github/workflows/build-push.yml`) triggers on push to `main`.

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
