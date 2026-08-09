# Plan — SBXT-014-MultiplatformSbxRunnerPs1

Replace the Windows-only, dot-sourced `shells/sbx-runner.ps1` with **`sbxup`**: a single Go binary,
cross-compiled for Windows/Linux/macOS, distributed as tagged GitHub Release assets, installed onto
`PATH` by a one-liner and updated by either re-running that one-liner or `sbxup --self-update`.
Versioned with [AbcVersion](https://github.com/deneblab/AbcVersion).

Decisions live in `issue.md` (1: Go binary · 2: GitHub Releases · 3: installer re-run + `--self-update`
· 4: PATH executable · 5: named `sbxup` · 6: AbcVersion).

> Supersedes the earlier bash+`.ps1` plan (decision 1 was reversed by the user). Nothing from that
> plan was built, so the reversal costs no work.

## Design decisions

- **Layout**: root `go.mod` (`module github.com/deneblab/sbx-templates`), all runner code in the single
  package `cmd/sbxup/`. One directory keeps AbcVersion's path-scoped project config trivial and suits
  a ~500-line program; `src/` is left alone because `build-push.*` treats every `src/` subdir as a
  Docker image context.
- **Cross-compile on one runner.** Go needs no per-OS runner: `CGO_ENABLED=0` + `GOOS`/`GOARCH` builds
  all six targets on `ubuntu-latest`. This is strictly simpler than AbcVersion's Native-AOT matrix.
  Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.
- **Version stamping**: `-ldflags "-s -w -X main.version=$(abcversion -p semversion --project sbxup)"`.
  Dev builds report `dev`.
- **YAML**: `gopkg.in/yaml.v3` (proxy reachability verified). Real parsing of quoted values/comments —
  part of the rationale for choosing Go.
- **Config compatibility**: search `.agents/sbxup.yaml` → `.agents/sbx-runner.yaml` → `sbxup.yaml` →
  `sbx-runner.yaml`. Schema unchanged (`template`, `agent`, `clone`, `cache`); the `branch`-key
  deprecation warning is preserved.
- **Behaviour is ported, not redesigned.** `_ResolveSandboxName` via `sbx list` (never guess the name),
  fold-comparison of names, clone precedence `--no-clone` > `--clone` > config, auto-resume when the
  sandbox already exists — all carried over from the `.ps1` verbatim in intent.
- **Installer/release shape mirrors AbcVersion** so deneblab tools install identically: POSIX `sh`,
  whole body inside `main()` invoked on the final line (a truncated download cannot execute), per-asset
  `.sha256` sidecars in `sha256sum` format, verify-then-`mv`, `SBXUP_VERSION` / `SBXUP_INSTALL_DIR` /
  `SBXUP_BASE_URL` overrides.
- **`shells/sbx-runner.ps1` is deprecated, not deleted** — kept so existing Windows users are not
  broken before they install `sbxup`. Removal is a follow-up.

## Components

1. [ ] `go.mod` / `go.sum` — NEW. Module + `gopkg.in/yaml.v3`.
2. [ ] `cmd/sbxup/main.go` — NEW. Flag parsing (`--init --clone --no-clone --exec --status --stop
       --dry-run --version --self-update --help`, `--config/--template/--agent`), dispatch, passthrough args.
3. [ ] `cmd/sbxup/config.go` — NEW. Config discovery (4-name search order), YAML load, unknown-key and
       `branch`-key warnings, truthiness, `--init` writer.
4. [ ] `cmd/sbxup/sbx.go` — NEW. Sandbox-name folding, `sbx list` resolution, command construction for
       run/stop/exec, cache-dir creation, dry-run rendering.
5. [ ] `cmd/sbxup/selfupdate.go` — NEW. Resolve latest release, download asset + `.sha256`, verify,
       atomically replace the running executable (Windows: rename-in-place dance).
6. [ ] `cmd/sbxup/*_test.go` — NEW. Table-driven tests: config search order, key warnings, clone
       precedence, dry-run command construction (the parity criterion), name folding, install-dir logic.
7. [ ] `.abcversion.json` — NEW. `BaseVersion` + `Projects.sbxup.Path = "cmd/sbxup"`.
8. [ ] `install.sh` — NEW (repo root, matching AbcVersion's location). OS/arch detection, checksum
       verification, install to `$HOME/.local/bin`, PATH advice.
9. [ ] `install.ps1` — NEW (repo root). Same flow; `%LOCALAPPDATA%\Programs\sbxup`, idempotent user-PATH update.
10. [ ] `.github/workflows/release-sbxup.yml` — NEW. Trigger: push to `main` touching `cmd/sbxup/**`
        (+ `workflow_dispatch`). Install `abcversion` native binary → compute version → cross-compile 6
        targets → per-asset `.sha256` → release tagged `sbxup-v{version}`.
11. [ ] `Taskfile.yml` — EDIT. Add `sbxup:build`, `sbxup:test`, `sbxup:version`.
12. [ ] `shells/README.md` — EDIT. Mark `sbx-runner.ps1` deprecated; point to `sbxup`.
13. [ ] `README.md` — EDIT. Install one-liners + usage.
14. [ ] `CLAUDE.md` — EDIT. Rewrite "Running a Sandbox"; document the Go module and `sbxup` tasks.

## Integration notes

- `scripts/version/version.sh`, `version.yaml`, `build-push.*` and `build-push.yml` are **untouched** —
  AbcVersion is scoped to `sbxup` only; the Docker pipeline keeps its existing versioning.
- Creation order: 1 → 2 → 3 → 4 → 5 → 6 (test) → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14.
- Config file `.agents/sbx-runner.yaml` in this repo keeps working unchanged.

## Verification

- `go vet ./...` and `go test ./...` pass.
- `go build ./cmd/sbxup` then `./sbxup --dry-run` in this repo prints the same `sbx run …` line the
  `.ps1` produces for `.agents/sbx-runner.yaml`.
- Cross-compilation smoke-checked locally for all six `GOOS`/`GOARCH` pairs.
- `sh -n install.sh` clean; installer's version/dir overrides exercised against a local file:// base URL.
- `abcversion -p semversion --project sbxup` resolves once `.abcversion.json` exists.
- Release workflow: YAML parse + logic review. A real release needs a push to `main`.

## Out of scope / follow-ups

- Publishing the first release (needs a push to `main`; the workflow creates the tag).
- Deleting `shells/sbx-runner.ps1` after users migrate.
- Migrating the Docker image pipeline from `version.sh` to AbcVersion.
- Homebrew/scoop/winget packaging.
