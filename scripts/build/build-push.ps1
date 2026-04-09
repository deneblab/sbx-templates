# build-push.ps1 — build (and optionally push) a sandbox image with computed semver.
# Usage: .\scripts\build\build-push.ps1 [-ImageName NAME] [-NoPush] [-DryRun]
#   -ImageName NAME  image directory name under src/ (default: sbx-claude-dotnet10)
#   -NoPush          build and load into local Docker daemon, do not push
#   -DryRun          print the docker command, do not execute

param(
    [string]$ImageName = "sbx-claude-dotnet10",
    [switch]$NoPush,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

$ScriptDir  = $PSScriptRoot
$RepoRoot   = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Image      = "docker.io/pkudrel/$ImageName"
$Context    = (Resolve-Path (Join-Path $RepoRoot "src\$ImageName")).Path

# Compute version scoped to this image's directory
$lines = & (Join-Path $RepoRoot "scripts/version/version.ps1") -Path "src/$ImageName"
$data  = @{}
foreach ($line in $lines) {
    if ($line -match '^(\w+)=(.+)$') { $data[$Matches[1]] = $Matches[2] }
}

$version   = $data['version']
$tag       = $data['tag']
$shortSha  = $data['short_sha']
$buildDate = $data['build_date_utc']

$pushFlag = if ($NoPush) { "--load" } else { "--push" }

Write-Host "Version : $version"
Write-Host "Tag     : $tag"
Write-Host "SHA     : $shortSha"
Write-Host "Image   : ${Image}:${tag}"
Write-Host ""

if ($DryRun) {
    Write-Host "[dry-run] would run:"
    Write-Host "  docker buildx build $pushFlag ``"
    Write-Host "    -t ${Image}:${tag} ``"
    Write-Host "    -t ${Image}:latest ``"
    Write-Host "    --build-arg VERSION=$version ``"
    Write-Host "    --build-arg SHORT_SHA=$shortSha ``"
    Write-Host "    --build-arg BUILD_DATE=$buildDate ``"
    Write-Host "    $Context"
    exit 0
}

& docker buildx build $pushFlag `
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
