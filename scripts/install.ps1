#requires -Version 5.1
<#
.SYNOPSIS
Build and install Graft for the current Windows user from this checkout.
.DESCRIPTION
Installs to LOCALAPPDATA\Graft\bin and adds it to the user's PATH without admin.
Use -NoPath to install only the executable without changing PATH.
#>
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Graft\bin'),
    [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'This installer is for Windows.' }
Get-Command go -ErrorAction Stop | Out-Null
$sourceRoot = Split-Path -Parent $PSScriptRoot
$destination = [System.IO.Path]::GetFullPath($InstallDir)
if ($destination.Contains(';')) { throw 'InstallDir cannot contain a PATH separator (;).' }
$binary = Join-Path $destination 'graft.exe'
New-Item -ItemType Directory -Path $destination -Force | Out-Null

# Build for this machine even if the caller previously configured cross-compilation.
$savedGoos = $env:GOOS
$savedGoarch = $env:GOARCH
$savedCgo = $env:CGO_ENABLED
Push-Location $sourceRoot
try {
    $env:GOOS = 'windows'
    $hostArch = & go env GOHOSTARCH
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine Go host architecture.' }
    $env:GOARCH = $hostArch.Trim()
    $env:CGO_ENABLED = '0'
    # Local framework development uses graft new --framework explicitly.
    & go build -o $binary ./cmd/graft
    if ($LASTEXITCODE -ne 0) { throw 'Graft build failed; PATH was not changed.' }
    & $binary version
    if ($LASTEXITCODE -ne 0) { throw 'Installed Graft failed verification; PATH was not changed.' }
} finally {
    $env:GOOS = $savedGoos
    $env:GOARCH = $savedGoarch
    $env:CGO_ENABLED = $savedCgo
    Pop-Location
}

function Test-PathEntry([string]$PathValue, [string]$Directory) {
    foreach ($entry in ($PathValue -split ';')) {
        if ([string]::IsNullOrWhiteSpace($entry)) { continue }
        $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
        if ([string]::Equals($expanded, $Directory.TrimEnd('\', '/'), [StringComparison]::OrdinalIgnoreCase)) { return $true }
    }
    return $false
}

if (-not $NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (Test-PathEntry $userPath $destination)) {
        $updatedPath = $destination
        if (-not [string]::IsNullOrEmpty($userPath)) { $updatedPath += ';' + $userPath }
        [Environment]::SetEnvironmentVariable('Path', $updatedPath, 'User')
    }
    if (-not (Test-PathEntry $env:Path $destination)) { $env:Path = $destination + ';' + $env:Path }
    $resolved = Get-Command graft -CommandType Application -ErrorAction SilentlyContinue
    if ($resolved -and -not [string]::Equals($resolved.Source, $binary, [StringComparison]::OrdinalIgnoreCase)) {
        Write-Warning "Another graft executable appears first on PATH: $($resolved.Source). Installed executable: $binary"
    }
}

Write-Host "Installed: $binary"
if (-not $NoPath) {
    Write-Host 'Run: graft version; graft new my-api'
    Write-Host 'Restart already-open CMD/terminal apps to inherit the updated user PATH.'
}
