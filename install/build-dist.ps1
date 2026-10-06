<#
.SYNOPSIS
    Builds the three release files into one folder: bridge-windows-amd64.exe,
    browser-bridge-extension.zip and install.ps1. The release workflow uses it, and so does
    test-install.ps1 when run locally.
#>
param(
    [string]$Out = 'dist',
    # The go executable, for machines where go is not on PATH.
    [string]$Go = 'go'
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$Out = [IO.Path]::GetFullPath((Join-Path (Get-Location) $Out))
New-Item -ItemType Directory -Force -Path $Out | Out-Null

function Invoke-Checked([string]$What, [scriptblock]$Command) {
    & $Command
    if ($LASTEXITCODE -ne 0) { throw "$What failed with exit code $LASTEXITCODE" }
}

$savedOs, $savedArch = $env:GOOS, $env:GOARCH
try {
    $env:GOOS, $env:GOARCH = 'windows', 'amd64'
    Invoke-Checked 'go build' {
        & $Go -C (Join-Path $root 'daemon') build -trimpath '-ldflags=-s -w' -o (Join-Path $Out 'bridge-windows-amd64.exe') ./cmd/bridge
    }
} finally {
    $env:GOOS, $env:GOARCH = $savedOs, $savedArch
}

$extensionRoot = Join-Path $root 'extension'
Invoke-Checked 'extension zip' { npm --prefix $extensionRoot run zip }
$version = (Get-Content -Raw -LiteralPath (Join-Path $extensionRoot 'package.json') | ConvertFrom-Json).version
Copy-Item -LiteralPath (Join-Path $extensionRoot ".output/browser-bridge-extension-$version-chrome.zip") -Destination (Join-Path $Out 'browser-bridge-extension.zip') -Force
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'install.ps1') -Destination $Out -Force

Get-ChildItem -LiteralPath $Out | Format-Table Name, Length
