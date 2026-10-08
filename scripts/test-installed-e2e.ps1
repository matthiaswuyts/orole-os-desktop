#Requires -Version 7.2
<#
.SYNOPSIS
Runs Playwright against an already installed Orole OS using an isolated daemon home and WebView2 profile.
.DESCRIPTION
Does not install/uninstall software, mock the app, reuse a user's app, or close processes by name.
Requires explicit opt-in and an interactive Windows desktop with WebView2. Test artifacts remain in
frontend/test-results/native and frontend/playwright-report/native. Temporary app data is removed.
#>
param(
  [Parameter(Mandatory)][string]$AppPath,
  [Parameter(Mandatory)][string]$CliPath,
  [switch]$AllowIsolatedHome,
  [ValidateRange(1024, 65535)][int]$Port = 9227,
  [ValidateRange(10, 180)][int]$WaitSeconds = 60
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'Installed WebView2 acceptance requires Windows.' }
if (-not $AllowIsolatedHome) { throw 'Pass -AllowIsolatedHome to permit mutations in a newly created temporary home only.' }
$app = (Resolve-Path -LiteralPath $AppPath).Path
$cli = (Resolve-Path -LiteralPath $CliPath).Path
if ([IO.Path]::GetFileName($app) -ne 'Orole OS.exe') { throw 'AppPath must name the installed Orole OS.exe, not a development server.' }
$daemon = Join-Path (Split-Path $app -Parent) 'orole-daemon.exe'
if (-not (Test-Path -LiteralPath $daemon -PathType Leaf)) { throw "Installed daemon is missing: $daemon" }
# The application owns a per-user single-instance lock. Never forward into or close a user's GUI.
if (Get-Process -Name 'Orole OS', 'orole-os' -ErrorAction SilentlyContinue) { throw 'Quit Orole OS yourself before running the isolated installed-app suite.' }
$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $Port)
try { $listener.Start() } finally { $listener.Stop() }
$root = Split-Path $PSScriptRoot -Parent
$runRoot = Join-Path ([IO.Path]::GetTempPath()) ('orole-cdp-' + [guid]::NewGuid().ToString('N'))
$dataHome = Join-Path $runRoot 'data'
$profile = Join-Path $runRoot 'webview2'
$null = New-Item -ItemType Directory -Path $dataHome, $profile
$endpoint = "http://127.0.0.1:$Port/"
$token = [guid]::NewGuid().ToString('N')
$manifest = Join-Path $runRoot 'launch.json'
$envNames = @('OROLE_HOME', 'WEBVIEW2_USER_DATA_FOLDER', 'WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS', 'OROLE_E2E_ALLOW_ISOLATED_HOME', 'OROLE_E2E_CDP_ENDPOINT', 'OROLE_E2E_MANIFEST')
$oldEnv = @{}
foreach ($name in $envNames) { $oldEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
$appProcess = $null
$daemonProcess = $null
$exitCode = 1
try {
  $env:OROLE_HOME = $dataHome
  $env:WEBVIEW2_USER_DATA_FOLDER = $profile
  $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = "--remote-debugging-address=127.0.0.1 --remote-debugging-port=$Port"
  $env:OROLE_E2E_ALLOW_ISOLATED_HOME = '1'
  $env:OROLE_E2E_CDP_ENDPOINT = $endpoint
  $env:OROLE_E2E_MANIFEST = $manifest
  # Start the installed daemon, not a build-directory substitute. The CLI only talks to this home.
  $daemonProcess = Start-Process -FilePath $daemon -PassThru
  $deadline = (Get-Date).AddSeconds($WaitSeconds)
  $ready = $false
  while ((Get-Date) -lt $deadline) {
    if ($daemonProcess.HasExited) { throw 'The isolated installed daemon exited before becoming ready.' }
    $status = & $cli daemon status 2>&1 | Out-String
    if ($LASTEXITCODE -eq 0 -and $status -match "pid:\s+$($daemonProcess.Id)\b") { $ready = $true; break }
    Start-Sleep -Milliseconds 250
  }
  if (-not $ready) { throw 'The isolated installed daemon did not become ready.' }
  & $cli settings set acceptance.launchToken $token
  if ($LASTEXITCODE -ne 0) { throw 'Could not record the isolated launch nonce.' }
  $appProcess = Start-Process -FilePath $app -PassThru
  $deadline = (Get-Date).AddSeconds($WaitSeconds)
  $ready = $false
  while ((Get-Date) -lt $deadline) {
    if ($appProcess.HasExited) { throw 'The installed GUI exited; another instance may own the native single-instance lock.' }
    try {
      $targets = Invoke-RestMethod -Uri ($endpoint + 'json/list') -TimeoutSec 2
      if (@($targets | Where-Object { $_.type -eq 'page' -and $_.title -eq 'Orole OS' }).Count -eq 1) { $ready = $true; break }
    } catch { Start-Sleep -Milliseconds 250 }
  }
  if (-not $ready) { throw 'WebView2 did not expose exactly one Orole OS CDP page.' }
  @{ endpoint = $endpoint; home = $dataHome; token = $token; appPid = $appProcess.Id; daemonPid = $daemonProcess.Id } | ConvertTo-Json | Set-Content -LiteralPath $manifest -Encoding utf8
  Push-Location (Join-Path $root 'frontend')
  try {
    & bunx playwright test --config playwright.native.config.ts
    $exitCode = $LASTEXITCODE
  } finally { Pop-Location }
} finally {
  # Only process handles created above are eligible for termination. No taskkill /IM or name matching.
  if ($null -ne $appProcess -and -not $appProcess.HasExited) {
    $appProcess.Kill($true)
    $null = $appProcess.WaitForExit(10000)
  }
  if ($null -ne $daemonProcess -and -not $daemonProcess.HasExited) {
    & $cli daemon stop 2>&1 | Write-Host
    if (-not $daemonProcess.WaitForExit(10000)) { $daemonProcess.Kill($true); $null = $daemonProcess.WaitForExit(10000) }
  }
  foreach ($name in $envNames) { [Environment]::SetEnvironmentVariable($name, $oldEnv[$name], 'Process') }
  Remove-Item -LiteralPath $runRoot -Recurse -Force
}
exit $exitCode
