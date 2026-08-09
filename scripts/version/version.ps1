# version.ps1 - PowerShell equivalent of version.sh
# Computes semver from version.yaml + git commit count.
#
# version.yaml format:
#   baseVersion: 0.1.0
#   baseCommitSha: abc1234   # optional

param(
    [switch]$VersionOnly,
    [string]$ConfigFile = "version.yaml",
    [string]$Override = "",
    [string]$TagPrefix = "v",
    [string]$Path = ""
)

if ($env:INPUT_CONFIG_FILE) { $ConfigFile = $env:INPUT_CONFIG_FILE }
if ($env:INPUT_MAJOR_MINOR) { $Override = $env:INPUT_MAJOR_MINOR }
if ($env:INPUT_TAG_PREFIX)  { $TagPrefix = $env:INPUT_TAG_PREFIX }

function ParseSemver([string]$InputStr) {
    if ($InputStr -match '^(\d+)\.(\d+)(\.(\d+))?$') {
        $p = 0
        if ($null -ne $Matches[4] -and $Matches[4] -ne '') { $p = [int]$Matches[4] }
        return @{
            Major = [int]$Matches[1]
            Minor = [int]$Matches[2]
            Patch = $p
        }
    }
    Write-Error "Cannot parse semver from '$InputStr'"
    exit 1
}

function InvokeGit {
    $result = & git @args 2>$null
    if ($result) { ($result -join " ").Trim() } else { "" }
}

function YamlVal([string]$File, [string]$Key) {
    $line = Get-Content $File | Where-Object { $_ -match "^${Key}:" } | Select-Object -First 1
    if (-not $line) { return "" }
    $val = ($line -replace "^${Key}:\s*", "") -replace '#.*', ''
    return $val.Trim()
}

# Resolve ConfigFile relative to repo root.
$repoRoot = (& git rev-parse --show-toplevel 2>$null)
if ($repoRoot -and -not [System.IO.Path]::IsPathRooted($ConfigFile)) {
    $ConfigFile = Join-Path $repoRoot $ConfigFile
}

# 1. Load Base Version
$baseCommitSha = ""
if ($Override -ne "") {
    $ver = ParseSemver $Override
} else {
    if (-not (Test-Path $ConfigFile)) {
        Write-Error "$ConfigFile not found"
        exit 1
    }
    $baseVersion = YamlVal $ConfigFile "baseVersion"
    if (-not $baseVersion) {
        Write-Error "baseVersion not found in $ConfigFile"
        exit 1
    }
    $ver = ParseSemver $baseVersion
    $baseCommitSha = YamlVal $ConfigFile "baseCommitSha"
}

# 2. Calculate Increment
$pathArgs = @()
if ($Path -ne "") {
    $pathArgs = @("--", $Path)
}

if ($baseCommitSha -ne "") {
    # Validate SHA exists in repo.
    $objType = InvokeGit cat-file -t $baseCommitSha
    if ($objType -ne "commit") {
        Write-Error "baseCommitSha '$baseCommitSha' not found in git history"
        exit 1
    }
    $inc = InvokeGit rev-list --count "${baseCommitSha}..HEAD" @pathArgs
} else {
    $inc = InvokeGit rev-list --count HEAD @pathArgs
}
if (-not $inc -or $inc -eq "") { $inc = 0 } else { $inc = [int]$inc }

# 3. Final Calculation
$finalPatch = $ver.Patch + $inc
$version = "$($ver.Major).$($ver.Minor).$finalPatch"

# 4. Output
if ($VersionOnly) {
    Write-Output $version
} else {
    $fullSha   = InvokeGit rev-parse HEAD
    $shortSha  = InvokeGit rev-parse --short=7 HEAD
    $branch    = InvokeGit branch --show-current
    if (-not $branch) { $branch = InvokeGit rev-parse --abbrev-ref HEAD }
    $buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $tag       = "$TagPrefix$version"

    $lines = @(
        "version=$version"
        "major=$($ver.Major)"
        "minor=$($ver.Minor)"
        "patch=$finalPatch"
        "tag=$tag"
        "sha=$fullSha"
        "short_sha=$shortSha"
        "branch=$branch"
        "build_date_utc=$buildDate"
    )

    if ($env:GITHUB_OUTPUT) {
        $lines | Out-File -Append -FilePath $env:GITHUB_OUTPUT -Encoding utf8
    } else {
        $lines | ForEach-Object { Write-Output $_ }
    }
}
