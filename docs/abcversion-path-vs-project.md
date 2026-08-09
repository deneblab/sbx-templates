# AbcVersion scoping: `--path`, `--project`, `--scope`

How this repository arrives at a version, and why the three flags are not
interchangeable. Originally written when only `--path` and `--project` existed;
**AbcVersion 1.2.18 added `--scope`, which is what the repo now uses.**

- **Requires** AbcVersion **1.2.18+** — the build scripts check and refuse older ones
- Measurements below taken at `HEAD = 4de2a0c`, 10 commits, `BaseVersion: 0.2.0`

---

## The three flags

| Flag | Question it answers | Effect on the commit count |
|---|---|---|
| `--path` | *Which repository do I read?* | none — a **locator**, not a filter |
| `--project` | *Which configured stream?* | narrows to that project's `Path` |
| `--scope` | *Which subtree?* | narrows to that subtree, no config needed |

`--scope` and `--project` both narrow, so they cannot be combined.

## What this repository uses

```bash
abcversion -p semversion --scope src                       # templates-v* release
abcversion -p semversion --scope src/sbx-claude-dotnet10   # one template's tag
abcversion -p semversion --project sbxup                   # sbxup-v* release
```

`sbxup` stays a named project because it is a release stream with its own
identity; every template number is derived from its directory, so it needs no
entry. `.abcversion.json` is just a base version plus that one project.

---

## Why `--path` cannot do this (the original finding)

The natural first guess is `--path src/<template>`. It does not work, and it
fails *silently* — it returns the repo-wide version rather than an error.
Measured before the switch, at `HEAD = 5410b11` (9 commits):

| Command | Result |
|---|---|
| `abcversion -p semversion` | `0.2.9` |
| `abcversion -p semversion --path src` | `0.2.9` |
| `abcversion -p semversion --path src/sbx-claude-dotnet10` | `0.2.9` |
| `abcversion -p semversion --path src/sbx-claude-python-uv` | `0.2.9` |

Every row equals the repo-wide number: the subtree is not a filter. As of
1.2.18 the `--help` text says so outright ("A locator, not a filter"), and
`--scope` is pointed to as the alternative.

`--path` is still useful — it drives a repository you are not standing in, and
composes with the others:

```console
$ abcversion -p semversion --path /w/DenebLab/sbx-templates --scope src
0.2.4
```

Nothing here needs it: `manifest.sh` and `build-push.sh` both `cd` to the repo
root first.

## The formula

`BaseVersion` + commits touching the scope. Verified against `git rev-list`
at `HEAD = 4de2a0c`:

| Scope | `git rev-list --count HEAD -- <path>` | Version |
|---|---|---|
| `src` | 4 | `0.2.0` + 4 = `0.2.4` |
| `src/sbx-claude-dotnet10` | 4 | `0.2.4` |
| `src/sbx-claude-python-uv` | 3 | `0.2.3` |
| `cmd/sbxup` (`--project sbxup`) | 5 | `0.2.5` |
| *(no scope — whole repo)* | 10 | `0.2.10` |

`python-uv` sitting a patch behind its siblings is the property being bought:
it is the one template the last commit touched one fewer time. An unchanged
template keeps its tag across a release, so `sbxup` reuses the image a user
already built instead of rebuilding it.

The switch from per-template `Projects` entries to `--scope` was
**version-neutral** — every project's `BaseVersion` already equalled the global
`0.2.0`, so the numbers were identical before and after.

---

## Failure modes are loud

None of the mistakes below can silently produce a plausible-but-wrong version;
each exits `1` with a specific message:

```console
$ abcversion -p semversion --scope src/does-not-exist
ArgumentException: Scope 'src/does-not-exist' matches no commits in this repository.
                   Scope is resolved from the repository root, not the current directory.

$ abcversion -p semversion --scope src --project sbxup
ArgumentException: --scope and --project cannot be used together (--scope 'src',
                   --project 'sbxup'). Both narrow the commit count — pick one.

$ abcversion -p semversion --scope /abs/path
ArgumentException: Scope '/abs/path' must be relative to the repository root.

$ abcversion -p semversion --project nope
ArgumentException: Project 'nope' not found in .abcversion.json. Available projects: sbxup
```

This is what makes it safe to derive a release number from a string. A typo in
a scope fails the build; it does not ship a tag computed from the wrong commits.

`manifest.sh` and `build-push.sh` additionally check `abcversion --version`
against the 1.2.18 minimum, so a stale binary reports that rather than an
unrecognised-flag error.

---

## Reproduce

```bash
abcversion --version                                      # expect >= 1.2.18
abcversion -p semversion --path src                       # repo-wide => locator, not filter
abcversion -p semversion --scope src                      # narrowed  => filter

git rev-list --count HEAD -- src/sbx-claude-python-uv     # 3
abcversion -p semversion --scope src/sbx-claude-python-uv # 0.2.0 + 3
```

Version numbers rise as commits land; the *relationships* between them are the
stable part.

---

## Escape hatch

If one template ever needs its own `BaseVersion` — a deliberate minor bump for a
breaking change to a single image — give that template a `Projects` entry and
switch its call site to `--project`. The two styles coexist; `--scope` is simply
the default because it costs no configuration.

---

## Changelog

- **1.2.15–1.2.17** — only `--path` and `--project`. Per-template versions
  required a `Projects` entry each, breaking "adding a template is adding a
  directory". Recorded here at the time, along with a display bug: `info --path
  <subdir>` printed `Config: (not found)` while still applying the `BaseVersion`
  from that very config.
- **1.2.18** — added `--scope`, clarified the `--path` help text, and fixed the
  `info` config line (it now reports the repository root and the config it
  used). This repository dropped its five template `Projects` entries in
  response; only `sbxup` remains.
