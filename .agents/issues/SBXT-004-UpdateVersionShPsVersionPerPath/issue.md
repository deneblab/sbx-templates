---
issueId: "SBXT-004-UpdateVersionShPsVersionPerPath"
humanTitle: "Update version.sh/ps1 to support per-path commit counting"
issueUrl: ""
createdAt: "2026-04-09T00:00:00Z"
tags: [versioning, build, scripts]
---

# Update version.sh/ps1 to support per-path commit counting

Update `version.sh` and `version.ps1` to support counting commits scoped to a specific directory path, enabling independent version bumps per image when only its files change.

## Detailed Proposal

### New parameter: `--path`

Add an optional `--path <dir>` argument to both `version.sh` and `version.ps1`. When provided, `git rev-list --count` is scoped to only count commits that touch files under that path.

### version.sh changes

Current (global counting):
- With baseCommitSha: `git rev-list --count <sha>..HEAD`
- Without baseCommitSha: `git rev-list --count HEAD`

Proposed (path-scoped when `--path` is set):
- With baseCommitSha: `git rev-list --count <sha>..HEAD -- <path>`
- Without baseCommitSha: `git rev-list --count HEAD -- <path>`
- Without `--path`: unchanged behavior (backward-compatible)

### version.ps1 changes

Add `-Path` parameter (default empty). Same logic — append `-- <path>` to `git rev-list` when set.

### Taskfile / build-push integration

`build-push.sh` and `build-push.ps1` already know the image context dir. They should pass it:
- `version.sh --path src/<image-name>`
- `version.ps1 -Path src/<image-name>`

### Per-image version.yaml

Each `src/<name>/` gets its own `version.yaml` so images can have independent base versions. The `--file` parameter already exists in `version.sh`; `version.ps1` already has `-ConfigFile`.

### Backward compatibility

- No `--path` → counts all commits globally (current behavior)
- No per-image `version.yaml` → falls back to root `version.yaml`
- Existing `task version` / `task build` / `task push` keep working as-is

### Example outcome

Given 10 total commits, 3 touching `src/sbx-claude-dotnet10/`, 1 touching `src/sbx-claude-dotnet10-node/`:
- `version.sh` → 0.1.10 (global)
- `version.sh --path src/sbx-claude-dotnet10` → 0.1.3
- `version.sh --path src/sbx-claude-dotnet10-node` → 0.1.1

### Additional improvements (while touching these scripts)

**1. Dirty-tree indicator (`+dirty` suffix)**
Currently the version is always clean. Add a `+dirty` suffix when there are uncommitted changes in the working tree (or scoped to `--path` if set). Useful for local builds to distinguish from CI-built versions.
- `version.sh`: check `git diff --quiet -- <path>` and `git diff --cached --quiet -- <path>`
- Output: `0.1.3+dirty` when working tree has changes

**2. Pre-release label support**
Add optional `--pre <label>` flag to produce versions like `0.1.3-rc.1` or `0.1.3-dev.5`. Useful for branch builds.
- When `--pre` is set: `VERSION="${MAJOR}.${MINOR}.${FINAL_PATCH}-${PRE_LABEL}.${INC}"`

**3. `version.ps1` semver regex is stricter than `version.sh`**
- `version.ps1` uses `'^(\d+)\.(\d+)(\.(\d+))?$'` (anchored with `$`)
- `version.sh` uses `'^([0-9]+)\.([0-9]+)(\.([0-9]+))?'` (no end anchor)
- These should be consistent. Align both to the same pattern.

**4. `version.sh` output goes to `/dev/stdout` by default**
- `OUTPUT_DEST="${GITHUB_OUTPUT:-/dev/stdout}"` — writing to `/dev/stdout` with `>>` works on Linux but is unusual and can behave oddly in some contexts.
- Cleaner approach: if no `GITHUB_OUTPUT`, just `echo` each line (like `version.ps1` does).

**5. Common `--json` output option**
Add `--json` / `-Json` flag to emit version info as a JSON object instead of `key=value` lines. Makes it easier for scripts/tools to consume without `eval` or manual parsing.

## Agent Summary
*(updated by agent on resume — user text above remains untouched)*
- Goal: Enable per-image independent versioning + clean up script inconsistencies
- Scope: `version.sh`, `version.ps1`, `build-push.sh`, `build-push.ps1`, `Taskfile.yml`, per-image `version.yaml`
- Constraints: Fully backward-compatible — omitting new flags preserves current behavior
- Success criteria:
  - `version.sh --path src/<name>` returns version scoped to that path
  - Regex and output behavior aligned between .sh and .ps1
  - Optional: dirty indicator, pre-release labels, JSON output
- Priority: `--path` is the core change; items 1-5 are opportunistic improvements

# ChangeLog
- 2026-04-09 — Issue created
- 2026-04-09 — Agent resume: added detailed proposal with --path parameter design, integration points, and backward-compat strategy
- 2026-04-09 — Agent resume: added 5 additional improvement proposals (dirty indicator, pre-release, regex alignment, stdout fix, JSON output)
- 2026-04-09 — Agent resume: no user edits detected; issue ready for /issue-create end
