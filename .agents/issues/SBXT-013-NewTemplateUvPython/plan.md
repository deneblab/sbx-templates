# Plan — SBXT-013-NewTemplateUvPython

New sandbox Docker template `sbx-claude-python-uv`: Python-only, latest CPython managed by
Astral's `uv` (https://docs.astral.sh/uv/). Follows the existing two-stage template pattern.

## Design decisions
- Image name: `sbx-claude-python-uv` (dir `src/sbx-claude-python-uv/`).
- Python-only — no Node.js.
- Latest CPython via `uv python install --default` (creates `python`/`python3` on PATH in `~/.local/bin`).
- `uv`/`uvx` binaries copied from `ghcr.io/astral-sh/uv:latest` distroless image (per Astral Docker guide).
- Cache convention: `UV_CACHE_DIR=/workspace/.sbx-cache/uv` (persists like nuget/npm/go caches).
- `UV_LINK_MODE=copy` (safe across cache-mount filesystems). `SBX_SANDBOX=true` like all images.
- Two-stage build (`deps` + `claude`) so Claude Code updates independently via `--no-cache-filter claude`.
- No per-image `version.yaml` (shared root `version.yaml`, same as other templates).

## Components
1. [ ] `src/sbx-claude-python-uv/Dockerfile` — NEW. deps stage installs base tools + uv + latest Python;
       claude stage installs Claude Code + OCI labels.
2. [ ] `Taskfile.yml` — EDIT. Add `version:python-uv`, `build:python-uv`, `push:python-uv`,
       `update-claude:python-uv` (mirrors golang124-node24 block; internal `_*:{{OS}}` tasks are generic).
3. [ ] `README.md` — EDIT. Add templates-table row for `pkudrel/sbx-claude-python-uv` (Python + uv).
4. [ ] `CLAUDE.md` — EDIT. Add bullet to Project Overview + `build:python-uv` / `update-claude:python-uv`
       lines to the Taskfile examples.

## Integration notes
- `build-push.ps1`/`.sh` and `version.ps1`/`.sh` are image-agnostic (driven by `-ImageName`/`--image`),
  so no script changes are needed — only the new Dockerfile + Taskfile wiring.
- Creation order: 1 → 2 → 3 → 4.

## Verification
- `task version:python-uv` prints a semver.
- `task build:python-uv` (or `-DryRun`) builds; inside container `uv --version`, `python --version`,
  `python3 --version` resolve, and `/workspace/.sbx-cache/uv` is the uv cache dir.
