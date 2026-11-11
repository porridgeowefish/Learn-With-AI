param(
  [int]$Port = 8787
)

$ErrorActionPreference = 'Stop'

function Get-RepoRoot {
  return (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
}

function Get-PortOwner {
  param([int]$Port)
  try {
    $conn = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction Stop |
      Select-Object -First 1
    if ($conn) { return [int]$conn.OwningProcess }
  } catch {
    return $null
  }
  return $null
}

function Get-ProcessPath {
  param([int]$ProcessId)
  try {
    return (Get-Process -Id $ProcessId -ErrorAction Stop).Path
  } catch {
    return $null
  }
}

$repoRoot = Get-RepoRoot
$exePath = Join-Path $repoRoot 'dist\dev\learn-with-ai.exe'
$ownerPid = Get-PortOwner -Port $Port

if (-not $ownerPid) {
  Write-Host "Learn-With-AI is not listening on port $Port."
  exit 0
}

$ownerPath = Get-ProcessPath -ProcessId $ownerPid
if (-not $ownerPath -or ([string]::Compare($ownerPath, $exePath, $true) -ne 0)) {
  Write-Host "Port $Port is used by PID $ownerPid ($ownerPath), outside this Learn-With-AI workspace."
  exit 2
}

try {
  Invoke-WebRequest -UseBasicParsing -Method Post -Uri "http://127.0.0.1:$Port/api/system/shutdown" -TimeoutSec 2 | Out-Null
} catch {
  Write-Host "Shutdown API did not answer; forcing PID $ownerPid."
}

$deadline = (Get-Date).AddSeconds(5)
do {
  Start-Sleep -Milliseconds 200
  $stillRunning = Get-Process -Id $ownerPid -ErrorAction SilentlyContinue
} while ($stillRunning -and (Get-Date) -lt $deadline)

if ($stillRunning) {
  Stop-Process -Id $ownerPid -Force -ErrorAction Stop
  Write-Host "Force-stopped Learn-With-AI PID $ownerPid."
} else {
  Write-Host "Stopped Learn-With-AI PID $ownerPid."
}
exit 0
