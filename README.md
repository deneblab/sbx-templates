# sbx-templates

Deneblab sandbox templates for Claude Code development environments.

## Templates

| Image | Base | Extras |
|-------|------|--------|
| `docker.io/pkudrel/sbx-claude-dotnet10` | `docker/sandbox-templates:claude-code` | .NET SDK 10.0 |

## Run a sandbox

### Option A — sbx-runner (recommended)

Dot-source `shells/sbx-runner.ps1` once in `$PROFILE.CurrentUserAllHosts`:

```powershell
. C:\path\to\sbx-templates\shells\sbx-runner.ps1
```

Then from any project directory that has `.agents\sbx-runner.yaml`:

```powershell
sbx-runner               # launch sandbox, auto branch
sbx-runner -DryRun       # preview command without running
sbx-runner -Branch feat  # named branch
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
task version   # show computed semver
task build     # build image, load into local Docker daemon
task push      # build and push to Docker Hub
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
bash scripts/build/version.sh --version-only   # e.g. 0.1.5
pwsh scripts/build/version.ps1 -VersionOnly
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
