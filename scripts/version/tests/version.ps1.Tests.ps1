# Tests for scripts/version/version.ps1
# Usage: Invoke-Pester scripts/version/tests/version.ps1.Tests.ps1
#
# Creates temporary git repos and verifies version.ps1 output.

BeforeAll {
    $Script:VersionPs1 = (Resolve-Path "$PSScriptRoot/../version.ps1").Path
}

function New-TestRepo {
    $tmp = New-Item -ItemType Directory -Path (Join-Path ([System.IO.Path]::GetTempPath()) "vtest-$([guid]::NewGuid().ToString('N').Substring(0,8))")
    Push-Location $tmp.FullName
    git init -q
    git config user.email "test@test.com"
    git config user.name "Test"
    return $tmp.FullName
}

function Remove-TestRepo([string]$Path) {
    Pop-Location
    Remove-Item -Recurse -Force $Path -ErrorAction SilentlyContinue
}

function Get-VersionOnly {
    param([string]$RepoPath, [hashtable]$ExtraArgs = @{})
    Push-Location $RepoPath
    try {
        $args_ = @('-VersionOnly')
        foreach ($k in $ExtraArgs.Keys) { $args_ += @("-$k", $ExtraArgs[$k]) }
        & pwsh -NoProfile -File $Script:VersionPs1 @args_
    } finally {
        Pop-Location
    }
}

function Get-VersionData {
    param([string]$RepoPath, [hashtable]$ExtraArgs = @{})
    Push-Location $RepoPath
    try {
        $args_ = @()
        foreach ($k in $ExtraArgs.Keys) { $args_ += @("-$k", $ExtraArgs[$k]) }
        $lines = & pwsh -NoProfile -File $Script:VersionPs1 @args_
        $data = @{}
        foreach ($line in $lines) {
            if ($line -match '^(\w+)=(.+)$') { $data[$Matches[1]] = $Matches[2] }
        }
        return $data
    } finally {
        Pop-Location
    }
}

