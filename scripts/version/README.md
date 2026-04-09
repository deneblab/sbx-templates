# version

Portable semver calculator based on `version.yaml` + git commit count. Drop this folder into any git repo.

## Files

```
version/
  version.sh          # Bash
  version.ps1         # PowerShell
  tests/
    version.sh.test.sh       # Bash tests
    version.ps1.Tests.ps1    # Pester tests
```

## Setup

Create a `version.yaml` at your repo root (or pass `--file`):

```yaml
baseVersion: 0.1.0
baseCommitSha: abc1234   # optional — count commits only after this SHA
```

## Usage

```bash
# Print just the version
bash scripts/version/version.sh --version-only          # 0.1.5

# Full key=value output (for eval or CI)
bash scripts/version/version.sh                          # version=0.1.5 tag=v0.1.5 ...

# Scope counting to a subdirectory (monorepo)
bash scripts/version/version.sh --path src/my-service    # counts only commits touching that path

# Use alternate config file
bash scripts/version/version.sh --file path/to/version.yaml

# Override base version (ignores version.yaml)
bash scripts/version/version.sh --override 2.0.0

# Custom tag prefix
bash scripts/version/version.sh --prefix "release-"
```

PowerShell equivalents:

```powershell
pwsh scripts/version/version.ps1 -VersionOnly
pwsh scripts/version/version.ps1 -Path "src/my-service"
pwsh scripts/version/version.ps1 -ConfigFile "path/to/version.yaml"
pwsh scripts/version/version.ps1 -Override "2.0.0"
pwsh scripts/version/version.ps1 -TagPrefix "release-"
```

## Output

Full output (key=value, one per line):

```
version=0.1.5
major=0
minor=1
patch=5
tag=v0.1.5
sha=abc1234567890abcdef1234567890abcdef123456
short_sha=abc1234
branch=main
build_date_utc=2026-04-09T12:00:00Z
```

In CI, set `GITHUB_OUTPUT` to write directly to GitHub Actions output file.

## How versioning works

```
final_patch = version.yaml.baseVersion.patch + commit_count
```

- `commit_count` = total commits (or commits after `baseCommitSha` if set)
- With `--path`: only commits touching that directory are counted
- Major and minor come from `baseVersion`; patch is computed

## Running tests

```bash
bash scripts/version/tests/version.sh.test.sh
```

```powershell
Invoke-Pester scripts/version/tests/version.ps1.Tests.ps1
```
