# build-push.ps1 — build (and optionally push) a sandbox image with computed semver.
# Usage: .\scripts\build\build-push.ps1 [-ImageName NAME] [-NoPush] [-DryRun] [-UpdateClaude]
#   -ImageName NAME  image directory name under src/ (default: sbx-claude-dotnet10)
#   -NoPush          build and load into local Docker daemon, do not push
#   -DryRun          print the docker command, do not execute
#   -UpdateClaude    skip cache for the 'claude' stage only (fast Claude Code update)

param(
    [string]$ImageName = "sbx-claude-dotnet10",
    [switch]$NoPush,
    [switch]$DryRun,
    [switch]$UpdateClaude
)

$ErrorActionPreference = 'Stop'

$ScriptDir  = $PSScriptRoot
$RepoRoot   = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Image      = "docker.io/pkudrel/$ImageName"
$Context    = (Resolve-Path (Join-Path $RepoRoot "src\$ImageName")).Path

$AbcVersionMin = [version]"1.2.18" # first release with --scope

if (-not (Get-Command abcversion -ErrorAction SilentlyContinue)) {
    Write-Error "abcversion not found on PATH - install it from https://github.com/deneblab/abcversion/releases/latest"
    exit 1
}

$abcRaw = (& abcversion --version) -join " "
if ($abcRaw -notmatch '(\d+\.\d+\.\d+)' -or [version]$Matches[1] -lt $AbcVersionMin) {
    Write-Error "abcversion $AbcVersionMin+ required for --scope (found '$abcRaw') - update from https://github.com/deneblab/abcversion/releases/latest"
    exit 1
}

# Version scoped to this image's directory: BaseVersion plus the commits touching it, so an
# unchanged template keeps its tag. No .abcversion.json entry needed — --scope is the narrowing.
$version = & abcversion -p semversion --scope "src/$ImageName" 2>$null
if ($LASTEXITCODE -ne 0 -or -not $version) {
    Write-Error "abcversion --scope 'src/$ImageName' failed - does that directory exist in git?"
    exit 1
}

$tag       = "v$version"
$shortSha  = (& git -C $RepoRoot rev-parse --short=7 HEAD).Trim()
$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'")

$pushFlag     = if ($NoPush) { "--load" } else { "--push" }
$noCacheArgs  = if ($UpdateClaude) { @("--no-cache-filter", "claude") } else { @() }

Write-Host "Version       : $version"
Write-Host "Tag           : $tag"
Write-Host "SHA           : $shortSha"
Write-Host "Image         : ${Image}:${tag}"
Write-Host "Update claude : $UpdateClaude"
Write-Host ""

if ($DryRun) {
    $noCacheStr = if ($UpdateClaude) { "--no-cache-filter claude " } else { "" }
    Write-Host "[dry-run] would run:"
    Write-Host "  docker buildx build $pushFlag ${noCacheStr}``"
    Write-Host "    -t ${Image}:${tag} ``"
    Write-Host "    -t ${Image}:latest ``"
    Write-Host "    --build-arg VERSION=$version ``"
    Write-Host "    --build-arg SHORT_SHA=$shortSha ``"
    Write-Host "    --build-arg BUILD_DATE=$buildDate ``"
    Write-Host "    $Context"
    exit 0
}

& docker buildx build $pushFlag @noCacheArgs `
    -t "${Image}:${tag}" `
    -t "${Image}:latest" `
    --build-arg "VERSION=$version" `
    --build-arg "SHORT_SHA=$shortSha" `
    --build-arg "BUILD_DATE=$buildDate" `
    $Context

if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ""
if ($NoPush) {
    Write-Host "Built: ${Image}:${tag} (loaded to local daemon)"
} else {
    Write-Host "Pushed: ${Image}:${tag}"
    Write-Host "Pushed: ${Image}:latest"
}
