# sbx-templates

Deneblab sandbox templates for Claude Code development environments.

## Templates

| Template | Extras |
|----------|--------|
| `dotnet10` | .NET SDK 10.0.4xx |
| `dotnet10-node24` | .NET SDK 10.0.4xx, Node.js 24.x |
| `golang124-node24` | Go 1.24.2, Node.js 24.x |
| `python-uv` | Latest Python via [uv](https://docs.astral.sh/uv/) |
| `dotnet10-python-uv` | .NET SDK 10.0.4xx, latest Python via [uv](https://docs.astral.sh/uv/) |

Name one in `template:` (see [Example configs](#example-configs)); `sbxup` builds it locally from the
`templates-v*` release.

All templates extend `docker/sandbox-templates:claude-code`.

The .NET images install the SDK from `dotnet-install.sh`, pinned by `ARG DOTNET_SDK_VERSION`
(currently `10.0.401`), because both apt sources cap at the 10.0.1xx band — a `global.json`
pinning a 4xx SDK cannot be satisfied from apt. Override for one build with
`build-push.sh --dotnet-sdk-version X.Y.Z` (PowerShell: `-DotnetSdkVersion`).

**No Claude attribution.** Every image writes `/etc/claude-code/managed-settings.json` with empty
`attribution.commit` and `attribution.pr`, so Claude Code adds no `Co-Authored-By: Claude` trailer to
commits and no "Generated with Claude Code" line to pull requests. It is a *managed* setting: it sits
above user and project settings, so a project's own `.claude/settings.json` cannot turn attribution back
on. To get it back, rebuild the template without that block in its `Dockerfile`.

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

The smallest useful config is one line:

```yaml
template: dotnet10
```

With every key:

```yaml
template: dotnet10   # a template from the templates-v* release, built locally on first use
version: 0.2.8       # optional: pin the release (default: latest); a fork: owner/repo@0.1.4
agent: claude        # optional (default: claude)
sandbox: app_claude  # optional: sandbox name (default: <parent>_<folder>_<agent>)
clone: false         # optional: true => run on a private in-container git clone
cache: .sbx-cache    # optional: mount local cache dir into sandbox
mounts:              # optional: extra host directories; ':ro' for read-only (see below)
  - ~/shared-libs:ro
github:              # optional: a short-lived GitHub App token for the sandbox (see below)
  bitwarden-item: github-deneblab-agent
hide: [dev]          # optional: directories the agent cannot reach (default: dev, when it exists)
```

The sandbox is named after the project and its parent directory: `/w/deneblab/sbx-templates` gets
`deneblab_sbx-templates_claude` (a project at a drive or filesystem root: `<folder>_<agent>`), so two
projects that share a folder name in different organizations get different sandboxes. `sandbox:` sets the
name verbatim (letters, digits, `-` and `_`; no `.`, because sbx cannot apply a network approval to such a
sandbox). A sandbox named with dots by an older `sbxup` is still resumed; `sbx rm <name>` and a new start
replace it with one approvals work for. A sandbox from an older `sbxup` (`claude-<folder>`) is not
resumed: the next start creates a new one, and `sbx rm claude-<folder>` removes the old one.

Not a config key: `sbxup` also imports your skills into sbx's store (see "Skills" below); `--no-skills` skips it.

`template` is a **name**, never an image reference: `sbxup` builds every template locally from the
release and does not pull one of ours from a registry. A value like
`docker.io/pkudrel/sbx-claude-dotnet10:latest` is an error that names the replacement
(`template: sbx-claude-dotnet10`). Old configs that use a `build:` block still load, with a warning:
`build.name` becomes `template` and `build.release` becomes `version`.

### Example configs

The template is chosen by its short name: `dotnet10`, `dotnet10-node24`, `dotnet10-python-uv`,
`golang124-node24` or `python-uv`. Every key except `template` is optional.

**Minimal** — the latest release, everything else at its default:

```yaml
template: dotnet10
```

**A different stack, on a private clone, with a package cache:**

```yaml
template: golang124-node24
clone: true              # the agent works on a private in-container git clone of the repo
cache: .sbx-cache        # created at the project root if it is missing
```

**Frozen environment** — stay on one release until you change the line:

```yaml
template: dotnet10
version: 0.2.6           # or templates-v0.2.6; without it sbxup follows the latest
```

**Shared libraries and docs from outside the project:**

```yaml
template: dotnet10
mounts:
  - ~/shared-libs:ro     # read-only: the right choice for anything that is only a source
  - ../docs              # relative to the project directory; writable
```

**On Windows** — backslashes need no quotes:

```yaml
template: dotnet10
mounts:
  - D:\data\models:ro
  - ~\docs
```

**Everything at once:**

```yaml
template: dotnet10
version: 0.2.6
agent: claude            # the default
clone: false             # the default
cache: .sbx-cache
mounts:
  - ~/shared-libs:ro
  - ../docs
```

**Templates from a fork** — the same `version` key, with the repository in front:

```yaml
template: dotnet10
version: myfork/sbx-templates@0.1.4
```

**Moving an old config over.** The registry form no longer works; `build:` still loads, with a warning.

```yaml
# before: an image reference (now an error that names the replacement)
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
```
```yaml
# after
template: dotnet10
```

```yaml
# before: the same template and release stated three times
template: sbx-claude-dotnet10:0.2.8
agent: claude
clone: false
build:
  name: dotnet10
  release: templates-v0.2.8
```
```yaml
# after
template: dotnet10
version: 0.2.8           # keep it only if you want the release frozen
```

`agent: claude` and `clone: false` can simply be dropped: they are the defaults.

### Mounting more directories

`mounts` lists extra host directories to make available in the sandbox, in the syntax `sbx run` takes
for an extra workspace — `path`, or `path:ro` for read-only:

```yaml
template: dotnet10
mounts:
  - ~/shared-libs:ro      # read-only: the right choice for anything that is only a source
  - ../docs               # relative to the project directory; writable
  - D:\data\models:ro     # a Windows path
```

- **Paths.** `~` is your home directory; a relative path is relative to the project directory (like
  `cache`); there are no environment variables. Inside the sandbox a directory appears at the same
  absolute path as on the host (on Windows `C:\x` shows up as `/c/x`).
- **Modes.** Without a suffix a directory is **writable**. `:ro` is passed to `sbx`; `:rw` is accepted
  for a config that wants to say so, and is dropped before `sbx` sees it. Any other suffix is an error,
  so a typo such as `:r0` cannot mount a directory writable by accident. The agent runs with permissions
  off, so a writable mount can be changed or deleted without asking; every mount and its mode is printed
  at start-up (`Mount: /home/u/shared-libs (read-only)`).
- **A missing directory** is a warning and is skipped: the config is shared and the directory may exist
  on one machine only. The project and the `cache` directory are never mounted twice.
- **Two guards**, because a repository can arrive with someone else's config:
  1. **Refusal.** sbxup will not mount your home directory (or a parent of it, or a filesystem root) or a
     credential directory — `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.claude`, `~/.azure`, `~/.kube`,
     `~/.docker`, `~/.password-store`, `~/.config/gcloud`, `~/.config/gh` — nor anything inside or above
     one, with or without `:ro`, since reading leaks as well. The rest of your home directory is fine:
     `~/shared-libs` is allowed. Symlinks are followed before the check, and the directory need not
     exist. The list catches accidents; it is not complete.
  2. **Approval.** Mounts outside the project need your approval once. The prompt lists each with its
     mode; `:ro` does not skip it. The answer is remembered in your user cache (not in the repository) for
     that project and that exact list, so adding a directory or changing `:ro` to writable asks again.
     Saying no starts the sandbox without those mounts and asks again next time; with no terminal it is
     refused, and running sbxup once from a terminal approves it.
- **They apply when a sandbox is created.** `sbxup` resumes an existing sandbox with no run arguments, so
  a changed `mounts` reaches it only after `sbx rm <name>` (that deletes the sandbox's own state, not your
  project files). sbxup prints a note about it, and asks nothing, when it resumes.

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace in the sandbox. Package caches (NuGet, npm, Go modules) stored under `/workspace/.sbx-cache/` will persist across sandbox runs.

### Hiding directories from the agent

`hide` lists project directories the agent must not reach. The files stay where they are on the host; in
the sandbox each directory shows only a `HIDDEN.txt`:

```yaml
template: dotnet10
hide:
  - dev
  - data/private
```

- **Default.** Without the key, sbxup hides `dev` when the project has one. `hide: []` hides nothing.
- **How.** `sbx` cannot leave part of a workspace out, so sbxup covers each directory with a read-only
  `sbx mount` of its own empty directory (`~/.config/sbxup/hide/`, `%APPDATA%\sbxup\hide\` on Windows,
  `~/Library/Application Support/sbxup/hide/` on macOS) before the agent starts. A directory that does not
  exist yet is covered too (`sbx` creates it, empty, on the host). `sbx` can show the same directory at more
  than one path (a dynamic mount brings `/mnt/host` with the whole workspace), so sbxup covers every path it
  finds in the sandbox's mount table and, on every start, checks that each shows only `HIDDEN.txt`; if one
  does not, the agent is not started.
- **No sudo.** `sudo umount` would remove the cover, so the agent loses `sudo` in that sandbox: no
  `sudo apt-get install` there. Removing an entry later takes its cover away but does not bring `sudo`
  back; `sbx rm <name>` and a new start do. Sandboxes without anything to hide keep `sudo`.
- **Entries** are paths relative to the project: no `..`, no absolute paths, no patterns, and none that
  overlaps `cache`, a `mounts` entry or another entry. A failed cover or `sudo` removal stops the start.
- **Changes apply to an existing sandbox** on its next start: a new entry is covered, a removed one is
  uncovered.
- **Not with clone mode.** `--clone` keeps the whole host repository readable at `/run/sandbox/source`,
  so `hide` and `clone` together are an error for now (set `hide: []` to use clone mode).
- **Keep such directories git-ignored.** The cover hides them from the agent, not from git.

### Skills

sbx mounts one shared `agent-skills` store, read-only, into every sandbox, and fills it only on request.
`sbxup` requests it with `sbx skills import --force`, which copies the skills installed for the coding
agents on this machine (`~/.agents`, `~/.claude`, `~/.config/opencode`, `~/.copilot`, `~/.cursor` and
`~/.factory`, each under `skills/`) into that store. There is no config key.

- **A new sandbox imports without asking.**
- **A resumed sandbox asks, and only when your skills changed** since the last import. sbxup keeps a
  checksum of those directories (files inside each skill folder, by path and content) in your user cache:
  `sbxup/skills-import.json` under `%LOCALAPPDATA%` on Windows, `~/Library/Caches` on macOS, `~/.cache`
  (or `$XDG_CACHE_HOME`) on Linux. A "no" is remembered for that state, so you are asked again only after
  the next change. No terminal or no answer skips the import and remembers nothing.
- **The store is shared and live.** An import changes the skills of every sandbox, running ones included.
  `--no-skills` skips the import and the question for one start.
- **It replaces.** A skill of the same name is replaced completely, including one added with
  `sbx skills add`. A skill you deleted on the host is not documented to be removed from the store.
- **It never stops a start.** `sbx skills import` is marked experimental; a failure is a warning that shows
  sbx's output. `--dry-run` shows the command and runs nothing.

### GitHub App tokens

With a `github:` block, `sbxup` gives the sandbox a GitHub token that lasts an hour instead of a
personal token you set by hand. The token belongs to a GitHub App, whose private key is kept in
Bitwarden and never enters the sandbox.

```yaml
github:
  bitwarden-item: github-deneblab-agent   # the Bitwarden item holding the app
  # optional, with their defaults:
  # key-attachment: <the item's only .pem attachment>
  # client-id-field: client-id            # JWT issuer; falls back to the 'app-id' field
  # repositories: [docs, deneblab/abcversion]   # added to the repo in 'origin'; same owner only
  # permissions: {contents: write, pull_requests: write, issues: write}
```

The Bitwarden item has a `client-id` (or `app-id`) field and the app's private key as a `.pem`
attachment. You need the Bitwarden CLI, `bw`, logged in once (`bw login`).

What a start looks like:

```
> sbxup
GitHub token: deneblab/sbx-templates from Bitwarden "github-deneblab-agent"
  permissions: contents:write, issues:write, pull_requests:write
  Allow this project to use it? [y/N] y                          # first time only
Bitwarden is locked. Master password (Enter = no GitHub token this time):
GitHub token: ready (expires 14:32)
...
GitHub token: revoked                                            # after you leave Claude
```

- **The password is asked at every start**, unless `BW_SESSION` is set in the terminal
  (`$env:BW_SESSION = bw unlock --raw` in PowerShell, `export BW_SESSION=$(bw unlock --raw)` in bash).
  **Enter on an empty password starts without a token.** When sbxup unlocks the vault, it locks it again
  once the key is read.
- **The token covers the repository in `origin`** plus `repositories`, all of the same owner, with
  `permissions`. The first use of each combination asks for your approval; it is remembered in your user
  cache (`sbxup/github/`), never in the repository, and a change asks again. "No" is not remembered.
- **It is renewed while `sbx run` lasts** (about every 45 minutes) with `sbx secret set github`, and
  **revoked when the session ends**, so a sandbox left running keeps no valid token.
- **It never stops a start.** A missing `bw`, a wrong password, an app not installed on the repository or a
  refused permission is a warning, and the sandbox starts without a token. `--dry-run` shows what would be
  asked for and calls neither Bitwarden nor GitHub.
- `--no-gh-token` starts without a token. `sbxup --gh-token` mints one and sets it in the sandbox that is
  already running, e.g. from a second terminal; that token is not renewed and simply expires.

## Run without Docker Hub

Every push that touches `src/` publishes a **`templates-v{version}` GitHub Release** carrying the
Dockerfiles themselves — a `manifest.json` catalogue and a `templates-{version}.tar.gz` of the whole
`src/` tree, each with a `.sha256`. `sbxup` builds from those directly, so no image is ever pulled
from a registry:

```bash
sbxup --init          # lists the templates in the latest release, you pick one
sbxup                 # builds it locally the first time, then reuses the image
```

`--init` writes the template name and every other key as a commented example, so the file shows
what can be set:

```yaml
template: dotnet10   # built locally from src/sbx-claude-dotnet10/Dockerfile in the release tarball

# version: 0.2.8     # pin the release to freeze the environment; without it sbxup follows the latest
# agent: claude      # agent passed to 'sbx run' (default: claude)
# clone: true        # run on a private in-container git clone (default: off)
# cache: .sbx-cache  # project directory mounted into the sandbox for package caches

# Extra host directories mounted into the sandbox; append :ro for read-only.
# ...
# mounts:
#   - ../shared-libs:ro

# Short-lived GitHub App token for the repo in 'origin', renewed while 'sbx run' lasts.
# ...
# github:
#   bitwarden-item: github-deneblab-agent  # item with a .pem attachment and a client-id field
#   ...
```

**Only `template` is live**, which is the point: you name a template, not a release tag, and no
default is written down, so a default that changes in a later sbxup still reaches your config.
Uncomment a block to use it. `--init` never overwrites an existing config; `sbxup --init --dry-run`
prints the full template so you can copy what you need.

### Choosing a release

`version:` takes `<owner>/<repo>@<version|tag|latest>`, and the short forms mean the default
repository:

```yaml
version: deneblab/sbx-templates@latest         # newest release, re-checked periodically
version: deneblab/sbx-templates@0.1.4          # frozen
version: 0.1.4                                 # same, default repository
version: templates-v0.1.4                      # same, full tag
version: latest                                # same as leaving the key out
```

`version` pins the **release** (`templates-v…`), not the version of the image built from it. The two
are independent counters — a release is cut on every change under `src/`, while each template's own
version moves only when its directory changes — so `version: 0.2.8` can build an image `0.2.5`. The
line printed at start-up shows both: `Template: dotnet10 0.2.5 (templates-v0.2.8)`. The same key
selects a fork, and the deprecated `build.release` is still read as `version`.

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
sbxup --template dotnet10           # use (and build, if missing) a template without editing the config
sbxup --update                      # check for a newer template version and build it, without asking
sbxup --rebuild                     # rebuild even though the image exists
sbxup --update-claude               # rebuild only the Claude Code layer
sbxup --refresh                     # re-check for a newer release, re-download its assets
sbxup --init --template dotnet10    # non-interactive; no prompt
```

### When a newer version is published

A new `templates-v…` release does not always mean a new image: each template's version moves only when
its own directory changes. When it does — say `dotnet10` goes from `0.2.8` to `0.2.9` — `sbxup` **never
builds it on its own**. With an older version already built, it keeps running that one and asks:

```
New version of template dotnet10 is available: 0.2.9 (using 0.2.8)
  https://github.com/deneblab/sbx-templates/releases/tag/templates-v0.4.12
Building takes a few minutes and needs Docker. Update? [y/N]
```

Only `y` builds it; a bare Enter declines. A "no" is remembered until the next release check (the
180 hours above, or `--refresh`), so it does not nag on every start. `sbxup --update` re-checks and
builds without asking. Without a terminal (a script, CI) it never asks and never builds — it runs the
installed version and prints a line pointing at `--update`. It also does not ask when Docker is not
running, since the answer could not be acted on. A first start, with nothing built yet, builds
without asking, and so does a `version:` pin: that is the release you asked for.

Built images and this decision are per user, not per project, so a "yes" in one project makes the new
version available to every other project's *new* sandboxes. An existing sandbox keeps the image it was
created with — `sbxup` resumes it by name — so to move it to the new version, remove it with
`sbx rm <name>` (this deletes the sandbox's own state, not your project files) and run `sbxup` again.
To keep a project on a version regardless, pin it with `version:`.

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
`--template`, it writes `template: dotnet10`, which is valid without any network.

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

**Use the locally built image** — `sbxup` does not accept an image reference (`template` is a template
name, and `sbxup` builds its own tag from the release), so run an image built by `task build:*` with
`sbx` directly, as in [Option B](#option-b--direct-sbx-command):

```bash
sbx run --template docker.io/pkudrel/sbx-claude-dotnet10:latest claude
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
