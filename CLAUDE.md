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
sandbox: app_claude  # optional: sandbox name (default: <parent>_<folder>_<agent>)
clone: false         # optional: true => run on a private in-container git clone
cache: .sbx-cache    # optional: mount local cache dir into sandbox
hide: [dev]          # optional: covered in the sandbox, agent loses sudo (default: dev, when it exists)
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
  defaults off, and in what `--init` writes only `template:` is live: every other key is a *comment*. The
  example pin uses the release version, not the image's: `version` pins the release (`templates-v...`), and
  the two counters are independent, so `version: 0.2.8` can build an image `0.2.5` (the start-up line
  prints both). A fork gets a live `version:` and no commented pin, since uncommenting it would make two.
- **The commented keys come from `configDocs` (`configdoc.go`)**, one entry per key with its example and
  comment; `renderConfig` writes `template`/`version` itself. `TestConfigDocsCoverEveryKey` reflects over
  `Config` and `GitHubConfig` and fails when a field has no entry there or is missing from `usage`, and
  `TestConfigDocsUncommentToAValidConfig` loads every block with its `# ` removed. Adding a config key
  therefore means adding it in three places, and the test names the one forgotten.
- **`--init` needs no network to be valid.** Its fallback body is `renderConfig("template: dotnet10", …)`,
  because a template is just a name resolved on first run. It never overwrites an existing file; the
  warning points to `--init --dry-run`, which prints the template anyway.
- **`--build` is still parsed** (and ignored) so an old script does not leak it through to `sbx run`.
- **Older sbxup cannot read the new shape.** It would try to pull an image called `dotnet10`; mention it
  in the release notes when this ships.

`.sbx/sbxup.config.yaml` is the **only** path read — there is no search order, so there is never a question of which of several files won. `legacyConfigPaths` in `config.go` lists the previously supported names; they are probed only to turn "config not found" into a rename instruction, never loaded.

When `clone: true` (or `--clone`), `sbxup` passes `--clone` to `sbx run` so the agent works on a private in-container git clone of the host repo. Default is off; `--no-clone` forces it off. The removed `branch` key now warns with a hint to rename it to `clone`.

When `cache` is set, the directory is created at the project root (if missing) and mounted as an additional workspace. If not set, no cache mounting occurs.

### The sandbox name is sbxup's (`sandboxName` in `sbx.go`)

`<parent>_<folder>_<agent>`, e.g. `deneblab_sbx-templates_claude`, through `joinSandboxName` (in each part,
characters other than letters, digits and `-` become `-`, so `_` only ever separates); a project at a
filesystem or drive root gets `<folder>_<agent>`. `sandbox:` replaces it verbatim. Things worth keeping in mind:

- **`_`, not `.` and not `/`** (SBXT-012). `sbx create --name` was documented as taking letters, digits, `-`
  and `.`, so the separator used to be `.`; but the scope of an approval rule takes only letters, digits,
  `-` and `_`, and `sbx policy approval respond` failed for every sbxup sandbox (`invalid approval policy
  scope: condition values[0] "antipiracy.simplescraper.claude" must contain only alphanumeric characters,
  hyphens, and underscores`). **Not yet verified:** that `sbx create --name` accepts `_`.
- **Dotted sandboxes are resumed, with a note.** `foldName` drops `.` and `_`, so the sandbox an older sbxup
  named `a.b.claude` is found and resumed under its own name (its `restrict/` and `hide/` records are keyed
  by it, so they still match). `run()` prints that approvals cannot be applied to it and names `sbx rm`.
- **sbxup always names the sandbox**, so every new one is created in two steps (`sbx create --name`, then
  `sbx run --name`, see "Repository restrictions"); `sbx run` never picks a name. One name serves the run,
  `--stop`, `--exec`, `--status`, `--gh-token` and `keepFresh`; `resolveSandboxName` only finds sbx's spelling.
- **No migration, on purpose.** The old `<agent>-<folder>` is not looked for: it folds differently
  (`claudesbxtemplates` vs `deneblabsbxtemplatesclaude`), so the first start after the update creates a new
  sandbox and the old one waits for `sbx rm`. Say so in the release notes.
- **`sandbox:` is validated, not rewritten** (`validSandboxName`): the user typed it, so `x/y` or `x.y` is an
  error at load time rather than a sandbox called `x-y`.

