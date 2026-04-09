# sbx-runner

PowerShell function that wraps the `sbx` CLI to launch, manage, and connect to Claude Code sandboxes using a simple YAML config.

## Setup

Dot-source the script in your PowerShell profile (`$PROFILE.CurrentUserAllHosts`):

```powershell
. C:\path\to\sbx-templates\shells\sbx-runner.ps1
```

## Config file

Place a `sbx-runner.yaml` in your project at `.agents/sbx-runner.yaml` (preferred) or `sbx-runner.yaml` in the project root.

```yaml
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto    # resolves to current git branch
```

| Key        | Required | Description                                                        |
|------------|----------|--------------------------------------------------------------------|
| `template` | yes      | Docker image for the sandbox                                       |
| `agent`    | yes      | Agent name passed to `sbx run` (e.g. `claude`)                    |
| `branch`   | no       | Branch name, or `auto` to detect from `git rev-parse --abbrev-ref HEAD` |

## Usage

```powershell
sbx-runner                     # Run sandbox using config defaults
sbx-runner -Branch feat        # Override the branch name
sbx-runner -Exec               # Open a shell in the existing sandbox
sbx-runner -Status             # List sandboxes for the current project
sbx-runner -Stop               # Stop the sandbox for the current project
sbx-runner -DryRun             # Preview the sbx command without running it
sbx-runner -Help               # Show built-in help
```

### Parameters

| Parameter      | Type   | Description                                                            |
|----------------|--------|------------------------------------------------------------------------|
| `-Config`      | string | Path to YAML config (default: `.agents\sbx-runner.yaml` or `sbx-runner.yaml`) |
| `-Template`    | string | Docker image (overrides config)                                        |
| `-Agent`       | string | Agent name (overrides config)                                          |
| `-Branch`      | string | Branch name (overrides config); `auto` in config resolves to current git branch |
| `-Exec`        | switch | Exec into an existing sandbox instead of creating one                  |
| `-Status`      | switch | List sandboxes matching the current project folder                     |
| `-Stop`        | switch | Stop the sandbox for the current project                               |
| `-DryRun`      | switch | Show what would run without executing                                  |
| `-Help`        | switch | Show usage information                                                 |

Extra arguments after the named parameters are passed through to `sbx run`.

## Behavior

- **Config lookup**: searches `.agents\sbx-runner.yaml` first, then `sbx-runner.yaml` in the current directory. Unknown YAML keys trigger a warning.
- **Auto-branch**: when `branch: auto` is set, the current git branch is detected automatically.
- **Auto-resume**: if `sbx run` reports that a sandbox already exists, the existing sandbox is resumed automatically.
- **Sandbox naming**: the sandbox name follows the pattern `{agent}-{folder}` (e.g. `claude-my-project`), derived from the agent name and current directory.
