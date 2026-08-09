# sbx-runner (deprecated)

> **Deprecated — superseded by `sbxup`.**
>
> `sbxup` is a single cross-platform binary (Linux, macOS, Windows) with the same behaviour, plus
> `--self-update`. It installs onto `PATH` with one command and needs no profile edits:
>
> ```powershell
> irm https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.ps1 | iex
> ```
>
> See the [root README](../README.md#run-a-sandbox). Your existing `.agents\sbx-runner.yaml` keeps
> working — `sbxup` reads it unchanged.
>
> After installing `sbxup`, remove the dot-source line from `$PROFILE.CurrentUserAllHosts`.
> This script remains only so current setups do not break; it will be removed in a later release.

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
clone: false    # true => run on a private in-container git clone
```

| Key        | Required | Description                                                        |
|------------|----------|--------------------------------------------------------------------|
| `template` | yes      | Docker image for the sandbox                                       |
| `agent`    | yes      | Agent name passed to `sbx run` (e.g. `claude`)                    |
| `clone`    | no       | `true` passes `--clone` to `sbx run` (private in-container git clone); default `false` |

## Usage

```powershell
sbx-runner                     # Run sandbox using config defaults
sbx-runner --clone             # Run on a private in-container git clone
sbx-runner --no-clone          # Force clone mode off
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
| `--clone`      | switch | Pass `--clone` to `sbx run` (private in-container git clone); overrides config |
| `--no-clone`   | switch | Force clone mode off, even if enabled in config                        |
| `-Exec`        | switch | Exec into an existing sandbox instead of creating one                  |
| `-Status`      | switch | List sandboxes matching the current project folder                     |
| `-Stop`        | switch | Stop the sandbox for the current project                               |
| `-DryRun`      | switch | Show what would run without executing                                  |
| `-Help`        | switch | Show usage information                                                 |

Extra arguments after the named parameters are passed through to `sbx run`.

## Behavior

- **Config lookup**: searches `.agents\sbx-runner.yaml` first, then `sbx-runner.yaml` in the current directory. Unknown YAML keys trigger a warning; the removed `branch` key warns with a hint to rename it to `clone`.
- **Clone mode**: when `clone: true` (or `--clone`) is set, `--clone` is passed to `sbx run` so the agent works on a private in-container git clone of the host repo. Default is off; `--no-clone` forces it off regardless of config.
- **Auto-resume**: if `sbx run` reports that a sandbox already exists, the existing sandbox is resumed automatically.
- **Sandbox naming**: the sandbox name follows the pattern `{agent}-{folder}` (e.g. `claude-my-project`), derived from the agent name and current directory.
