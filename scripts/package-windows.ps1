#requires -Version 5.1
[CmdletBinding()]
param(
    [ValidateSet('amd64','arm64')][string]$Architecture = 'amd64',
    [ValidatePattern('^(v?[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?)$')][string]$Version = 'v0.3.0'
)
$ErrorActionPreference = 'Stop'
$sourceRoot = Split-Path -Parent $PSScriptRoot
$savedGoos = $env:GOOS
$savedGoarch = $env:GOARCH
$savedCgo = $env:CGO_ENABLED
Push-Location $sourceRoot
try {
    # Source generation executes on the host before selecting the binary target.
    $env:GOOS = $null
    $env:GOARCH = $null
    & go run ./internal/cmd/bundle
    if ($LASTEXITCODE -ne 0) { throw 'Framework bundle generation failed.' }
    # A UAC/compatibility manifest tells Program Compatibility Assistant that
    # Setup is a current per-user installer, preventing its legacy-installer
    # heuristic from showing a false "might not have run correctly" dialog.
    $resource = Join-Path $sourceRoot "cmd\setup\graftsetup_windows_$Architecture.syso"
    & go run github.com/akavel/rsrc@v0.10.2 -manifest cmd/setup/graftsetup.manifest -arch $Architecture -o $resource
    if ($LASTEXITCODE -ne 0) { throw 'Setup manifest resource generation failed.' }
    $env:GOOS = 'windows'
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = '0'
    $destination = Join-Path $sourceRoot "publish\graft-windows-$Architecture"
    New-Item -ItemType Directory -Force -Path $destination | Out-Null
    $binary = Join-Path $destination 'graft.exe'
    & go build -trimpath -ldflags "-X github.com/spidermeaow/graft-framework/internal/cli.Version=$Version" -o $binary ./cmd/graft
    if ($LASTEXITCODE -ne 0) { throw 'CLI build failed.' }
    $payload = Join-Path $sourceRoot 'cmd\setup\payload'
    New-Item -ItemType Directory -Force -Path $payload | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $payload 'graft.bin') -Force
    $setup = Join-Path $destination 'Setup.exe'
    & go build -trimpath -tags graftsetup -ldflags '-H=windowsgui' -o $setup ./cmd/setup
    if ($LASTEXITCODE -ne 0) { throw 'Setup build failed.' }
    $setupText = [Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($setup))
    if (-not $setupText.Contains('requestedExecutionLevel') -or -not $setupText.Contains('Graft.Setup')) {
        throw 'Setup manifest was not embedded.'
    }
    Get-FileHash -LiteralPath $binary -Algorithm SHA256
    Get-FileHash -LiteralPath $setup -Algorithm SHA256
    Write-Host "Standalone installer: $setup"
    Write-Host "Send this file: $binary"
    Write-Host 'On the destination Windows machine: .\graft.exe install'
} finally {
    $env:GOOS = $savedGoos
    $env:GOARCH = $savedGoarch
    $env:CGO_ENABLED = $savedCgo
    Pop-Location
}
