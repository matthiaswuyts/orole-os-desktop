<#
.SYNOPSIS
  Starts the development desktop with hot reload and isolated data.
.PARAMETER DataDir
  Data directory, relative to the repository or absolute (default: .orole-dev).
#>
param([string]$DataDir = ".orole-dev")
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

    Write-Host "Starting Orole OS with data in $env:OROLE_HOME"
    Write-Host "Existing daemons are not restarted; daemon-owned terminals survive closing the desktop."
    # MyGo builds and bundles the daemon before the desktop launches it.
    go tool mygo dev
    if ($LASTEXITCODE) { throw "mygo dev failed (exit $LASTEXITCODE)" }
} finally {
    $env:OROLE_HOME = $previousHome
    Pop-Location
}
