<#
.SYNOPSIS
  Starts the development desktop with hot reload and isolated data.
.PARAMETER DataDir
  Data directory, relative to the repository or absolute (default: .orole-dev).
.PARAMETER RestartDaemon
  Stops this checkout's running orole-daemon first, so the daemon is rebuilt and started fresh. Without it a daemon
  that is already running keeps serving the old code. Daemon-owned terminals and running bot turns end.
#>
param([string]$DataDir = ".orole-dev", [switch]$RestartDaemon)
$ErrorActionPreference = "Stop"

$previousHome = $env:OROLE_HOME
Push-Location $PSScriptRoot
try {
    $env:OROLE_HOME = [System.IO.Path]::GetFullPath($DataDir)
    $null = Get-Command go -ErrorAction Stop
    $null = Get-Command bun -ErrorAction Stop

    if (-not (Test-Path "frontend/node_modules" -PathType Container)) {
        bun install --cwd frontend
        if ($LASTEXITCODE) { throw "frontend dependency installation failed" }
    }

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
