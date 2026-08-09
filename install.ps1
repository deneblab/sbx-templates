<#
.SYNOPSIS
    Installer for the sbxup binary on Windows.

.DESCRIPTION
    irm https://raw.githubusercontent.com/deneblab/sbx-templates/main/install.ps1 | iex

    Environment:
      SBXUP_VERSION      version to install, e.g. 0.1.4 (default: latest sbxup release)
      SBXUP_INSTALL_DIR  where to put the binary (default: $env:LOCALAPPDATA\Programs\sbxup)
      SBXUP_BASE_URL     release base URL (default: GitHub releases; override for mirrors/tests)
#>

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Install-Sbxup {
    $repoUrl   = 'https://github.com/deneblab/sbx-templates'
    $apiUrl    = 'https://api.github.com/repos/deneblab/sbx-templates/releases'
    $tagPrefix = 'sbxup-v'

    # Check architecture first, so an unsupported machine gets this message rather than an
    # incidental failure from the path setup below.
    $arch = $env:PROCESSOR_ARCHITECTURE
    $goarch = switch ($arch) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "Unsupported architecture '$arch'. Published: amd64, arm64. See $repoUrl/releases" }
    }

    $asset   = "sbxup-windows-$goarch.exe"
    $version = $env:SBXUP_VERSION
    $baseUrl = if ($env:SBXUP_BASE_URL) { $env:SBXUP_BASE_URL } else { "$repoUrl/releases" }

    if ($env:SBXUP_INSTALL_DIR) {
        $installDir = $env:SBXUP_INSTALL_DIR
    }
    elseif ($env:LOCALAPPDATA) {
        $installDir = Join-Path $env:LOCALAPPDATA 'Programs\sbxup'
    }
    else {
        throw 'LOCALAPPDATA is not set; specify a target with SBXUP_INSTALL_DIR.'
    }

    if ($version) {
        $tag = "$tagPrefix$($version -replace '^v', '')"
    }
    else {
        # This repository also publishes Docker-image releases, so /releases/latest is not
        # necessarily an sbxup release. Pick the newest tag carrying the sbxup prefix.
        try {
            $releases = Invoke-RestMethod -Uri "$apiUrl`?per_page=50" -UseBasicParsing
        }
        catch {
            throw "Cannot reach GitHub to determine the latest sbxup release. Set SBXUP_VERSION to pin one."
        }
        $tag = ($releases | Where-Object { $_.tag_name -like "$tagPrefix*" } | Select-Object -First 1).tag_name
        if (-not $tag) {
            throw "No $tagPrefix* release found at $repoUrl/releases"
        }
    }

    $url = "$baseUrl/download/$tag/$asset"
    Write-Host "Installing sbxup (windows-$goarch) from $tag"

    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Path $tmp -Force | Out-Null

    try {
        $binPath = Join-Path $tmp $asset
        $sumPath = "$binPath.sha256"

        try {
            Invoke-WebRequest -Uri $url -OutFile $binPath -UseBasicParsing
        }
        catch {
            throw "Download failed: $url`nIf you pinned a version, check it exists at $repoUrl/releases"
        }

        try {
            Invoke-WebRequest -Uri "$url.sha256" -OutFile $sumPath -UseBasicParsing
        }
        catch {
            throw "No checksum published for this release ($url.sha256)."
        }

        # The .sha256 is written by sha256sum/shasum as "<digest>  <filename>".
        $expected = ((Get-Content $sumPath -Raw).Trim() -split '\s+')[0]
        $actual   = (Get-FileHash -Path $binPath -Algorithm SHA256).Hash

        if ($actual -ine $expected) {
            throw "Checksum mismatch - the download is corrupt or has been tampered with. Nothing was installed."
        }

        # Copy into place only after verification, so a failure never leaves a half-installed binary.
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        $target = Join-Path $installDir 'sbxup.exe'
        Copy-Item -Path $binPath -Destination $target -Force

        Write-Host "Installed sbxup to $target"

        # Persist to the *user* PATH; never touch the machine-wide one, which needs admin.
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if ($userPath -notlike "*$installDir*") {
            $newPath = if ([string]::IsNullOrEmpty($userPath)) { $installDir } else { "$userPath;$installDir" }
            [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
            Write-Host ''
            Write-Host "Added $installDir to your user PATH. Open a new terminal for it to take effect."
        }
    }
    finally {
        Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-Sbxup