### `mounts:` — extra host directories (`mounts.go`)

A list of `path` or `path:ro`, the syntax `sbx run` already takes for an extra workspace; `buildCreateArgs`
receives the cache and the mounts as one `workspaces` list and always emits `.` first, even when the list is
empty: unlike `sbx run`, `sbx create` without a path makes a sandbox with **no workspace at all** (seen on a
Windows host: the agent got an empty directory), and with extra directories the first would become the primary
workspace. Things worth keeping in mind:

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

### Skills come from `sbx skills import` (`skills.go`)

sbx mounts a shared `agent-skills` store (read-only, live) into every sandbox and fills it only when asked.
`importSkills` runs `sbx skills import --force` before the template is ensured. It is not a config key, on
purpose: a repository config must not decide what is copied from the user's home. Things worth keeping in mind:

- **New sandbox: import, no question. Resume: ask, only if the sources changed.** "Changed" is
  `skillsChecksum` against `sbxup/skills-import.json` in the user cache (one record per user, since the
  store is global). The record holds the checksum of the last import *or* the last "no", so a declined state
  is not asked about twice. No terminal, no answer or a failed import records nothing; a missing record on
  resume counts as changed.
- **The checksum follows the importer's rules** over the same six `skillSources`: files inside each
  top-level skill folder (a top-level symlink to a directory counts), not loose top-level files. Keep
  `skillSources` in step with `sbx skills import --help`.
- **The checksum is not stored in `~/.claude/skills`**: it covers six directories, a file there would
  change what it hashes, and Claude Code on the host would see it.
- **`--force` is required**, not a convenience: without it sbx prompts before replacing each installed
  skill. A re-import replaces a skill completely, including a same-named one from `sbx skills add`.
- **A failure is a warning.** The command is `EXPERIMENTAL` in sbx. Its output is shown only on failure.
- **The seam is `sbxSkillsImport`**, a variable like `sbxList`, so tests never run `sbx`.
- **Deliberately not done:** copying files into `agent-skills` ourselves. Only its Windows path is confirmed,
  sbx may keep a registry that a manual copy bypasses, and sbxup would need a manifest to avoid deleting what
  sbx put there. SBXT-001's per-project `skills:` key (`--skills=off|readonly`) was superseded by this.

### GitHub App tokens (`ghtoken.go`, `githubapp.go`, `bitwarden.go`)

