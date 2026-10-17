<#
.SYNOPSIS
  Replaces the installer MyGo built with one that first closes a running Orole OS (see installer-wrapper.nsi).
.PARAMETER Tag
  Also upload the wrapped installer to the (draft) GitHub release of this tag, replacing the one MyGo uploaded.
#>
param([string]$Installer = "", [string]$Tag = "")
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
if (-not $Installer) { $Installer = (Get-ChildItem "build\windows-amd64\*Setup*.exe" | Select-Object -First 1).FullName }
if (-not $Installer -or -not (Test-Path $Installer)) { throw "no installer to wrap in build\windows-amd64" }
$make = (Get-Command makensis -ErrorAction SilentlyContinue).Source
if (-not $make) {
  # The NSIS that `mygo build` downloads (os.UserCacheDir\mygo\nsis-<version>).
  $make = Get-ChildItem (Join-Path $env:LOCALAPPDATA "mygo\nsis-*") -Recurse -Filter makensis.exe -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty FullName
}
if (-not $make) { throw "makensis not found" }
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("orole-wrap-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$null = New-Item -ItemType Directory $tmp
try {
  $inner = Join-Path $tmp "inner.exe"
  $out = Join-Path $tmp "wrapped.exe"
  Copy-Item $Installer $inner
  & $make -V2 "/DINNER=$inner" "/DOUT=$out" "/DCLOSE=$(Join-Path $PSScriptRoot 'close-orole.ps1')" (Join-Path $PSScriptRoot "installer-wrapper.nsi")
  if ($LASTEXITCODE -or -not (Test-Path $out)) { throw "makensis failed" }
  Move-Item -Force $out $Installer
  Write-Host "wrapped $(Split-Path $Installer -Leaf) ($((Get-Item $Installer).Length) bytes)"
} finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
if ($Tag) {
  gh release upload $Tag $Installer --clobber
  if ($LASTEXITCODE) { throw "uploading the wrapped installer to $Tag failed" }
}
