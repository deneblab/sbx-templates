---
issueId: "SBXT-013-NewTemplateUvPython"
humanTitle: "New template: UV and Python"
issueUrl: ""
createdAt: "2026-07-03T08:28:22Z"
tags: [docker, template, python, uv]
---

# New template: UV and Python

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user‑agent communication and planning)

New templet: UV and python

## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal: Add a new sandbox Docker template `sbx-claude-python-uv` providing the latest Python managed via `uv`, following the existing `sbx-claude-*` template pattern.
- Scope: New `src/sbx-claude-python-uv/Dockerfile` (two-stage `deps` + `claude` build), Taskfile tasks (`build:python-uv`, `update-claude:python-uv`), build/version script wiring, `uv` cache under `/workspace/.sbx-cache/uv`, and CLAUDE.md/README docs.
- Decisions (confirmed by user):
  - Python-only (no Node.js pairing).
  - Latest Python (installed/managed by `uv`, e.g. `uv python install`).
  - Image name: `sbx-claude-python-uv`.
- Constraints: Mirror existing templates (extend `docker/sandbox-templates:claude-code`, set `SBX_SANDBOX=true`, independent Claude Code update via `--no-cache-filter claude`).
- Reference: `uv` = Astral's Python package/project manager — https://docs.astral.sh/uv/ (Docker guide: https://docs.astral.sh/uv/guides/integration/docker/).
- Install approach (from Astral Docker guide):
  - Add binary in `deps` stage via `COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/` (pin to a specific `uv` version tag for reproducibility).
  - Install latest Python with `uv python install` (consider `--compile-bytecode`).
  - Env vars: `UV_CACHE_DIR=/workspace/.sbx-cache/uv` (constant cache location under existing convention), `UV_LINK_MODE=copy` (safe with cache mounts across filesystems), `UV_COMPILE_BYTECODE=1`.
- Success criteria: Image builds and pushes via Taskfile; `uv`/`uvx` and latest `python` available on PATH inside the sandbox; `uv` cache persists under `.sbx-cache/uv`; docs updated.

# ChangeLog
- 2026-07-03 — Issue created
- 2026-07-03 — Recorded decisions: Python-only, latest Python via uv, image name sbx-claude-python-uv; updated Agent Summary
- 2026-07-03 — Clarified uv = Astral (docs.astral.sh/uv); added reference links and recommended Docker install approach (COPY from ghcr.io/astral-sh/uv, uv python install, UV_CACHE_DIR/UV_LINK_MODE env)
- 2026-07-03 — Triggered scenario via /issue
- 2026-07-03 — Scaffolded template: created src/sbx-claude-python-uv/Dockerfile; wired Taskfile (version/build/push/update-claude:python-uv); updated README + CLAUDE.md. Verified version script (0.1.0), task list, and build dry-run
- 2026-07-03 — Local build succeeded (docker.io/pkudrel/sbx-claude-python-uv:v0.1.0 + :latest). Smoke test OK: uv 0.9.26, CPython 3.14.2 (python/python3), Claude Code 2.1.199; UV_CACHE_DIR=/workspace/.sbx-cache/uv, UV_LINK_MODE=copy, SBX_SANDBOX=true; all binaries resolve on PATH
