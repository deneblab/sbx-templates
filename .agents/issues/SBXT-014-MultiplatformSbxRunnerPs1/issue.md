---
issueId: "SBXT-014-MultiplatformSbxRunnerPs1"
humanTitle: "Multiplatform sbx-runner.ps1"
issueUrl: ""
createdAt: "2026-08-08T13:18:44Z"
tags: [sbx-runner, shell, multiplatform, tooling]
---

# Multiplatform sbx-runner.ps1

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user‑agent communication and planning)

Multiplatform sbx-runner.ps1

Right now sbx-runner.ps1 is write in powershell.
I think i need multiplaform version:
- windows, linux
- easy to install

Make propositions


## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*

- Goal: Make `sbx-runner` usable on Windows **and** Linux (and macOS for free), installed and updated from GitHub with a one-liner rather than manual profile editing.
- Status: **decided** (2026-08-09) — see "Decisions" below. Ready for implementation planning.

### Current state (analysis of `shells/sbx-runner.ps1`)

- ~276 lines, shipped as a **dot-sourced PowerShell function** (`$PROFILE.CurrentUserAllHosts`), documented in `shells/README.md`.
- What it actually does is thin: read 4 YAML keys (`template`, `agent`, `clone`, `cache`), warn on unknown/removed keys, resolve the sandbox name via `sbx list`, then shell out to `sbx run|stop|exec`. No PowerShell-only capability is required.
- It never mutates the parent shell (no `cd`, no env export) — so **it does not need to be a sourced function at all**; an executable on `PATH` would work and is far easier to install.
- Genuinely Windows-bound bits to fix in any port:
  - hardcoded backslash config path `.agents\sbx-runner.yaml` (lines 83, 88, 94, 153, 173, 191, 235)
  - the manual `--flag` reparse block (lines 41–64) — a workaround for PowerShell not mapping `--flag` to `-Flag`; unnecessary in a POSIX getopt loop
  - `Get-Item .`, `New-Item`, `Write-Warning`/`Write-Error`, `Set-Content -Encoding UTF8`
- Repo precedent: this project **already ships dual `.sh` + `.ps1` implementations** (`scripts/version/version.{sh,ps1}`, `scripts/build/build-push.{sh,ps1}`) with `Taskfile.yml` dispatching by OS, and a bash test file at `scripts/version/tests/version.sh.test.sh`.

### Decisions (user, 2026-08-09)

Driving priority stated by the user: **easy install/update from GitHub.**

