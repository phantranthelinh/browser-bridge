#Requires -Version 5.1
<#
.SYNOPSIS
    Installs browser-bridge for the current user: the bridge daemon and the Chrome extension.

.DESCRIPTION
    irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex

    Run with -Help for the options. Design: docs/superpowers/specs/2026-10-06-installer-design.md

    Keep this file ASCII-only: Windows PowerShell 5.1 reads a UTF-8 file without a BOM as ANSI, so
    any other character would come out differently from a file than from irm.
#>
[CmdletBinding()]
param(
    [switch]$Help,
    [switch]$NoStart,
    [switch]$NoPath,
    [switch]$NoWait,
    [switch]$Uninstall
)

Set-StrictMode -Version 3.0
$ErrorActionPreference = 'Stop'
# The progress bar makes Invoke-WebRequest many times slower in Windows PowerShell 5.1.
$ProgressPreference = 'SilentlyContinue'
# Windows PowerShell 5.1 may not offer TLS 1.2 by default, and GitHub requires it.
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo = 'phantranthelinh/browser-bridge'
$ReleasesUrl = "https://github.com/$Repo/releases"
$BridgeHome = if ($env:BRIDGE_HOME) { $env:BRIDGE_HOME } else { Join-Path $env:USERPROFILE '.browser-bridge' }
$BinDir = Join-Path $BridgeHome 'bin'
$BinPath = Join-Path $BinDir 'bridge.exe'
$ExtDir = Join-Path $BridgeHome 'extension'
$WaitSeconds = 180

function Write-Step([string]$Message) { Write-Host "==> $Message" }
function Write-Ok([string]$Message) { Write-Host "$([char]0x2713) $Message" -ForegroundColor Green }
function Write-Warn([string]$Message) { Write-Host "! $Message" -ForegroundColor Yellow }

function Show-Help {
    Write-Host @"
browser-bridge installer

  irm $ReleasesUrl/latest/download/install.ps1 | iex
      install, or update to the latest release
  iex "& { `$(irm $ReleasesUrl/latest/download/install.ps1) } -Uninstall"
      uninstall

Options
  -NoStart     install, but do not start the daemon
  -NoPath      do not add bridge to PATH
  -NoWait      do not wait for the extension to connect
  -Uninstall   stop the daemon and remove what the installer created

Environment
  BRIDGE_VERSION        install this version (e.g. 0.1.0) instead of the latest
  BRIDGE_HOME           install folder (default: %USERPROFILE%\.browser-bridge)
  BRIDGE_INSTALL_BASE   take the release files from this URL or local folder (for testing)
"@
}

