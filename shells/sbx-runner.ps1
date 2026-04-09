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
    [CmdletBinding()]
    param(
        [string]$Config   = "",
        [string]$Template = "",
        [string]$Agent    = "",
        [string]$Branch   = "",
        [switch]$Exec,
        [switch]$Status,
        [switch]$Stop,
        [switch]$DryRun,
        [switch]$Help,
        [Parameter(ValueFromRemainingArguments)]
        [string[]]$ExtraArgs
    )

    # --- Help ----------------------------------------------------------------
    if ($Help) {
        Write-Host @"
sbx-runner — launch and manage Claude Code sandboxes from sbx-runner.yaml

Usage:
  sbx-runner                   Run sandbox using config defaults
  sbx-runner -Branch feat      Override the branch name
  sbx-runner -Exec             Open a shell in the existing sandbox
  sbx-runner -Status           List sandboxes for the current project
  sbx-runner -Stop             Stop the sandbox for the current project
  sbx-runner -DryRun           Preview the sbx command without running it
  sbx-runner -Help             Show this help message

Parameters:
  -Config <path>    Path to YAML config (default: .agents\sbx-runner.yaml or sbx-runner.yaml)
  -Template <img>   Docker image to use (overrides config)
  -Agent <name>     Agent name, e.g. claude (overrides config)
  -Branch <name>    Branch name; set to 'auto' in config to use current git branch
  -Exec             Exec into an existing sandbox instead of creating one
  -Status           List sandboxes matching the current project (agent-folder pattern)
  -Stop             Stop the sandbox for the current project
  -DryRun           Show what would run without executing

Config file (.agents\sbx-runner.yaml):
  template: docker.io/pkudrel/sbx-claude-dotnet10:latest
  agent: claude
  branch: auto        # resolves to current git branch
"@
        return
    }

    # --- Helpers -------------------------------------------------------------
    function _ReadYaml([string]$File, [string]$Key) {
        $line = Get-Content $File | Where-Object { $_ -match "^\s*${Key}\s*:" } | Select-Object -First 1
        if (-not $line) { return "" }
        ($line -replace "^\s*${Key}\s*:\s*", "") -replace '#.*', '' | ForEach-Object { $_.Trim() }
    }

    function _ResolveGitBranch() {
        try {
            $ref = git rev-parse --abbrev-ref HEAD 2>$null
            if ($LASTEXITCODE -eq 0 -and $ref) { return $ref.Trim() }
        } catch {}
        return ""
    }

    function _ValidateConfig([string]$File) {
        $knownKeys = @("template", "agent", "branch")
        $lines = Get-Content $File | Where-Object { $_ -match "^\s*\S+\s*:" -and $_ -notmatch "^\s*#" }
        foreach ($line in $lines) {
            $key = ($line -replace "\s*:.*", "").Trim()
            if ($key -notin $knownKeys) {
                Write-Warning "Unknown key '$key' in $File (expected: $($knownKeys -join ', '))"
            }
        }
    }

    # --- Resolve config path -------------------------------------------------
    if (-not $Config) {
        if      (Test-Path ".agents\sbx-runner.yaml") { $Config = ".agents\sbx-runner.yaml" }
        elseif  (Test-Path "sbx-runner.yaml")         { $Config = "sbx-runner.yaml" }
    }

    if ($Config -and (Test-Path $Config)) {
        Write-Host "Config: $Config"
        _ValidateConfig $Config
        if (-not $Template) { $Template = _ReadYaml $Config "template" }
        if (-not $Agent)    { $Agent    = _ReadYaml $Config "agent" }
        if (-not $Branch)   { $Branch   = _ReadYaml $Config "branch" }
    } elseif ($Config) {
        Write-Error "Config file not found: $Config"
        return
    }

    if (-not $Agent) {
        $configHint = if ($Config) { $Config } else { ".agents\sbx-runner.yaml (not found)" }
        Write-Error "agent is required (set in $configHint or pass -Agent). Run 'sbx-runner -Help' for usage."
        return
    }

    # --- Sandbox name --------------------------------------------------------
    $folderName = (Get-Item .).Name
    $sandboxName = "$Agent-$folderName"

    # --- Status mode ---------------------------------------------------------
    if ($Status) {
        Write-Host "Sandboxes matching '$folderName':"
        $list = & sbx list 2>&1 | Where-Object { $_ -match [regex]::Escape($folderName) }
        if ($list) { $list | ForEach-Object { Write-Host "  $_" } }
        else       { Write-Host "  (none found)" }
        return
    }

    # --- Stop mode -----------------------------------------------------------
    if ($Stop) {
        if ($DryRun) { Write-Host "[dry-run] sbx stop $sandboxName"; return }
        Write-Host "Stopping sandbox: $sandboxName"
        & sbx stop $sandboxName
        return
    }

    # --- Exec mode -----------------------------------------------------------
    if ($Exec) {
        if ($DryRun) { Write-Host "[dry-run] sbx exec -it $sandboxName bash"; return }
        Write-Host "Exec into: $sandboxName"
        & sbx exec -it $sandboxName bash
        return
    }

    if (-not $Template) {
        $configHint = if ($Config) { $Config } else { ".agents\sbx-runner.yaml (not found)" }
        Write-Error "template is required (set in $configHint or pass -Template). Run 'sbx-runner -Help' for usage."
        return
    }

    # --- Resolve branch: auto ------------------------------------------------
    if ($Branch -eq "auto") {
        $Branch = _ResolveGitBranch
        if (-not $Branch) {
            Write-Error "branch is set to 'auto' but could not detect the current git branch. Are you in a git repository?"
            return
        }
        Write-Host "Branch (auto-detected): $Branch"
    }

    # --- Build and run -------------------------------------------------------
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
