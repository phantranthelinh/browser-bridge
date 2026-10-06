<#
.SYNOPSIS
    Fails unless a release tag, the daemon (protocol.Version) and the extension (package.json)
    carry the same version.
#>
param([Parameter(Mandatory = $true)][string]$Tag)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

$protocol = Get-Content -Raw -LiteralPath (Join-Path $root 'daemon/internal/protocol/protocol.go')
if ($protocol -notmatch '(?m)^\s*Version\s*=\s*"([^"]+)"') { throw 'Version not found in protocol.go' }
$daemon = $Matches[1]
$extension = (Get-Content -Raw -LiteralPath (Join-Path $root 'extension/package.json') | ConvertFrom-Json).version
$version = $Tag -replace '^v', ''

if ($daemon -ne $version -or $extension -ne $version) {
    throw "version mismatch: tag $Tag, daemon $daemon, extension $extension"
}
Write-Host "Version $version matches the tag, the daemon and the extension."
