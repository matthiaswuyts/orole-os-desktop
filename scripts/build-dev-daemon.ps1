<#
.SYNOPSIS
  Builds build\orole-sandbox-setup.exe and then build\orole-daemon.exe with the helper's SHA-256 embedded, for
  `go tool mygo dev` (mygo.json devCommand). scripts/build.ps1 does the same for release builds.
.DESCRIPTION
  The daemon starts the setup helper elevated (one UAC prompt) and refuses any file whose SHA-256 differs from
  the one embedded here (sandbox.SetupHelperSHA256), because the folder it sits in is writable by the user.
#>
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)
$null = New-Item -ItemType Directory -Force build
go build -ldflags=-H=windowsgui -o build\orole-sandbox-setup.exe ./cmd/orole-sandbox-setup
if ($LASTEXITCODE) { throw "sandbox setup helper build failed" }
# .NET instead of Get-FileHash: mygo dev runs this in Windows PowerShell 5.1, which cannot load that cmdlet when
# started from PowerShell 7 (it inherits 7's PSModulePath).
$helperSHA = -join ([System.Security.Cryptography.SHA256]::Create().ComputeHash([System.IO.File]::ReadAllBytes((Resolve-Path build\orole-sandbox-setup.exe).Path)) | ForEach-Object { $_.ToString("x2") })
go build -ldflags "-X github.com/rayorole/orole-os-desktop/internal/sandbox.SetupHelperSHA256=$helperSHA" -o build\orole-daemon.exe ./cmd/daemon
if ($LASTEXITCODE) { throw "daemon build failed" }