function Get-Source {
    if ($env:BRIDGE_INSTALL_BASE) { return $env:BRIDGE_INSTALL_BASE.TrimEnd('/', '\') }
    if ($env:BRIDGE_VERSION) { return "$ReleasesUrl/download/v$($env:BRIDGE_VERSION.TrimStart('v'))" }
    return "$ReleasesUrl/latest/download"
}

# Fetches one release file to $Dest, from a URL or, when testing, from a local folder.
function Get-ReleaseFile([string]$Name, [string]$Dest) {
    $source = Get-Source
    if (Test-Path -LiteralPath $source -PathType Container) {
        Copy-Item -LiteralPath (Join-Path $source $Name) -Destination $Dest -Force
        return
    }
    $url = "$source/$Name"
    for ($i = 1; $i -le 3; $i++) {
        try {
            Invoke-WebRequest -Uri $url -OutFile $Dest -UseBasicParsing -TimeoutSec 120
            return
        } catch {
            if ($i -eq 3) { throw "cannot download $url ($($_.Exception.Message))" }
            Write-Warn "download failed, retrying ($i/3)..."
            Start-Sleep -Seconds 5
        }
    }
}

# A just-stopped daemon keeps bridge.exe and its log locked for a moment, and Chrome may hold
# extension files briefly; a few retries cover both.
function Invoke-WithRetry([scriptblock]$Action, [string]$What) {
    for ($i = 1; ; $i++) {
        try {
            & $Action
            return
        } catch {
            if ($i -ge 10) { throw "$What failed: $($_.Exception.Message)" }
            Start-Sleep -Milliseconds 500
        }
    }
}

function Stop-Daemon {
    if (-not (Test-Path -LiteralPath $BinPath)) { return }
    Write-Step 'Stopping the running daemon...'
    & $BinPath stop | Out-Host
    if ($LASTEXITCODE -ne 0) { Write-Warn "bridge stop exited with $LASTEXITCODE" }
}

function Install-Binary {
    Write-Step "Downloading bridge.exe from $(Get-Source)"
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $download = "$BinPath.download"
    Get-ReleaseFile 'bridge-windows-amd64.exe' $download
    Invoke-WithRetry { Move-Item -LiteralPath $download -Destination $BinPath -Force } 'replacing bridge.exe'
    Write-Ok "Installed $BinPath ($(& $BinPath version))"
}

function Install-Extension {
    Write-Step 'Downloading the extension...'
    $zip = Join-Path $BridgeHome 'extension.download.zip'
    $staging = Join-Path $BridgeHome 'extension.new'
    Get-ReleaseFile 'browser-bridge-extension.zip' $zip
    if (Test-Path -LiteralPath $staging) { Remove-Item -LiteralPath $staging -Recurse -Force }
    Expand-Archive -LiteralPath $zip -DestinationPath $staging -Force
    Remove-Item -LiteralPath $zip -Force
    if (-not (Test-Path -LiteralPath (Join-Path $staging 'manifest.json'))) { throw 'the extension zip has no manifest.json' }
    # Always the same folder: an extension loaded unpacked from here keeps working after updates.
    if (Test-Path -LiteralPath $ExtDir) {
        Invoke-WithRetry { Remove-Item -LiteralPath $ExtDir -Recurse -Force } "replacing $ExtDir (close Chrome and retry if this keeps failing)"
    }
    Move-Item -LiteralPath $staging -Destination $ExtDir
    Write-Ok "Extension files in $ExtDir"
}

function Test-SamePath([string]$A, [string]$B) { return $A.TrimEnd('\') -ieq $B.TrimEnd('\') }

# Tells Explorer, and so every program started afterwards, that the user's PATH changed.
function Send-EnvironmentChanged {
    if (-not ('BridgeInstaller.Native' -as [type])) {
        Add-Type -Namespace BridgeInstaller -Name Native -MemberDefinition @'
[DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
'@
    }
    $result = [UIntPtr]::Zero
    # HWND_BROADCAST, WM_SETTINGCHANGE, SMTO_ABORTIFHUNG
    [void][BridgeInstaller.Native]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
}

# Edits the user's PATH in the registry, keeping its value type. [Environment]::SetEnvironmentVariable
# would expand the %VARIABLES% in every other entry and save them as plain strings.
function Update-UserPath([scriptblock]$Change) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try {
        $raw = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $kind = if ($key.GetValueNames() -contains 'Path') { $key.GetValueKind('Path') } else { [Microsoft.Win32.RegistryValueKind]::ExpandString }
        $entries = @($raw -split ';' | Where-Object { $_ })
        $updated = @(& $Change $entries)
        if (($updated -join ';') -eq ($entries -join ';')) { return $false }
        $key.SetValue('Path', ($updated -join ';'), $kind)
    } finally {
        $key.Close()
    }
    Send-EnvironmentChanged
    return $true
}

function Add-ToPath {
    $added = Update-UserPath {
        param($entries)
        if (@($entries | Where-Object { Test-SamePath $_ $BinDir }).Count -gt 0) { $entries } else { @($entries) + $BinDir }
    }
    if (-not @($env:Path -split ';' | Where-Object { Test-SamePath $_ $BinDir })) { $env:Path = "$env:Path;$BinDir" }
    if ($added) { Write-Ok "Added $BinDir to your PATH (new terminals see it)" }
}

function Remove-FromPath {
    $removed = Update-UserPath {
        param($entries)
        @($entries | Where-Object { -not (Test-SamePath $_ $BinDir) })
    }
    $env:Path = (@($env:Path -split ';' | Where-Object { $_ -and -not (Test-SamePath $_ $BinDir) })) -join ';'
    if ($removed) { Write-Ok "Removed $BinDir from your PATH" }
}

function Get-DaemonStatus {
    $out = & $BinPath status
    if ($LASTEXITCODE -ne 0) { return $null }
    return ($out | Out-String | ConvertFrom-Json)
}

# StrictMode turns reading a missing property into an error, and /status omits empty fields.
function Get-Prop($Object, [string]$Name) {
    $p = $Object.PSObject.Properties[$Name]
    if ($p) { return $p.Value }
    return $null
}

function Start-Daemon {
    Write-Step 'Starting the daemon...'
    & $BinPath start | Out-Host
    if ($LASTEXITCODE -ne 0) {
        Write-Warn "The daemon did not start. See $(Join-Path $BridgeHome 'logs\daemon.log'), then run: bridge start"
        return $false
    }
    return $true
}

function Show-LoadUnpackedSteps {
    Write-Host ''
    Write-Host 'Load the extension in Chrome or Edge (once):' -ForegroundColor Cyan
    Write-Host '  1. Open chrome://extensions (Edge: edge://extensions)'
    Write-Host '  2. Turn on Developer mode (top right)'
    Write-Host "  3. Click Load unpacked and choose $ExtDir"
    try {
        Set-Clipboard -Value $ExtDir
        Write-Host '     (that path is on your clipboard)'
    } catch {
        # no clipboard, e.g. on a CI runner
    }
    Write-Host ''
}

# True when an older or newer build of the extension is loaded: it reached the daemon, which
# turned it away. Reload picks up the files the installer just replaced.
function Test-OutdatedExtension($Status) {
    $extensionProtocol = Get-Prop $Status.extension 'protocolVersion'
    return [bool]($extensionProtocol -and $extensionProtocol -ne $Status.protocolVersion)
}

function Write-ReloadExtension($Status) {
    Write-Warn "The loaded extension speaks protocol $(Get-Prop $Status.extension 'protocolVersion'), the daemon $($Status.protocolVersion): click Reload on Browser Bridge in chrome://extensions."
}

function Connect-Extension([switch]$Wait) {
    $st = Get-DaemonStatus
    if (-not $st) { return }
    if (Get-Prop $st.extension 'connected') {
        Write-Ok 'The extension is connected.'
        Write-Host '  After an update, click Reload on Browser Bridge in chrome://extensions.'
        return
    }
    if (Test-OutdatedExtension $st) {
        Write-ReloadExtension $st
        if (-not $Wait) { return }
    } else {
        # Shown whether or not we wait: with -NoWait this is the only place the user learns the steps.
        Show-LoadUnpackedSteps
    }
    if (-not $Wait) {
        Write-Host '  Check that it connected with: bridge status'
        return
    }
    Write-Step "Waiting up to $($WaitSeconds / 60) minutes for the extension to connect (Ctrl+C to skip)..."
    $warned = Test-OutdatedExtension $st
    $deadline = (Get-Date).AddSeconds($WaitSeconds)
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 2
        $st = Get-DaemonStatus
        if (-not $st) { continue }
        if (Get-Prop $st.extension 'connected') {
            Write-Ok 'The extension is connected.'
            return
        }
        # An installed extension reconnects on its own within 30s of the daemon restarting.
        if (-not $warned -and (Test-OutdatedExtension $st)) {
            Write-ReloadExtension $st
            $warned = $true
        }
    }
    Write-Warn 'The extension has not connected yet. Do the steps above, then check with: bridge status'
}

function Write-Summary {
    Write-Host ''
    Write-Ok "browser-bridge $(& $BinPath version) is installed in $BridgeHome"
    Write-Host "  Extension folder (Load unpacked once): $ExtDir"
    Write-Host '  bridge status   shows the daemon and the extension'
    Write-Host '  bridge start    starts the daemon again after a reboot'
    if ($NoPath) { Write-Host "  bridge is not on PATH (-NoPath): run $BinPath" }
    Write-Host '  Any program running as you can drive this browser through bridge.' -ForegroundColor Yellow
}

function Install-Bridge {
    Write-Step "Installing browser-bridge into $BridgeHome"
    New-Item -ItemType Directory -Force -Path $BridgeHome | Out-Null
    Stop-Daemon
    Install-Binary
    Install-Extension
    if (-not $NoPath) { Add-ToPath }
    if (-not $NoStart) {
        if (Start-Daemon) { Connect-Extension -Wait:(-not $NoWait) }
    }
    Write-Summary
}

function Uninstall-Bridge {
    Write-Step "Uninstalling browser-bridge from $BridgeHome"
    Stop-Daemon
    Remove-FromPath
    # Only what bridge creates: BRIDGE_HOME may point at a folder that also holds other files.
    foreach ($name in 'bin', 'extension', 'extension.new', 'extension.download.zip', 'logs', 'artifacts', 'config.json', 'daemon.pid', 'daemon.addr') {
        $path = Join-Path $BridgeHome $name
        if (Test-Path -LiteralPath $path) {
            Invoke-WithRetry { Remove-Item -LiteralPath $path -Recurse -Force } "removing $path"
        }
    }
    if ((Test-Path -LiteralPath $BridgeHome) -and -not (Get-ChildItem -LiteralPath $BridgeHome -Force | Select-Object -First 1)) {
        Remove-Item -LiteralPath $BridgeHome -Force
    }
    Write-Ok 'browser-bridge is uninstalled.'
    Write-Host '  Remove the extension too: chrome://extensions > Browser Bridge > Remove'
}

if ($Help) {
    Show-Help
} elseif ($Uninstall) {
    Uninstall-Bridge
} else {
    Install-Bridge
}
