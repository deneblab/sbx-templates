# AbcVersion: `--path` selects a repository, `--project` scopes the commit count

Findings from migrating this repository off `version.yaml` onto AbcVersion
(commit `5410b11`). Written down because the distinction decides how templates
are versioned, and because the obvious reading of `--path` is wrong.

- **Measured at** `HEAD = 5410b11`, 9 commits total, `BaseVersion: 0.2.0`
- **Tool version** AbcVersion 1.2.17 (`releases/latest` at time of writing)

---

## TL;DR

`--path` answers *which repository*. `--project` answers *which part of it*.
They are independent axes, and **only `--project` narrows the commit count**.

Pointing `--path` at a subdirectory does not scope anything — it returns the
same repo-wide version as running the tool at the root. Per-directory versions
exist only as named `Projects` entries in `.abcversion.json`.

---

## Why this came up

Each template in `src/` needs its own version, so that a template nobody
touched keeps its tag across a release and `sbxup` reuses the image a user has
already built. The previous `version.sh --path src/<dir>` did exactly that with
an ad-hoc flag.

The question was whether AbcVersion's `--path` could replace it — which would
have meant no per-template configuration at all, preserving the repo's "adding
a template is adding a directory" property. It cannot.

---

## The two axes

From `abcversion --help`:

```
Options:
  --path <path>        Path to git repository (default: current directory)
  --project <name>     Project name from .abcversion.json (default: main)
```

The help text is accurate and complete; it is just easy to misread `--path` as
a path *filter* when it is a repository *locator*.

## Evidence

All rows measured in one pass at `HEAD = 5410b11`:

| Command | Result |
|---|---|
| `abcversion -p semversion` | `0.2.9` |
| `abcversion -p semversion --path src` | `0.2.9` |
| `abcversion -p semversion --path src/sbx-claude-dotnet10` | `0.2.9` |
| `abcversion -p semversion --path src/sbx-claude-python-uv` | `0.2.9` |
| `abcversion -p semversion --project templates` | **`0.2.4`** |
| `abcversion -p semversion --project dotnet10` | **`0.2.4`** |
| `abcversion -p semversion --project python-uv` | **`0.2.3`** |
| `abcversion -p semversion --project sbxup` | **`0.2.5`** |

Every `--path` row equals the repo-wide `0.2.9`: the subtree is not a filter.

The `--project` rows confirm the actual formula — `BaseVersion` plus commits
touching that project's `Path`:

| Project | `Path` | `git rev-list --count HEAD -- <path>` | Version |
|---|---|---|---|
| `templates` | `src` | 4 | `0.2.0` + 4 = `0.2.4` |
| `dotnet10` | `src/sbx-claude-dotnet10` | 4 | `0.2.4` |
| `python-uv` | `src/sbx-claude-python-uv` | 3 | `0.2.3` |
| `sbxup` | `cmd/sbxup` | 5 | `0.2.5` |
| *(repo-wide)* | — | 9 | `0.2.9` |

`python-uv` sitting a patch behind its siblings is the property being bought:
it is the one template that commit `5410b11` touched one fewer time.

### The flags compose

`--path` is not useless — it drives a repository you are not standing in. Run
from `/tmp`, entirely outside the checkout:

```console
$ abcversion -p semversion --path /w/DenebLab/sbx-templates --project python-uv
0.2.3
```

That is its real use: a CI job whose checkout lives in a subdirectory, or one
script versioning several repositories. Nothing in `sbx-templates` needs it —
`manifest.sh` and `build-push.sh` both `cd` to the repo root first.

---

## Consequence for this repository

Per-template scoping only exists as configuration, so **adding a template means
adding a `Projects` entry**, keyed by the template's `short` name:

```json
"python-uv": {
  "Name": "python-uv",
  "Path": "src/sbx-claude-python-uv",
  "BaseVersion": "0.2.0"
}
```

That is a real cost — it breaks the previous "adding a template is adding a
directory, nothing else to update" invariant, and `src/*/template.yaml`,
`manifest.sh`, `CLAUDE.md` and `README.md` were all updated to say so.

### Why the cost is acceptable: a missing entry fails loudly

The danger with a name lookup is a silent fallback to a default — which here
would be the repo-wide number, a real-looking version with wrong semantics
(it changes on every commit anywhere, churning the tag of a template that
never changed). AbcVersion does not do that:

```console
$ abcversion -p semversion --project nope
ERROR ... System.ArgumentException: Project 'nope' not found in .abcversion.json.
                                    Available projects: sbxup
$ echo $?
1
```

Non-zero exit, and it lists what *is* configured. `manifest.sh` and
`build-push.sh` catch this and turn it into the fix:

```
Error: no 'rust' project in .abcversion.json
  add: "rust": { "Name": "rust", "Path": "src/<dir>", "BaseVersion": "0.2.0" }
```

So forgetting the entry costs a failed build with the JSON to paste, not a
quietly wrong release.

---

## Secondary finding: `info --path <subdir>` misreports the config

`abcversion info` given a subdirectory claims it found no configuration, yet
still applies the `BaseVersion` from it:

```console
$ abcversion info --path src/sbx-claude-python-uv
Repository:    /w/DenebLab/sbx-templates/src/sbx-claude-python-uv
Config:        (not found)
SemVersion:    0.2.9
```

`0.2.9` is `0.2.0` (from `.abcversion.json`) + 9 commits. If the config had
genuinely been ignored the answer would be `0.0.9`, because the default
`BaseVersion` is `0.0.0` — confirmed against a scratch repository with three
commits and no config file:

```console
$ abcversion --path /tmp/plainrepo -p semversion
0.0.3
```

So the `Config: (not found)` line is a display artifact of passing a
subdirectory, not a statement about what was used. Harmless to the numbers,
but actively misleading while diagnosing a version — which is the one job
`info` exists to do.

---

## Reproduce

```bash
git rev-parse --short HEAD; git rev-list --count HEAD

abcversion -p semversion
abcversion -p semversion --path src                       # same answer => no scoping
abcversion -p semversion --project templates              # different => scoping

# the formula, for any project
git rev-list --count HEAD -- src/sbx-claude-python-uv     # 3
abcversion -p semversion --project python-uv              # 0.2.0 + 3

# default BaseVersion with no config
mkdir /tmp/plainrepo && cd /tmp/plainrepo && git init -q .
for i in 1 2 3; do : > f$i; git add .; git commit -qm c$i; done
abcversion -p semversion                                  # 0.0.3
```

Version numbers rise as commits land; the *relationships* between them are the
stable part.

---

## Possible upstream changes (deneblab/AbcVersion)

Neither is required — this repository works as configured — but both would have
saved time here:

1. **Fix the `info` config line** so it reports the configuration actually used
   when `--path` names a subdirectory. Lowest effort, removes a misleading
   diagnostic.
2. **Consider making `--path` scope the commit count** when it points inside a
   repository, or add a separate flag (`--scope`, say) that does. That would
   allow per-directory versions without a `Projects` entry per directory, which
   is the property this repository gave up. If the current behaviour is
   deliberate, saying so explicitly in `--help` — "repository location, not a
   path filter" — would settle it at the point of use.
