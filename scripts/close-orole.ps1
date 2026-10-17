<#
.SYNOPSIS
  Closes Orole OS and its daemon when they run from the folder -Dir, so the installer can replace their files.
.DESCRIPTION
  Asks each window to close first, waits up to 8 s, then ends what is left. Only processes whose executable
  is inside -Dir are touched: a development build or another install is left alone. A bot's computer (QEMU)
  runs in the daemon's kill-on-close job, so it stops with the daemon; its disk stays.
#>
param([Parameter(Mandatory)][string]$Dir)
$prefix = [IO.Path]::GetFullPath($Dir).TrimEnd('\') + '\'
# CIM instead of Process.Path: the installer stub is 32-bit, and its PowerShell cannot read the path of a 64-bit process.
function Get-Mine {
  Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase) } |
    ForEach-Object { Get-Process -Id $_.ProcessId -ErrorAction SilentlyContinue }
}
$mine = @(Get-Mine)
if (-not $mine) { return }
$mine | ForEach-Object { $null = $_.CloseMainWindow() }
$deadline = (Get-Date).AddSeconds(8)
while ((Get-Date) -lt $deadline -and @(Get-Mine | Where-Object { $_.MainWindowHandle -ne 0 })) { Start-Sleep -Milliseconds 250 }
# The daemon has no window: end it (and anything else left) once the app is gone.
Get-Mine | ForEach-Object { try { $_.Kill() } catch {} }
$deadline = (Get-Date).AddSeconds(8)
while ((Get-Date) -lt $deadline -and @(Get-Mine)) { Start-Sleep -Milliseconds 250 }
