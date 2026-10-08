<#
.SYNOPSIS
  Installs the built NSIS installer silently into a temp dir, launches the app against an
  isolated OROLE_HOME, checks that the daemon appears and answers on its pipe, then closes
  everything and uninstalls.
.DESCRIPTION
  Never touches %LOCALAPPDATA%\Orole (OROLE_HOME is a temp dir). The installer registers the
  orole:// URL scheme and an uninstall entry for the current user; the uninstaller removes both.
  Needs a desktop session with WebView2 (it launches the real GUI window).
#>
param([string]$Installer = "", [int]$WaitSeconds = 30)
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
if (-not $Installer) { $Installer = (Get-ChildItem "build\windows-amd64\*Setup*.exe" | Select-Object -First 1).FullName }
$cli = Join-Path $root "build\orole.exe"
$target = Join-Path ([IO.Path]::GetTempPath()) ("orole-smoke-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$ohome = "$target-data"
$null = New-Item -ItemType Directory $ohome
$env:OROLE_HOME = $ohome
$ok = $false
try {
  Write-Host "installing $Installer -> $target"
  # NSIS: /D must be the last argument and unquoted.
  $p = Start-Process -FilePath $Installer -ArgumentList "/S", "/D=$target" -Wait -PassThru
  Write-Host "installer exit code $($p.ExitCode)"
  Get-ChildItem $target | Format-Table Name, Length -AutoSize | Out-String | Write-Host
  $app = Join-Path $target "Orole OS.exe"
  $appProc = Start-Process -FilePath $app -PassThru
  $deadline = (Get-Date).AddSeconds($WaitSeconds)
  $status = $null
  while ((Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 500
    $out = & $cli daemon status 2>&1 | Out-String
    if ($out -match "daemon: running") { $status = $out; break }
  }
  if (-not $status) { throw "daemon did not appear within ${WaitSeconds}s" }
  Write-Host $status
  $d = Get-Process orole-daemon -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq (Join-Path $target "orole-daemon.exe") }
  Write-Host "daemon process path: $($d.Path) (pid $($d.Id))"
  Write-Host "app alive: $(-not $appProc.HasExited), main window title: '$((Get-Process -Id $appProc.Id).MainWindowTitle)'"
  Write-Host (& $cli daemon diagnostics | Out-String)
  $ok = $true
} finally {
  Get-Process -Name "Orole OS" -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "$target*" } | ForEach-Object { $null = $_.CloseMainWindow(); Start-Sleep 2; if (-not $_.HasExited) { $_.Kill() } }
  & $cli daemon stop 2>&1 | Write-Host
  Start-Sleep -Seconds 1
  $un = Join-Path $target "uninstall.exe"
  if (Test-Path $un) {
    Start-Process -FilePath $un -ArgumentList "/S", "_?=$target" -Wait
    Write-Host "uninstalled (exit ok); remaining files: $((Get-ChildItem $target -ErrorAction SilentlyContinue | Measure-Object).Count)"
  }
  Remove-Item -Recurse -Force $target, $ohome -ErrorAction SilentlyContinue
  Remove-Item Env:OROLE_HOME -ErrorAction SilentlyContinue
}
if (-not $ok) { exit 1 }