| # | Decision | Choice |
|---|----------|--------|
| 1 | Implementation | **C — single Go binary.** *(revised 2026-08-09; superseded choice A)* One codebase, cross-compiled per OS/arch. |
| 2 | Distribution source | **GitHub Releases — tagged, versioned assets.** Installer pulls a pinned/latest release rather than raw `main`. |
| 3 | Update path | **Both** — re-running the install one-liner is idempotent *and* a `--self-update` subcommand re-fetches and replaces in place. |
| 4 | Install model | **Executable on `PATH`, both platforms.** Replaces the dot-sourced-function model on Windows. |
| 5 | Name | **`sbxup`** *(2026-08-09)* — the tool is renamed from `sbx-runner`. Binary, release assets, install path and command all become `sbxup`. |
| 6 | Versioning | **[AbcVersion](https://github.com/deneblab/AbcVersion)** *(2026-08-09)* — replaces `scripts/version/version.sh` for this artifact. |

Rejected: **A** (bash + `.ps1`) — initially chosen, then reversed by the user; two implementations to keep behaviourally in sync was the cost that decided against it. **B** (single pwsh 7 script) — forces a PowerShell install on Linux, contradicting the install-ease priority.

Decision 1 was reversed after decisions 2–4 were already fixed. Those three carry over unchanged and in fact fit Go **better** than they fit A: Releases are mandatory for binary distribution anyway, a single binary makes `--self-update` a straightforward self-replacement, and a `PATH` executable is the only sane model for a compiled artifact. The reversal therefore costs no prior decision.

Consequences worth carrying into the plan:

- **Cross-compile + release matrix is now required, not optional** — this was C's stated downside and is accepted. Six targets: linux/darwin/windows × amd64/arm64.
- The repo gains its **first Go module**; today it contains zero `.go` files and no `go.mod`. Toolchain verified present (go1.26.0) and `proxy.golang.org` reachable.
- **`shells/sbx-runner.ps1` becomes redundant.** It is kept in place and marked deprecated rather than deleted, so existing Windows users are not broken before they install the binary. Removal is a follow-up.
- Release infrastructure must still be built first — the repo has **zero git tags** today and only one workflow (`build-push.yml`, Docker images).
- Because the runner is a `PATH` executable, **the login shell no longer matters** for running it. Shell choice only affects the installer's one-time `PATH` wiring.

### Assumptions (agent defaults; flag if wrong)

- **YAML parsing**: `gopkg.in/yaml.v3` — a real parser, which was part of the rationale for choosing Go. One well-established dependency; correct handling of quoting and comments that the `grep`/`sed` approach only approximates.
- **Shells wired by the installer**: bash and zsh (`PATH` block, marker-guarded); other shells work but are not auto-wired.
- **macOS**: built and shipped for both architectures; not separately tested.
- **`sbx` itself is not vendored** — the binary shells out to `sbx` exactly as the `.ps1` does today.

### Versioning via AbcVersion (decision 6)

`abcversion` is a deneblab CLI that derives semver from git history: `BaseVersion` in `.abcversion.json`
plus a first-parent commit count, optionally scoped per project path. Read from the repo (`README.md`):

- `abcversion -p semversion` prints the version; `--project <name>` selects a configured project.
- Multi-project config scopes counting to a path — so a `sbxup` project with `"Path": "cmd/sbxup"`
  bumps only when runner source changes, exactly the property `version.sh --path` was giving us.
- CI installs it as a **native binary** (`curl` one asset, no .NET SDK step), per the README's
  GitHub Actions recipe.

Scope of the switch: **`sbxup` only.** Docker image builds keep `scripts/version/version.sh` and
`version.yaml` untouched, so the two systems coexist rather than one migration blocking the other.
Migrating the image pipeline to AbcVersion is a sensible follow-up, not part of this issue.

AbcVersion also supplies the **house pattern for the installer and release workflow** — its
`install.sh` (POSIX sh, whole body in `main()` called on the last line so a truncated download cannot
execute, per-asset `.sha256` sidecars, verify-then-`mv`, `VERSION`/`INSTALL_DIR`/`BASE_URL` env
overrides) and its `github-release.yml` (staging dir, per-asset checksum, `softprops/action-gh-release`)
are the templates to mirror, so deneblab tools install the same way.

- Scope: a Go module (`go.mod`, `cmd/sbxup/`) implementing the full CLI including `--version` and
  `--self-update`; `install.sh` + `install.ps1` with OS/arch detection modelled on AbcVersion's;
  `.abcversion.json` defining the `sbxup` project; a release workflow that cross-compiles with
  `GOOS`/`GOARCH`, publishes checksums and tags `sbxup-v{version}`; Go unit tests; deprecation of
  `shells/sbx-runner.ps1`; doc updates in `shells/README.md`, `README.md`, `CLAUDE.md`, `Taskfile.yml`.
- Constraints: `sbx-runner.yaml` schema (`template`, `agent`, `clone`, `cache`) and the CLI surface (`--init`, `--clone`, `--no-clone`, `--exec`, `--status`, `--stop`, `--dry-run`, `--help`) must stay identical to the current PowerShell behaviour; the `branch`-key deprecation warning must be preserved; no change to `sbx` invocation semantics.
- Success criteria:
  1. `sbxup --dry-run` emits the same command line the `.ps1` does for the same config, enforced by unit tests.
  2. `--init`, `--status`, `--stop`, `--exec`, `--clone`/`--no-clone` all behave as they do today.
  3. Install is a single documented command per platform, fetching a tagged release asset; re-running it upgrades in place.
  4. `sbxup --self-update` upgrades an installed binary from GitHub, checksum-verified, on both platforms.
  5. A tagged release (`sbxup-v{version}`, version from `abcversion`) carries every published binary plus its `.sha256`.
  6. `shells/README.md`, `README.md`, and `CLAUDE.md` document the new install path and no longer present dot-sourcing as the install method.

### Naming consequences (decision 5)

- Command, binary, release assets and install path all become `sbxup`; `cmd/sbxup/` is the Go package.
- **Config file**: `sbxup` keeps reading `.agents/sbx-runner.yaml` for backward compatibility, but
  prefers `.agents/sbxup.yaml` when present. Search order: `.agents/sbxup.yaml` → `.agents/sbx-runner.yaml`
  → `sbxup.yaml` → `sbx-runner.yaml`. `--init` writes the new name. This repo's own
  `.agents/sbx-runner.yaml` therefore keeps working untouched.
- The YAML *schema* (`template`, `agent`, `clone`, `cache`) is unchanged, so no user config edits are forced.

# ChangeLog
- 2026-08-08 — Issue created
- 2026-08-08 — Analyzed shells/sbx-runner.ps1 and repo conventions; added Agent Summary with three propositions (A: bash port, B: pwsh everywhere, C: Go binary), distribution/install proposal, and open questions
- 2026-08-09 — User set priority "easy install/update from github"; recorded decisions (A bash+ps1, GitHub Releases with tags, both re-run-installer and --self-update, PATH executable on both platforms), replaced propositions with Decisions/Assumptions, noted release-infra gap (zero tags today) and the tag/version-scheme question, expanded scope and success criteria
- 2026-08-09 — Triggered scenario via /issue.
- 2026-08-09 — Created plan.md + state.json; verified version path-scoping with scripts/version/version.sh
- 2026-08-09 — User reversed decision 1: Go binary (C) instead of bash+ps1 (A); decisions 2-4 carry over unchanged. Verified go1.26.0 present and proxy.golang.org reachable
- 2026-08-09 — User renamed the tool to `sbxup` (decision 5) and chose AbcVersion for versioning (decision 6); read deneblab/AbcVersion docs, adopted its installer + release-workflow pattern; replanned
- 2026-08-09 — Implemented all 14 components: Go module + cmd/sbxup (CLI, config, sbx wrapper, self-update, 20 tests), .abcversion.json, install.sh/install.ps1, release-sbxup.yml, and doc/Taskfile updates. Verified: tests/vet/gofmt clean, 6 cross-compile targets, dry-run parity with the .ps1, legacy config still read, installer happy-path + tamper-refusal against a file:// release
