#Requires -Version 5.1
# sbx-runner.ps1 — dot-source this file to get the sbx-runner function.
#
# In your $PROFILE.CurrentUserAllHosts:
#   . C:\path\to\sbx-templates\shells\sbx-runner.ps1
#
# Then from any directory that has .agents\sbx-runner.yaml (or sbx-runner.yaml):
#   sbx-runner
#   sbx-runner --init
#   sbx-runner --clone
#   sbx-runner --dry-run
#
# Config search order:
#   1. .agents\sbx-runner.yaml  (preferred)
#   2. sbx-runner.yaml          (fallback, current directory)
#
# sbx-runner.yaml format:
#   template: docker.io/pkudrel/sbx-claude-dotnet10:latest
#   agent: claude
#   clone: false        # optional: true => run on a private in-container git clone
#   cache: .sbx-cache   # optional

function sbx-runner {
    [CmdletBinding()]
    param(
        [string]$Config   = "",
        [string]$Template = "",
        [string]$Agent    = "",
        [switch]$Clone,
        [switch]$NoClone,
        [switch]$Exec,
        [switch]$Status,
        [switch]$Stop,
        [switch]$Init,
        [switch]$DryRun,
        [switch]$Help,
        [Parameter(ValueFromRemainingArguments)]
        [string[]]$ExtraArgs
    )

    # --- Normalize --double-dash arguments ------------------------------------
    # PowerShell does not map --flag to -Flag for functions; reparse manually.
    $_reparse = @()
    if ($Config   -match '^--') { $_reparse += $Config;   $Config   = '' }
    if ($Template -match '^--') { $_reparse += $Template; $Template = '' }
    if ($Agent    -match '^--') { $_reparse += $Agent;    $Agent    = '' }
    if ($ExtraArgs) { $_reparse += $ExtraArgs }
    $ExtraArgs = @()
    for ($_i = 0; $_i -lt $_reparse.Count; $_i++) {
        switch ($_reparse[$_i].ToLower()) {
            '--init'     { $Init    = $true }
            '--clone'    { $Clone   = $true }
            '--no-clone' { $NoClone = $true }
            '--exec'     { $Exec    = $true }
            '--status'   { $Status  = $true }
            '--stop'     { $Stop    = $true }
            '--dry-run'  { $DryRun  = $true }
            '--help'     { $Help    = $true }
            '--config'   { $_i++; if ($_i -lt $_reparse.Count) { $Config   = $_reparse[$_i] } }
            '--template' { $_i++; if ($_i -lt $_reparse.Count) { $Template = $_reparse[$_i] } }
            '--agent'    { $_i++; if ($_i -lt $_reparse.Count) { $Agent    = $_reparse[$_i] } }
            default      { $ExtraArgs += $_reparse[$_i] }
        }
    }

    # --- Help ----------------------------------------------------------------
    if ($Help) {
        Write-Host @"
sbx-runner — launch and manage Claude Code sandboxes from sbx-runner.yaml

Usage:
  sbx-runner                    Run sandbox using config defaults
  sbx-runner --init             Create a default sbx-runner.yaml config
  sbx-runner --clone            Run on a private in-container git clone (overrides config)
  sbx-runner --no-clone         Disable clone mode (overrides config)
  sbx-runner --exec             Open a shell in the existing sandbox
  sbx-runner --status           List sandboxes for the current project
  sbx-runner --stop             Stop the sandbox for the current project
  sbx-runner --dry-run          Preview the sbx command without running it
  sbx-runner --help             Show this help message

Parameters:
  --config <path>    Path to YAML config (default: .agents\sbx-runner.yaml or sbx-runner.yaml)
  --template <img>   Docker image to use (overrides config)
  --agent <name>     Agent name, e.g. claude (overrides config)
  --clone            Pass --clone to 'sbx run' (private in-container git clone)
  --no-clone         Force clone mode off, even if enabled in config
  --init             Generate a default config file (.agents\sbx-runner.yaml)
  --exec             Exec into an existing sandbox instead of creating one
  --status           List sandboxes matching the current project (agent-folder pattern)
  --stop             Stop the sandbox for the current project
  --dry-run          Show what would run without executing

Config file (.agents\sbx-runner.yaml):
  template: docker.io/pkudrel/sbx-claude-dotnet10:latest
  agent: claude
  clone: false        # optional: true => run on a private in-container git clone
  cache: .sbx-cache   # optional: mount local cache dir into sandbox
"@
        return
    }

    # --- Helpers -------------------------------------------------------------
    function _ReadYaml([string]$File, [string]$Key) {
        $line = Get-Content $File | Where-Object { $_ -match "^\s*${Key}\s*:" } | Select-Object -First 1
        if (-not $line) { return "" }
        ($line -replace "^\s*${Key}\s*:\s*", "") -replace '#.*', '' | ForEach-Object { $_.Trim() }
    }

    function _ValidateConfig([string]$File) {
        $knownKeys = @("template", "agent", "clone", "cache")
        $lines = Get-Content $File | Where-Object { $_ -match "^\s*\S+\s*:" -and $_ -notmatch "^\s*#" }
        foreach ($line in $lines) {
            $key = ($line -replace "\s*:.*", "").Trim()
            if ($key -eq "branch") {
                Write-Warning "Key 'branch' in $File is no longer supported ('sbx run' dropped --branch). Rename it to 'clone: true|false'."
            } elseif ($key -notin $knownKeys) {
                Write-Warning "Unknown key '$key' in $File (expected: $($knownKeys -join ', '))"
            }
        }
    }

    function _IsTruthy([string]$Value) {
        return ($Value.Trim().ToLower() -in @("true", "1", "yes", "on"))
    }

    # --- Init mode -----------------------------------------------------------
    if ($Init) {
        $initPath = if ($Config) { $Config } else { ".agents\sbx-runner.yaml" }
        if (Test-Path $initPath) {
            Write-Warning "Config file already exists: $initPath"
            return
        }
        $initDir = Split-Path $initPath -Parent
        if ($initDir -and -not (Test-Path $initDir)) {
            New-Item -ItemType Directory -Path $initDir -Force | Out-Null
        }
        @"
template: docker.io/pkudrel/sbx-claude-dotnet10:latest
agent: claude
clone: false
"@ | Set-Content -Path $initPath -Encoding UTF8
        Write-Host "Created config: $initPath"
        return
    }

    # --- Resolve config path -------------------------------------------------
    if (-not $Config) {
        if      (Test-Path ".agents\sbx-runner.yaml") { $Config = ".agents\sbx-runner.yaml" }
        elseif  (Test-Path "sbx-runner.yaml")         { $Config = "sbx-runner.yaml" }
    }

    $CloneConfig = ""
    if ($Config -and (Test-Path $Config)) {
        Write-Host "Config: $Config"
        _ValidateConfig $Config
        if (-not $Template) { $Template = _ReadYaml $Config "template" }
        if (-not $Agent)    { $Agent    = _ReadYaml $Config "agent" }
        $CloneConfig = _ReadYaml $Config "clone"
        $Cache = _ReadYaml $Config "cache"
    } elseif ($Config) {
        Write-Error "Config file not found: $Config"
        return
    }

    if (-not $Agent) {
        $configHint = if ($Config) { $Config } else { ".agents\sbx-runner.yaml (not found)" }
        Write-Error "agent is required (set in $configHint or pass --agent). Run 'sbx-runner --init' to create a default config."
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
        Write-Error "template is required (set in $configHint or pass --template). Run 'sbx-runner --init' to create a default config."
        return
    }

    # --- Resolve clone mode --------------------------------------------------
    # Precedence: --no-clone > --clone > config 'clone' key (default: off).
    if ($NoClone)    { $CloneEnabled = $false }
    elseif ($Clone)  { $CloneEnabled = $true }
    else             { $CloneEnabled = (_IsTruthy $CloneConfig) }
    if ($CloneEnabled) { Write-Host "Clone mode: on (--clone)" }

    # --- Resolve cache path --------------------------------------------------
    if ($Cache) {
        $cachePath = Join-Path (Get-Item .).FullName $Cache
        if (-not (Test-Path $cachePath)) {
            New-Item -ItemType Directory -Path $cachePath -Force | Out-Null
            Write-Host "Created cache directory: $cachePath"
        }
        Write-Host "Cache: $cachePath"
    }

    # --- Build and run -------------------------------------------------------
    $sbxArgs = @("run", "--template", $Template, $Agent)
    if ($CloneEnabled) { $sbxArgs += "--clone" }
    if ($Cache)        { $sbxArgs += @(".", $cachePath) }
    if ($ExtraArgs)    { $sbxArgs += $ExtraArgs }

    if ($DryRun) {
        Write-Host "[dry-run] sbx $($sbxArgs -join ' ')"
        return
    }

    # Check if sandbox already exists; if so, resume it directly
    $existing = & sbx list 2>&1 | Where-Object { $_ -match [regex]::Escape($sandboxName) }
    if ($existing) {
        Write-Host "Resuming existing sandbox: $sandboxName"
        & sbx run --name $sandboxName
    } else {
        & sbx @sbxArgs
    }
}