Describe "version.ps1" {

    Describe "basic commit counting" {
        It "counts all commits as patch increment" {
            $repo = New-TestRepo
            try {
                "baseVersion: 1.0.0" | Set-Content version.yaml
                git add . ; git commit -q -m "first"
                git commit -q --allow-empty -m "second"
                git commit -q --allow-empty -m "third"

                $v = Get-VersionOnly -RepoPath $repo
                $v | Should -Be "1.0.3"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "base patch added to count" {
        It "adds baseVersion patch to commit count" {
            $repo = New-TestRepo
            try {
                "baseVersion: 2.3.5" | Set-Content version.yaml
                git add . ; git commit -q -m "first"
                git commit -q --allow-empty -m "second"

                $v = Get-VersionOnly -RepoPath $repo
                $v | Should -Be "2.3.7"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "baseCommitSha" {
        It "counts only commits after the specified SHA" {
            $repo = New-TestRepo
            try {
                "baseVersion: 0.1.0" | Set-Content version.yaml
                git add . ; git commit -q -m "first"
                git commit -q --allow-empty -m "second"
                $sha = (git rev-parse HEAD)

                git commit -q --allow-empty -m "third"
                git commit -q --allow-empty -m "fourth"

                @("baseVersion: 0.1.0", "baseCommitSha: $sha") | Set-Content version.yaml
                git add . ; git commit -q -m "update"

                $v = Get-VersionOnly -RepoPath $repo
                $v | Should -Be "0.1.3"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "override" {
        It "ignores version.yaml when override is set" {
            $repo = New-TestRepo
            try {
                "baseVersion: 1.0.0" | Set-Content version.yaml
                git add . ; git commit -q -m "first"
                git commit -q --allow-empty -m "second"

                $v = Get-VersionOnly -RepoPath $repo -ExtraArgs @{ Override = "5.0.0" }
                $v | Should -Be "5.0.2"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "custom config file" {
        It "reads from alternate config" {
            $repo = New-TestRepo
            try {
                "baseVersion: 1.0.0" | Set-Content version.yaml
                "baseVersion: 3.0.0" | Set-Content custom.yaml
                git add . ; git commit -q -m "first"

                $v = Get-VersionOnly -RepoPath $repo -ExtraArgs @{ ConfigFile = "custom.yaml" }
                $v | Should -Be "3.0.1"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "tag prefix" {
        It "uses custom prefix" {
            $repo = New-TestRepo
            try {
                "baseVersion: 1.0.0" | Set-Content version.yaml
                git add . ; git commit -q -m "first"

                $data = Get-VersionData -RepoPath $repo -ExtraArgs @{ TagPrefix = "release-" }
                $data['tag'] | Should -Be "release-1.0.1"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "path scoping" {
        It "counts only commits touching the given path" {
            $repo = New-TestRepo
            try {
                New-Item -ItemType Directory -Path "src/imageA" -Force | Out-Null
                New-Item -ItemType Directory -Path "src/imageB" -Force | Out-Null
                "baseVersion: 0.1.0" | Set-Content version.yaml
                "a" | Set-Content src/imageA/Dockerfile
                git add . ; git commit -q -m "init"

                "b" | Set-Content src/imageB/Dockerfile
                git add . ; git commit -q -m "add imageB"

                "a2" | Set-Content src/imageA/Dockerfile
                git add . ; git commit -q -m "update imageA"

                $vAll = Get-VersionOnly -RepoPath $repo
                $vA   = Get-VersionOnly -RepoPath $repo -ExtraArgs @{ Path = "src/imageA" }
                $vB   = Get-VersionOnly -RepoPath $repo -ExtraArgs @{ Path = "src/imageB" }

                $vAll | Should -Be "0.1.3"
                $vA   | Should -Be "0.1.2"
                $vB   | Should -Be "0.1.1"
            } finally { Remove-TestRepo $repo }
        }

        It "returns base version when no commits touch the path" {
            $repo = New-TestRepo
            try {
                New-Item -ItemType Directory -Path "src/empty" -Force | Out-Null
                "baseVersion: 0.5.0" | Set-Content version.yaml
                git add . ; git commit -q -m "init"
                git commit -q --allow-empty -m "empty"

                $v = Get-VersionOnly -RepoPath $repo -ExtraArgs @{ Path = "src/empty" }
                $v | Should -Be "0.5.0"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "path with baseCommitSha" {
        It "scopes both SHA range and path" {
            $repo = New-TestRepo
            try {
                New-Item -ItemType Directory -Path "src/app" -Force | Out-Null
                "baseVersion: 0.1.0" | Set-Content version.yaml
                "v1" | Set-Content src/app/file.txt
                git add . ; git commit -q -m "init"
                $sha = (git rev-parse HEAD)

                "v2" | Set-Content src/app/file.txt
                git add . ; git commit -q -m "update app"
                git commit -q --allow-empty -m "unrelated"

                @("baseVersion: 0.1.0", "baseCommitSha: $sha") | Set-Content version.yaml
                git add . ; git commit -q -m "set sha"

                $vPath   = Get-VersionOnly -RepoPath $repo -ExtraArgs @{ Path = "src/app" }
                $vGlobal = Get-VersionOnly -RepoPath $repo

                $vPath   | Should -Be "0.1.1"
                $vGlobal | Should -Be "0.1.3"
            } finally { Remove-TestRepo $repo }
        }
    }

    Describe "output keys" {
        It "includes all expected keys in full output" {
            $repo = New-TestRepo
            try {
                "baseVersion: 1.0.0" | Set-Content version.yaml
                git add . ; git commit -q -m "first"

                $data = Get-VersionData -RepoPath $repo
                $data.Keys | Should -Contain "version"
                $data.Keys | Should -Contain "major"
                $data.Keys | Should -Contain "minor"
                $data.Keys | Should -Contain "patch"
                $data.Keys | Should -Contain "tag"
                $data.Keys | Should -Contain "sha"
                $data.Keys | Should -Contain "short_sha"
                $data.Keys | Should -Contain "branch"
                $data.Keys | Should -Contain "build_date_utc"
            } finally { Remove-TestRepo $repo }
        }
    }
}
