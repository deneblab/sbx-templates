#Requires -Version 5.1
# sbx-runner.ps1 — dot-source this file to get the sbx-runner function.
#
# In your $PROFILE.CurrentUserAllHosts:
#   . C:\path\to\sbx-templates\shells\sbx-runner.ps1
#
# Then from any directory that has .agents\sbx-runner.yaml (or sbx-runner.yaml):
#   sbx-runner
#   sbx-runner -Branch my-feature
#   sbx-runner -DryRun
#
# Config search order:
#   1. .agents\sbx-runner.yaml  (preferred)
#   2. sbx-runner.yaml          (fallback, current directory)
#
# sbx-runner.yaml format:
#   template: docker.io/pkudrel/sbx-claude-dotnet10:latest
#   agent: claude
#   branch: auto

function sbx-runner {
    param(
        [string]$Config   = "",
        [string]$Template = "",
        [string]$Agent    = "",
        [string]$Branch   = "",
        [switch]$DryRun,
        [Parameter(ValueFromRemainingArguments)]
        [string[]]$ExtraArgs
    )

    function _ReadYaml([string]$File, [string]$Key) {
        $line = Get-Content $File | Where-Object { $_ -match "^\s*${Key}\s*:" } | Select-Object -First 1
        if (-not $line) { return "" }
        ($line -replace "^\s*${Key}\s*:\s*", "") -replace '#.*', '' | ForEach-Object { $_.Trim() }
    }

    # Resolve config path: explicit > .agents\sbx-runner.yaml > sbx-runner.yaml (current dir only)
    if (-not $Config) {
        if      (Test-Path ".agents\sbx-runner.yaml") { $Config = ".agents\sbx-runner.yaml" }
        elseif  (Test-Path "sbx-runner.yaml")         { $Config = "sbx-runner.yaml" }
    }

    if ($Config -and (Test-Path $Config)) {
        Write-Host "Config: $Config"
        if (-not $Template) { $Template = _ReadYaml $Config "template" }
        if (-not $Agent)    { $Agent    = _ReadYaml $Config "agent" }
        if (-not $Branch)   { $Branch   = _ReadYaml $Config "branch" }
    }

    if (-not $Template) { Write-Error "template is required (set in $Config or pass -Template)"; return }
    if (-not $Agent)    { Write-Error "agent is required (set in $Config or pass -Agent)"; return }

    $sbxArgs = @("run", "--template", $Template, $Agent)
    if ($Branch)    { $sbxArgs += @("--branch", $Branch) }
    if ($ExtraArgs) { $sbxArgs += $ExtraArgs }

    if ($DryRun) {
        Write-Host "[dry-run] sbx $($sbxArgs -join ' ')"
        return
    }

    # Capture all output (stdout + stderr) to detect "already exists"
    $all = & sbx @sbxArgs 2>&1
    $allText = $all -join "`n"
    $all | ForEach-Object {
        if ($_ -is [System.Management.Automation.ErrorRecord]) { Write-Error $_.Exception.Message }
        else { Write-Output $_ }
    }

    if ($LASTEXITCODE -ne 0 -and $allText -match "sandbox '([^']+)' already exists") {
        $existingName = $Matches[1]
        Write-Host "Resuming existing sandbox: $existingName"
        & sbx run $existingName
    }
}
