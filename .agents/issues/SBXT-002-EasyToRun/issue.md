---
issueId: "SBXT-002-EasyToRun"
humanTitle: "Easy To Run"
issueUrl: ""
createdAt: "2026-04-08T00:00:00Z"
tags: [dx, tooling]
---

# Easy To Run

Make the sandbox templates easy to run locally and in CI with minimal setup friction.


Not to run sbx with temnapte i need to do (or similar):  
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:v1 claude --branch auto

Any options to make it simpelr?

Make deataile propositions

Whato about:
- aliases
- dedicate app (runer)


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Reduce `sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:v1 claude --branch auto` to the shortest possible invocation.
- Scope: Researched official sbx docs — **no project-level config file exists** for sbx. Three viable approaches follow, ranked by effort vs. payoff.

### Proposition 1 — `task run` (recommended, zero extra tooling)
Add a `run` task to the existing `Taskfile.yml`. Works immediately since Task is already present.
```
task run                        # uses :latest, branch=auto
task run TAG=v0.1.2             # pin a specific image version
task run BRANCH=feature/x       # named branch
```
Implementation: add one task to `Taskfile.yml` with vars `TAG` (default `latest`) and `BRANCH` (default `auto`). No new files needed.

### Proposition 2 — Shell alias / PowerShell function (detailed)

#### What gets installed

**Bash/Zsh** — appended to `~/.bashrc` (bash) or `~/.zshrc` (zsh):
```bash
# sbx-dotnet10 — added by sbx-templates setup-alias.sh
alias sbx-dotnet10='sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude --branch auto'
```
Usage: `sbx-dotnet10` (uses :latest, auto branch)

**PowerShell** — appended to `$PROFILE.CurrentUserAllHosts` (typically `~/Documents/PowerShell/profile.ps1`):
```powershell
# sbx-dotnet10 — added by sbx-templates setup-alias.ps1
function sbx-dotnet10 { sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude --branch auto @args }
```
Usage: `sbx-dotnet10` or `sbx-dotnet10 --some-extra-flag` (extra args forwarded)

#### Files to create

**`scripts/setup-alias.sh`**
- Detects shell: reads `$SHELL`, picks `~/.zshrc` for zsh, `~/.bashrc` for bash, falls back to `~/.profile`
- Idempotency: greps for `sbx-dotnet10` before appending; prints "already installed" if found
- Prints the profile path it wrote to
- `--uninstall` flag: removes the alias block from the profile file

**`scripts/setup-alias.ps1`**
- Target: `$PROFILE.CurrentUserAllHosts` (creates file + parent dirs if missing)
- Idempotency: checks if `sbx-dotnet10` already exists in profile
- `[switch]$Uninstall` parameter: removes the function block
- Works on Windows PowerShell 5.1+ and PowerShell 7+

#### Taskfile integration
Add `task alias:install` and `task alias:uninstall` tasks that call the appropriate script via the existing OS dispatcher pattern.

#### Versions / overrides
The alias always uses `:latest`. For one-off pinned runs the user still calls sbx directly. This is intentional — the alias is for the "standard daily workflow" case.

#### Limitation
Alias is machine-local. After a template image rename, user must re-run `bash scripts/setup-alias.sh` (or `task alias:install`) to update. The setup scripts should be re-runnable: they overwrite the existing alias block, not append another copy.

### Proposition 3 — sbx user profile (native sbx feature)
`sbx` supports user-level TOML profiles at `~/.local/config/sbx/profiles/<name>.toml`. A profile can bake in `--template` and default agent. Invocation becomes something like `sbx run dotnet10`. Exact TOML schema needs confirmation from sbx source — not yet documented publicly. Low effort if schema is simple; may be the most "official" solution.

### Proposition 4 — Dedicated runner script (repo-local, no extra tooling)
A thin `run.sh` + `run.ps1` at the repo root (or in `scripts/`) that encodes the full command once. Invocation: `./run.sh` or `.\run.ps1`. Accepts optional args forwarded to sbx.
```
# run.sh
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude --branch auto "$@"
```
**Pro**: zero tooling, universally understandable, lives in version control. Can read a `sbx-runner.yaml` config for per-repo overrides (template tag, default agent, branch strategy).
**Con**: requires `./` prefix; slightly more chars than an alias but fully repo-contained.

### Proposition 5 — Dedicated .NET global tool ("runner app")
A small .NET CLI tool (`sbx-runner`) published as a NuGet global tool. Reads `sbx-runner.yaml` from the working directory and executes `sbx run` with the configured parameters. Invocation: `sbx-runner` or `sbxr`.
```yaml
# sbx-runner.yaml (per project)
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
branch: auto
```
**Pro**: richest UX — tab completion, validation, can read computed version from `version.yaml`, cross-platform without shell differences. Fits the .NET-first repo. **Con**: highest implementation effort; requires NuGet publish step.

- Constraints: `sbx` does **not** auto-resolve `docker.io` domain — full `docker.io/pkudrel/...` path required in all cases. No project-level sbx config file exists.
- Success criteria: Running the sandbox requires typing ≤15 characters from the repo root; the full image + flags are encoded once and not repeated per invocation.

# ChangeLog
- 2026-04-08 — Issue created
- 2026-04-08 — Added Agent Summary: simplify sbx run invocation via task run
- 2026-04-08 — Expanded with detailed propositions after researching sbx docs (no project config, three options: Taskfile/alias/profile)
- 2026-04-08 — Added Prop 2 alias detail (setup scripts), Prop 4 dedicated runner script, Prop 5 .NET global tool per user request
- 2026-04-08 — Expanded Prop 2 with full implementation detail: setup-alias.sh/.ps1, idempotency, uninstall, Taskfile integration
