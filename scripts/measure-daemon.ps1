<#
.SYNOPSIS
  Measures orole-daemon cold start, idle memory and idle CPU over a sampling window.
.DESCRIPTION
  Uses an isolated OROLE_HOME (a fresh temp dir), never %LOCALAPPDATA%\Orole.
  Cold start = process spawn until the named pipe accepts a connection (median of -Runs
  runs, fresh data dir each time, so it includes first-run database creation + migration).
  Idle memory/CPU are sampled from the last run's process for -IdleSeconds.
  Requires build\orole-daemon.exe and build\orole.exe (scripts\build.ps1 -DaemonOnly builds both).
#>
param(
  [int]$Runs = 5,
  [int]$IdleSeconds = 20,
  [string]$Daemon = "build\orole-daemon.exe",
  [string]$Cli = "build\orole.exe"
)
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)
foreach ($f in $Daemon, $Cli) { if (-not (Test-Path $f)) { throw "$f missing: run scripts\build.ps1 -DaemonOnly" } }

function New-Home { Join-Path ([IO.Path]::GetTempPath()) ("orole-perf-" + [guid]::NewGuid().ToString("N").Substring(0, 8)) }

function Get-PipeName([string]$home_) {
  $env:OROLE_HOME = $home_
  $j = & $Cli daemon diagnostics -json | ConvertFrom-Json
  return $j.endpoint -replace '^\\\\\.\\pipe\\', ''
}

$cold = @()
$lastProc = $null; $lastHome = $null
for ($i = 0; $i -lt $Runs; $i++) {
  if ($lastProc) { & $Cli daemon stop | Out-Null; Start-Sleep -Milliseconds 300; Remove-Item -Recurse -Force $lastHome -ErrorAction SilentlyContinue }
  $h = New-Home; $null = New-Item -ItemType Directory $h
  $pipe = Get-PipeName $h
  $env:OROLE_HOME = $h
  $sw = [Diagnostics.Stopwatch]::StartNew()
  $p = Start-Process -FilePath $Daemon -PassThru -WindowStyle Hidden
  $ok = $false
  while ($sw.Elapsed.TotalSeconds -lt 15) {
    $c = New-Object IO.Pipes.NamedPipeClientStream(".", $pipe, [IO.Pipes.PipeDirection]::InOut)
    try { $c.Connect(5); $ok = $true; $c.Dispose(); break } catch { $c.Dispose(); Start-Sleep -Milliseconds 2 }
  }
  $sw.Stop()
  if (-not $ok) { throw "daemon did not open its pipe within 15s" }
  $cold += $sw.Elapsed.TotalMilliseconds
  $lastProc = $p; $lastHome = $h
}

# Idle sampling on the last instance: let start-up settle for 2s, then measure.
Start-Sleep -Seconds 2
$p = Get-Process -Id $lastProc.Id
$cpu0 = $p.TotalProcessorTime.TotalSeconds
$t0 = Get-Date
Start-Sleep -Seconds $IdleSeconds
$p.Refresh()
$elapsed = ((Get-Date) - $t0).TotalSeconds
$cpuPct = (($p.TotalProcessorTime.TotalSeconds - $cpu0) / $elapsed) * 100 / [Environment]::ProcessorCount
$wsMB = [math]::Round($p.WorkingSet64 / 1MB, 1); $privMB = [math]::Round($p.PrivateMemorySize64 / 1MB, 1)

& $Cli daemon stop | Out-Null
Start-Sleep -Milliseconds 300
Remove-Item -Recurse -Force $lastHome -ErrorAction SilentlyContinue
Remove-Item Env:OROLE_HOME -ErrorAction SilentlyContinue

$sorted = $cold | Sort-Object
$result = [ordered]@{
  machine            = "$((Get-CimInstance Win32_Processor).Name.Trim()), $([Environment]::ProcessorCount) logical CPUs, $([math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1GB)) GB RAM, $((Get-CimInstance Win32_OperatingSystem).Caption) $((Get-CimInstance Win32_OperatingSystem).Version)"
  runs               = $Runs
  coldStartMsMedian  = [math]::Round($sorted[[int][math]::Floor(($sorted.Count - 1) / 2)], 1)
  coldStartMsMin     = [math]::Round($sorted[0], 1)
  coldStartMsMax     = [math]::Round($sorted[-1], 1)
  idleSeconds        = $IdleSeconds
  idleWorkingSetMB   = $wsMB
  idlePrivateMB      = $privMB
  idleCpuPercent     = [math]::Round($cpuPct, 3)
}
$result | ConvertTo-Json
