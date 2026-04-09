---
name: issue-create
description: "MD-only Planner for .agents issues"
---

# /issue-create — MD-only Planner for `.agents` (Ultra‑Safe Version)

**Arguments:** $ARGUMENTS — Issue title and optional --desc flag
**Creates/Edits:** .agents/issues/{ISSUE_ID}/issue.md only (planning‑only; reads allowed)

> Scope & Permissions
> - Planning‑only for issue content. The agent may create/update only .agents/issues/{ISSUE_ID}/issue.md.
> - **Single exception (configuration):** the agent may read **and write** .agents/config.md **only** to set or read the key SHORT-PROJECT-ID. No other files or keys may be modified.
> - This document avoids executable-looking code to satisfy strict bash-permission guards.

---

## Allowed invocations (examples; text only)
- /issue-create <title>  --desc <short description>    ← init or refine planning file
- /issue-create resume                                    ← analyze current issue.md and append ChangeLog
- /issue-create end                                       ← finalize planning and run the next command (skill) (not export)

---

## ID Schema (immutable project prefix, via config.md)

{ISSUE_ID} = {SHORT-PROJECT-ID}-{NNN}-{TITLE}

- SHORT-PROJECT-ID — **primary source** is .agents/config.md (see "Config file format").
  If missing, generate from repo folder name (sanitize [A-Za-z0-9], UPPERCASE, max 7), **save it** to .agents/config.md, and then use it.
  Once stored in config.md, this value is **immutable** and must be used for all future issues.
- NNN — next 3-digit, zero‑padded number for that project (001, 002, …).
- TITLE — directory‑friendly PascalCase, ~≤25 chars, diacritics/punctuation removed.

Collision handling: if {ISSUE_ID} already exists, append a numeric suffix (-2, -3, …).

---

## Config file format (.agents/config.md)

The agent reads and writes a simple key/value in Markdown. Example:

```md
# Agents Config

SHORT-PROJECT-ID: PROJECT
```

Rules:
- The key name is **SHORT-PROJECT-ID** (exact casing, hyphens).
- Value must be `[A-Z0-9]{1,7}` after sanitization.
- If multiple values are present or invalid, the agent aborts and asks to resolve manually.
- If existing issue folders have a **different** prefix than the config value, the agent aborts with an integrity error (to avoid drift).
- Do not start Phase 2, or any project analyze without user input
---

## Project detection (with config.md precedence)

1) Look for .agents/config.md; if found, parse the value of SHORT-PROJECT-ID.
   • If valid, use it.
   • If invalid or duplicated, abort with an error.
2) If no value is found: derive from the repo root folder name (the directory containing .git):
   • keep [A-Za-z0-9] → uppercase → trim to 7 chars; if empty, use PROJECT.
   • **Persist** the derived value by writing SHORT-PROJECT-ID: <VALUE> into .agents/config.md (create file if missing).
3) If `.agents/issues` already has folders with a conflicting prefix, abort with an integrity error and instruct the user to unify either the config or existing folders.

---

## Next-number algorithm (per project)
1) Ensure .agents/issues exists.
2) List subfolders matching ^{PROJECT}-\d{3}-.
3) Extract the 3-digit numbers for {PROJECT}, find max N (use 0 if none).
4) NEXT = N + 1, left‑pad to 3 → 001, 002, …

---

## Title transformation rules
- Normalize whitespace; remove punctuation/diacritics.
- Split on non‑alphanumerics → CapitalizeEachToken → join (PascalCase).
- Keep only [A-Za-z0-9]; truncate to ~48 chars.

Examples:
create new template → CreateNewTemplate
Fix   AUTH bug!!! → FixAuthBug
Wąski → szeroki → WaskiSzeroki

---

## issue.md — Front Matter v1 (minimal)

```md
---
issueId: "{PROJECT}-{NNN}-{TITLE}"   # immutable; must match folder path
humanTitle: "{HUMAN_TITLE}"          # free text; mirrors H1
issueUrl: "{ISSUE_URL}"              # optional; omit if unknown
createdAt: "{UTC_ISO}"               # ISO‑8601 UTC at creation time
tags: []                             # agent‑generated slugs, e.g., [frontend, auth]
---
```

- If issueUrl is unknown, leave  the field (empty string)

---

## issue.md — Body template (planning‑only)

```md
# {HUMAN_TITLE}

**User input** (user text above remains untouched. Agent cannot modify this section; it is for user‑agent communication and planning)

{HUMAN_ISSUE_DESCRIPTION}

## Agent Summary
*(added/updated by agent on resume; user text above remains untouched)*
- Goal:
- Scope:
- Constraints:
- Success criteria:

# ChangeLog
- {YYYY-MM-DD} — Issue created
```

ChangeLog rules: Append one new line per agent update/run.
Format: - YYYY‑MM‑DD — <short note>.

---

## Lifecycle

Phase 1 — Initialize
• Read SHORT-PROJECT-ID from .agents/config.md; if absent, derive, **persist to config.md**, and use it.
• Compute NNN.
• Create .agents/issues/{PROJECT}-{NNN}-{Title}/issue.md with the templates above.

Phase 2 — User Edit
• You edit issue.md to add/correct information.

Phase 3 — Start/Resume
• Run /issue-create <title> again or /issue-create resume.
• Agent reads issue.md, analyzes your changes, may add/update "## Agent Summary", and appends a dated line to # ChangeLog (for example: - 2025‑08‑29 — Added Agent Summary after user edits).

Phase 4 — End (handoff)
• Run /issue-create end. The agent:
  – Reads issue.md.
  – Derives a title (from humanTitle or H1) and a short description (first meaningful paragraph; single‑line, ~≤200 chars, quotes escaped).
  – Updates only issue.md by appending a ChangeLog line: - YYYY‑MM‑DD — Triggered scenario via /issue.
  – Now process '.claude\skills\issue-create\SKILL.md'