A `github:` block with `bitwarden-item` makes sbxup mint a one-hour GitHub App installation token and hand
it to the sandbox with `sbx secret set github --sandbox <name>`, which takes effect in a running sandbox.
(With `client-id` instead, the token comes from the user's own login; see the next section.) Things worth
keeping in mind:

- **The key never enters the sandbox.** `bw` reads the item's `client-id` field and `.pem` attachment on
  the host, the key stays in sbxup's memory for the session, and only the token is set. There is no key or
  token cache on purpose: a cached key would be a second copy of the secret outside Bitwarden.
- **The password reaches `bw` only through a child's environment** (`bw unlock --passwordenv`), never as an
  argument, and is read with echo off (`golang.org/x/term`; the dependencies are it, `golang.org/x/sys` for
  `lockFile`, and `yaml.v3`). An empty password is a deliberate skip (`errNoToken`), not a failure. With
  `BW_SESSION` set nothing is asked.
  When sbxup unlocked the vault it runs `bw lock` after reading. Every `bw` call gets `--nointeraction`.
- **The config comes from the repository, so the request is approved once**, as with `mounts:`. The key in
  `sbxup/github/<key>.json` covers project, item, owner, repositories and permissions, so naming another
  item or widening anything asks again. "No" and no terminal skip, and nothing is remembered.
- **One owner.** One installation mints one token and the sandbox holds one `github` secret, so
  `repositories` must share the owner of `origin`; another owner is an error naming the entry.
- **Unknown keys under `github:` are errors**, unlike top-level keys: a misspelt `permisions` would
  otherwise mint a token with the defaults.
- **The session outlives `sbx run`.** `run()` starts `keepFresh` beside `runSbx` and, after it returns,
  cancels it, waits for it and revokes the last token. A refresh mints 15 minutes before expiry, sets the
  new token and revokes the old one. A new sandbox is created first (`sbx create --name`, see
  "Repository restrictions"), so the token is set before the agent starts.
- **Ctrl+C.** `runSbx` catches `os.Interrupt` with `signal.Notify` while the child runs, so sbxup lives to
  revoke the token. Not `signal.Ignore`: an ignored signal is inherited by the child.
- **`sbx secret set` needs `-f`** (`sbxSecretSetArgs`): without it sbx refuses to replace an existing
  secret, so every refresh, every resumed sandbox and `--gh-token` would fail. Found on the host; a new
  sandbox has no secret yet, which is why the first test passed without it. `--gh-token` revokes the token
  it minted when the set fails.
- **Every failure is a warning**, and the sandbox starts without a token. `--dry-run` calls neither `bw`
  nor GitHub. `--gh-token` is a standalone command for a running sandbox; its token is not renewed.
- **Seams:** `bwRun`, `readPassword`, `gitOriginURL`, `sbxSecretSet`, `askGitHub`, `sleepCtx`,
  `githubAPIBase` and `runSbx` are variables, so tests never run `bw`, `git`, `sbx` or reach GitHub.
- **Verified on Windows** (2026-10-09, `bw` 2026.9.0):
  - `bw get attachment … --raw` prints the key to stdout;
  - the installation token works through the proxy for `gh api` and `git push`;
  - Ctrl+C exits Claude normally and sbxup then revokes the token;
  - a resumed sandbox gets the new token without warnings (`-f`).

  `bw` sees fields added in the app only after `bw sync`. `sbx secret set` also works for a sandbox that
  does not exist yet. The poll of `sbx ls` that `keepFresh` once had for a sandbox sbx named is gone:
  sbxup names every sandbox now (SBXT-010).

### Your own login: `github: client-id` (`userauth.go`)

The second mode of the `github:` block. `sbxup --gh-login` runs GitHub's device flow once per machine
(a code to enter on github.com); every start then costs one refresh call, with no Bitwarden, no password
and no private key on the host. It exists because the Bitwarden mode asked for the master password on
every start and ran `bw` five times. Things worth keeping in mind:

- **The two modes exclude each other.** `client-id` with `bitwarden-item` is an error, and so is
  `client-id` with any key only the Bitwarden mode reads (`repositories`, `permissions`, ...).
- **No client secret, anywhere.** GitHub requires none for the device flow, nor to refresh a token the
  device flow issued (checked in GitHub's docs, 2026-10-14). The config holds only the public client ID.
- **The token acts as the user**, limited to the app's permissions and the user's own rights, in every
  organization where the app is installed; the one-owner rule of the Bitwarden mode does not apply. It
  cannot be narrowed to repositories, so different permission sets mean different apps (a repository
  switching apps is a new approval, keyed by `client-id`). With `contents: read` the agent cannot push a
  branch, so it cannot open a PR from its own work; `.github/workflows/` needs the `workflows` permission.
- **One token, shared by every sandbox of the app.** A refresh ends the old access token too, so a start
  reuses the stored token while it has more than `refreshBefore` left, refreshes only under `lockFile`
  (flock / LockFileEx; the OS drops it when a process dies) and re-reads the file after taking the lock.
  `keepFresh` looks at the file every `userPollEvery` and sets a token another sbxup refreshed.
- **Never revoked on exit**: other sandboxes may be using it, and the revoke API is for the app owner.
  `--gh-logout` deletes the local file and points to github.com/settings/apps/authorizations.
- **The login is a file**, `os.UserConfigDir()/sbxup/github-auth/<client-id>.json`, mode `0600` (as Claude
  Code and `gh` without a keyring), in the config directory so clearing caches does not log the user out.
  The client ID becomes the file name, so `clientIDPattern` validates it. Access tokens last 8 h, refresh
  tokens 6 months; a dead login is a warning naming `--gh-login`, and the sandbox starts without a token.
- **Seams:** `githubLoginBase` (github.com OAuth endpoints), `userPollEvery`, plus the existing
  `githubAPIBase`, `sleepCtx` and `sbxSecretSet`.
- **Not yet verified on a host**: a real app with device flow enabled, two sandboxes at once, `gh api` and
  `git push` with a `ghu_` token through the sbx proxy.

### Repository restrictions: `restrict:` (`restrict.go`)

`restrict: {no-push: true, no-delete: true}` adds HTTP deny rules to the sandbox's own sbx policy
(`sbx policy deny network --sandbox N HOST --method M --path P`), so the sandbox proxy refuses a git push
over HTTPS, the GitHub API writes that create commits (contents, git/*, merges), or a DELETE under
`api.github.com/repos/**`. Checked on a host with sbx 0.48 (Windows, 2026-10-14). Things worth keeping in mind:

- **Guardrails, not a boundary.** GraphQL (`createCommitOnBranch`, which `gh` also needs for reading) and
  SSH stay open. The boundary is the token: "push only to repository X" is an installation token narrowed
  with `github.repositories`, which GitHub enforces whatever the URL.
- **Only wildcard rules.** Path rules match case-sensitively: a rule for `/PKudrel/Fringe2026.git/…` did not
  stop a push to `/pkudrel/fringe2026.git/…`, and GitHub accepts either. `/*/*/git-receive-pack` stopped both
  (and covers `repo.git`). "Push only to X" cannot be written at all: no negation, deny beats allow, and the
  default local policy is `default-allow-all`.
- **No approval, and a failure stops the start.** Deny rules only narrow, so a repository may ask for them.
  A rule that cannot be applied is an error (sandbox created but not started), unlike the GitHub token,
  where going without is the safe direction. Unknown keys and non-boolean values are errors.
- **Create, then run.** Every new sandbox is made with `sbx create --name <sandboxName(…)> …`, then rules
  and the token are set, then `sbx run --name`. `splitRunArgs` sends `--detach`, `--detach-keys`, `--new`,
  `--rm` and everything after `--` to `run`, the rest to `create`. (Until SBXT-010 this happened only with
  `restrict:` or `github:`; otherwise a single `sbx run` let sbx pick the name.)
- **Reconciled on every start, owned rules only.** `sbx policy ls N --json` (`{"rules": [...]}`; HTTP rules
  have `resource_type: http`, `methods`, `http_targets[{host,path}]`, `scope: sandbox:N`) gives what is in
  place; adding an existing rule is a no-op anyway ("Already covered"). `sbxup/restrict/<sandbox>.json` in the
  user cache records the rules sbxup added, so dropping a key removes only those, never a rule the user had.
  `sbx policy rm` needs `-f` (0.48 confirmations need a terminal). Nothing configured and nothing recorded
  costs no sbx call.
- **The agent is told.** git hides the proxy's reason and shows only `RPC failed; HTTP 403` (REST shows
  `Blocked by local rule for …`). A block between `<!-- sbxup:restrict -->` markers is written into
  `/home/agent/.claude/CLAUDE.md` with `sbx exec`; Claude reads it as user instructions. It is **replaced,
  not appended** (appending twice duplicated it on the host), and written only when its text changes.
- **Seams:** `sbxPolicyDeny`, `sbxPolicyRm`, `sbxExecStdin`, plus `sbxOutput` for `policy ls`.

### Hidden directories: `hide:` (`hide.go`)

`hide: [dev]` keeps project directories (typically git-ignored, filled by hand) out of the agent's reach
while it works live on the rest of the repository. `sbx` mounts a workspace only whole, so each path is
covered by `sbx mount <sandbox> <cover>:<target>:ro` and the agent loses sudo. Design and host evidence:
SBXT-011, GitHub #2. Things worth keeping in mind:

- **The cover is a boundary only without sudo.** On a Windows host (sbx 0.48) `sudo umount` removed it and
  the agent wrote into the host's real `dev`. `dropSudo` runs `sudo -n rm -f /etc/sudoers.d/agent` and checks
  that `sudo -n true` then fails; the passwords of `agent` and `root` are locked, so `%sudo` and `su` lead
  nowhere. **A template with a Docker daemon inside would reopen it** (the `docker` group is root): none of
  ours has one; keep it that way or drop `hide:` support for that template.
- **Default `dev`.** No key hides `dev` when it exists (`hideEntries`); `hide: []` is off. So `Config.Hide`
  is a `*[]string`: nil (absent) and empty (off) mean different things.
- **One cover per sandbox and target**, `os.UserConfigDir()/sbxup/hide/<sandbox>/<sha256(target)[:12]>/`
  holding only `HIDDEN.txt`, because `sbx umount` revokes the source for *every* mount of it. Config dir,
  not cache: sbx refuses to start a sandbox whose saved mount lost its source, and `ensureCover` recreates
  every cover before each start. The record `…/hide/<sandbox>.json` sits beside them (mounts made,
  `sudoDropped`, notice).
- **Order.** New sandbox: `sbx create` → restrictions → `applyHide` (`sbx exec <s> true` to start it, since
  `sbx mount` needs a running sandbox and there is no `sbx start`; mount; drop sudo; notice) → token →
  `sbx run`. A mount that hits "not running" is retried once after another start (seen on a host).
  Resume: only the difference to the record (`sbx umount` works on a stopped sandbox, no start needed).
- **Every path to a hidden directory is covered, and checked on every start** (`coverEverywhere`). On a host
  the first dynamic `sbx mount` brought `/mnt/host` (virtiofs, source `host`, every allowlisted host path, the
  workspace included), and `/mnt/host/w/…/dev` was readable while `/w/…/dev` was covered. So after the first
  cover sbxup reads `findmnt -J` in the sandbox, maps the hidden path under every virtiofs mount whose source
  (`host` or `host[/x]`) contains it (`aliasesOf`), covers each, repeats until nothing is new, and then requires
  `ls -A` to show exactly `HIDDEN.txt` on every path. Anything it cannot list or check stops the start. This
  costs a start plus a few `sbx exec` on every resume of a sandbox with something hidden. Extra covers are
  recorded with `of` = the configured target, so dropping an entry removes them too. `sbx umount` of a mount of
  the hidden host path does **not** revoke access through the workspace (checked on a host).
- **Sudo does not come back** when entries are removed; the message names `sbx rm`.
- **Target path:** `sandboxPath` — the host path, on Windows `W:\x` → `/w/x` (what `sbx create` prints);
  UNC is an error. `sbx mount` creates a missing target, in the sandbox and empty on the host.
- **Clone mode is refused** with `hide:`: the host repo stays readable at `/run/sandbox/source` (ro);
  covering there is not verified.
- **The agent is told** in a `<!-- sbxup:hide -->` block (`blockScript`, shared with `restrict:`).
- **Seams:** `sbxStart`, `sbxMount`, `sbxUmount`, `dropSudo`, `sbxExecStdin`, `sbxFindmnt`, `sbxListDir`;
  `TestMain` makes the first four fail, the last two go through `sbxOutput`. The test fake reproduces the
  host's mount table (workspace, then `/mnt/host` after the first mount).
- **Verified on a Windows host** (2026-10-14, sbxup 0.2.22, a new sandbox with real data in `dev`): two covers,
  `/w/…/dev` and `/mnt/host/w/…/dev`, both ro, `HIDDEN.txt` on both; `touch` → read-only, `umount` → must be
  superuser, `sudo -n true` → password required, git works, the host's `dev` untouched. Case variants (`DEV`,
  `robenova` under `/mnt/host`) land on the cover. Both covers come back after `sbx stop` + start. On a resume,
  adding an entry made two more covers, and removing it took away exactly those two and their directories while
  the `dev` covers kept working. **Linux (WSL, repo on the WSL disk)** behaves the same: `/mnt/host` appears
  there too and gets its cover from `~/.config/sbxup/hide/`, case variants do not exist, and both covers come
  back after a restart. **Not yet verified:** macOS.

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

### Reading `sbx` output (sbx 0.48+)

sbx 0.48 changed the human-readable output of every command (headings, status marks such as
`● running`, a `CREATED` column) and moved errors, warnings and hints to stderr. sbxup therefore:

- **Reads stdout only**, through `sbxOutput` (a seam; `TestMain` makes it fail so no test runs a real
  `sbx`). The interactive `runSbx` and the commands whose output is only shown on failure
  (`sbx skills import`, `sbx secret set`) are unchanged.
- **Asks for JSON first and keeps the table as the fallback** for an sbx without `--json`.
  `sbx ls --json` is documented (`.[].name`; before 0.48 `.sandboxes[].name`), so `sbxNames` reads both
  shapes. `sbx list` is tried only when `sbx ls` fails.
- **`sbx template ls --json` has undocumented fields.** `templateRefsJSON` accepts the array (0.48) or
  `{"images": [...]}` and, per image, repository + tag, a `name:tag` reference or a list of them, in any
  casing. An image list in which none is found is an error, not "nothing built": sbxup warns once and
  reads the table, because "nothing built" would rebuild the template on every start. When the real
  field names are known from a host, pin them here and narrow the parser.

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
