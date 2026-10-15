<#
.SYNOPSIS
  Starts the development desktop with hot reload and isolated data.
.PARAMETER DataDir
  Profile name or relative path under the git-ignored profiles\ folder of this checkout, or an absolute data
  directory (default: dev, i.e. profiles\dev).
.PARAMETER RestartDaemon
  Stops this checkout's running orole-daemon first, so the daemon is rebuilt and started fresh. Without it a daemon
  that is already running keeps serving the old code. Daemon-owned terminals and running bot turns end.
#>
param([string]$DataDir = "dev", [switch]$RestartDaemon)
$ErrorActionPreference = "Stop"

$previousHome = $env:OROLE_HOME
Push-Location $PSScriptRoot
try {
    # GetFullPath resolves against the process directory (where the shell started), not Push-Location, so
    # anchor relative names to this checkout's profiles folder first.
    if (-not [System.IO.Path]::IsPathRooted($DataDir)) { $DataDir = Join-Path $PSScriptRoot "profiles\$DataDir" }
    $env:OROLE_HOME = [System.IO.Path]::GetFullPath($DataDir)
    $null = Get-Command go -ErrorAction Stop
    $null = Get-Command bun -ErrorAction Stop

    # Always sync: a branch switch can add dependencies to an existing node_modules (a no-op when up to date).
    bun install --cwd frontend
    if ($LASTEXITCODE) { throw "frontend dependency installation failed" }

    if ($RestartDaemon) {
        $stale = Get-CimInstance Win32_Process -Filter "Name='orole-daemon.exe'" |
            Where-Object { $_.ExecutablePath -like "$PSScriptRoot*" -and $_.CommandLine -notmatch "mcp-gateway" }
        foreach ($daemon in $stale) { Write-Host "Stopping daemon $($daemon.ProcessId)"; Stop-Process -Id $daemon.ProcessId -Force }
    }

    Write-Host "Starting Orole OS with data in $env:OROLE_HOME"
    Write-Host "Existing daemons are not restarted (use -RestartDaemon after changing daemon code); daemon-owned terminals survive closing the desktop."
    # MyGo builds and bundles the daemon before the desktop launches it.
    go tool mygo dev
    if ($LASTEXITCODE) { throw "mygo dev failed (exit $LASTEXITCODE)" }
} finally {
    $env:OROLE_HOME = $previousHome
    Pop-Location
}
