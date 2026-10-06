<#
.SYNOPSIS
    Tests install.ps1 against release files built by build-dist.ps1. It runs the installer the way
    users do (iex) in a fresh shell, with a temporary BRIDGE_HOME, a free port, -NoPath and -NoWait,
    so the real installation on the machine is never touched.
#>
param(
    [Parameter(Mandatory = $true)][string]$Artifacts,
    # The shell that runs the installer: powershell (Windows PowerShell 5.1) or pwsh (7).
    [ValidateSet('powershell', 'pwsh')][string]$Shell = 'powershell'
)

$ErrorActionPreference = 'Stop'
$Artifacts = (Resolve-Path -LiteralPath $Artifacts).Path
$installer = Join-Path $Artifacts 'install.ps1'
$bridgeHome = Join-Path ([IO.Path]::GetTempPath()) ('bridge-install-test-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
$bin = Join-Path $bridgeHome 'bin\bridge.exe'

$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$listener.Start()
$addr = "127.0.0.1:$($listener.LocalEndpoint.Port)"
$listener.Stop()
New-Item -ItemType Directory -Path $bridgeHome | Out-Null
Set-Content -LiteralPath (Join-Path $bridgeHome 'config.json') -Value "{`"addr`": `"$addr`"}" -Encoding ASCII

# The bridge commands this script runs itself (status, stop) must find the test daemon too.
$savedHome = $env:BRIDGE_HOME
$env:BRIDGE_HOME = $bridgeHome

function Invoke-Installer([string]$Arguments) {
    $command = "`$env:BRIDGE_INSTALL_BASE = '$Artifacts'; " +
        "iex ('& {' + (Get-Content -Raw -LiteralPath '$installer') + '} $Arguments')"
    & $Shell -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command $command | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "installer $Arguments exited with $LASTEXITCODE" }
}

function Assert([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw "FAIL: $Message" }
    Write-Host "PASS: $Message" -ForegroundColor Green
}

function Get-DaemonPid {
    $out = & $bin status
    if ($LASTEXITCODE -ne 0) { return 0 }
    return [int]($out | Out-String | ConvertFrom-Json).pid
}

function Test-DaemonUp {
    try {
        Invoke-WebRequest -Uri "http://$addr/status" -UseBasicParsing -TimeoutSec 2 | Out-Null
        return $true
    } catch {
        return $false
    }
}

try {
    Write-Host "== fresh install ($Shell)"
    Invoke-Installer '-NoPath -NoWait'
    Assert (Test-Path -LiteralPath $bin) 'bridge.exe is installed'
    $manifest = Get-Content -Raw -LiteralPath (Join-Path $bridgeHome 'extension\manifest.json') | ConvertFrom-Json
    Assert ($null -ne $manifest.PSObject.Properties['key']) 'the extension manifest carries its key'
    $version = (& $bin version | Out-String).Trim()
    Assert ($version -eq $manifest.version) "bridge $version matches the extension $($manifest.version)"
    $first = Get-DaemonPid
    Assert ($first -gt 0) "the daemon runs on $addr (pid $first)"

    Write-Host '== reinstall while the daemon runs'
    Invoke-Installer '-NoPath -NoWait'
    $second = Get-DaemonPid
    Assert ($second -gt 0 -and $second -ne $first) "the daemon was replaced (pid $first, then $second)"

    Write-Host '== uninstall keeps files bridge did not create'
    Set-Content -LiteralPath (Join-Path $bridgeHome 'notes.txt') -Value 'not ours'
    Invoke-Installer '-Uninstall'
    Assert (-not (Test-DaemonUp)) 'the daemon is stopped'
    foreach ($name in 'bin', 'extension', 'logs', 'config.json', 'daemon.addr') {
        Assert (-not (Test-Path -LiteralPath (Join-Path $bridgeHome $name))) "$name is removed"
    }
    Assert (Test-Path -LiteralPath (Join-Path $bridgeHome 'notes.txt')) 'a file bridge did not create is kept, and so is its folder'

    Write-Host '== uninstall removes the home folder once it is empty'
    Remove-Item -LiteralPath (Join-Path $bridgeHome 'notes.txt')
    Invoke-Installer '-Uninstall'
    Assert (-not (Test-Path -LiteralPath $bridgeHome)) 'the home folder is removed'
} finally {
    # Cleanup must not throw: that would hide the failure that got us here.
    try {
        if (Test-Path -LiteralPath $bin) { & $bin stop | Out-Null }
        if (Test-Path -LiteralPath $bridgeHome) { Remove-Item -LiteralPath $bridgeHome -Recurse -Force }
    } catch {
        Write-Warning "cleanup of $bridgeHome failed: $_"
    }
    $env:BRIDGE_HOME = $savedHome
}
Write-Host "All installer tests passed ($Shell)." -ForegroundColor Green
