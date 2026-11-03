param(
  [int]$Port = 8787,
  [string]$WorkspaceRoot = '',
  [switch]$NoBrowser,
  [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'

function Show-LauncherError {
  param([string]$Message)
  try {
    Add-Type -AssemblyName PresentationFramework -ErrorAction Stop
    [System.Windows.MessageBox]::Show($Message, 'Learn-With-AI launcher', 'OK', 'Error') | Out-Null
  } catch {
    Write-Error $Message
  }
}

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
    $proc = Get-Process -Id $ProcessId -ErrorAction Stop
    return $proc.Path
  } catch {
    return $null
  }
}

function Test-Health {
  param([int]$Port)
  try {
    $res = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/health" -TimeoutSec 2
    return $res.StatusCode -eq 200
  } catch {
    return $false
  }
}

function Get-LatestWriteTimeUtc {
  param([string[]]$Paths)

  $latest = [DateTime]::MinValue
  foreach ($path in $Paths) {
    if (-not (Test-Path -LiteralPath $path)) { continue }

    $item = Get-Item -LiteralPath $path
    if (-not $item.PSIsContainer) {
      if ($item.LastWriteTimeUtc -gt $latest) { $latest = $item.LastWriteTimeUtc }
      continue
    }

    Get-ChildItem -LiteralPath $path -Recurse -File -ErrorAction SilentlyContinue |
      Where-Object {
        $_.FullName -notmatch '\\node_modules\\' -and
        $_.FullName -notmatch '\\dist\\' -and
        $_.FullName -notmatch '\\tmp\\'
      } |
      ForEach-Object {
        if ($_.LastWriteTimeUtc -gt $latest) { $latest = $_.LastWriteTimeUtc }
      }
  }
  return $latest
}

function Test-BuildNeeded {
  param(
    [string]$RepoRoot,
    [string]$ExePath
  )

  $frontendIndex = Join-Path $RepoRoot 'frontend\dist\index.html'
  if (-not (Test-Path -LiteralPath $ExePath)) { return $true }
  if (-not (Test-Path -LiteralPath $frontendIndex)) { return $true }

  $sourcePaths = @(
    (Join-Path $RepoRoot 'backend-go'),
    (Join-Path $RepoRoot 'frontend\src'),
    (Join-Path $RepoRoot 'frontend\public'),
    (Join-Path $RepoRoot 'frontend\index.html'),
    (Join-Path $RepoRoot 'frontend\package.json'),
    (Join-Path $RepoRoot 'frontend\package-lock.json'),
    (Join-Path $RepoRoot 'frontend\vite.config.ts'),
    (Join-Path $RepoRoot 'frontend\tsconfig.json'),
    (Join-Path $RepoRoot 'frontend\tsconfig.node.json'),
    (Join-Path $RepoRoot 'go.mod'),
    (Join-Path $RepoRoot 'package.json')
  )

  $latestSource = Get-LatestWriteTimeUtc -Paths $sourcePaths
  $exeTime = (Get-Item -LiteralPath $ExePath).LastWriteTimeUtc
  $frontendTime = (Get-Item -LiteralPath $frontendIndex).LastWriteTimeUtc
  $oldestBuild = if ($exeTime -lt $frontendTime) { $exeTime } else { $frontendTime }

  return $latestSource -gt $oldestBuild
}

function Invoke-ProjectBuild {
  param(
    [string]$RepoRoot,
    [string]$OutLog,
    [string]$ErrLog
  )

  $npm = Get-Command npm.cmd -ErrorAction SilentlyContinue
  if (-not $npm) { $npm = Get-Command npm -ErrorAction SilentlyContinue }
  if (-not $npm) { throw 'npm was not found on PATH; cannot refresh frontend/backend build.' }

  Push-Location $RepoRoot
  try {
    & $npm.Source run build > $OutLog 2> $ErrLog
    if ($LASTEXITCODE -ne 0) {
      throw "npm run build failed with exit code $LASTEXITCODE. Check $ErrLog and $OutLog."
    }
  } finally {
    Pop-Location
  }
}

function Stop-ExistingRootServer {
  param(
    [int]$ProcessId,
    [string]$ExePath,
    [int]$Port
  )

  $path = Get-ProcessPath -ProcessId $ProcessId
  if (-not $path -or ([string]::Compare($path, $ExePath, $true) -ne 0)) {
    throw "Port $Port is already used by PID $ProcessId ($path). Not an LLL server from this workspace."
  }

  try {
    Invoke-WebRequest -UseBasicParsing -Method Post -Uri "http://127.0.0.1:$Port/api/system/shutdown" -TimeoutSec 2 | Out-Null
  } catch {
    # Older or wedged builds may not answer shutdown; fall through to force stop.
  }

  $deadline = (Get-Date).AddSeconds(5)
  do {
    Start-Sleep -Milliseconds 200
    $stillRunning = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
  } while ($stillRunning -and (Get-Date) -lt $deadline)

  if ($stillRunning) {
    Stop-Process -Id $ProcessId -Force -ErrorAction Stop
    Start-Sleep -Milliseconds 300
  }
}

try {
  $repoRoot = Get-RepoRoot
  $launchArguments = @('--no-browser', '--port', "$Port")
  if (-not [string]::IsNullOrWhiteSpace($WorkspaceRoot)) {
    $resolvedWorkspace = (Resolve-Path -LiteralPath $WorkspaceRoot).Path
    $launchArguments += @('--workspace', "`"$resolvedWorkspace`"")
  }
  $exePath = Join-Path $repoRoot 'dist\dev\learn-with-ai.exe'
  $tmpDir = Join-Path $repoRoot 'trash\desktop-launcher'
  $pidFile = Join-Path $tmpDir 'lll.pid'
  $outLog = Join-Path $tmpDir 'lll.out.log'
  $errLog = Join-Path $tmpDir 'lll.err.log'
  $buildOutLog = Join-Path $tmpDir 'build.out.log'
  $buildErrLog = Join-Path $tmpDir 'build.err.log'

  New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
  $buildNeeded = (-not $SkipBuild) -and (Test-BuildNeeded -RepoRoot $repoRoot -ExePath $exePath)

  $ownerPid = Get-PortOwner -Port $Port
  if ($ownerPid) {
    $ownerPath = Get-ProcessPath -ProcessId $ownerPid
    $isCurrentBuild = $ownerPath -and ([string]::Compare($ownerPath, $exePath, $true) -eq 0)
    if ((-not $buildNeeded) -and $isCurrentBuild -and (Test-Health -Port $Port)) {
      if (-not $NoBrowser) { Start-Process "http://127.0.0.1:$Port/" | Out-Null }
      exit 0
    }
    Stop-ExistingRootServer -ProcessId $ownerPid -ExePath $exePath -Port $Port
  }

  if ($buildNeeded) {
    Invoke-ProjectBuild -RepoRoot $repoRoot -OutLog $buildOutLog -ErrLog $buildErrLog
  }

  if (-not (Test-Path -LiteralPath $exePath)) {
    throw "Missing $exePath after build. Check $buildErrLog and $buildOutLog."
  }

  $proc = Start-Process -FilePath $exePath `
    -ArgumentList $launchArguments `
    -WorkingDirectory $repoRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $outLog `
    -RedirectStandardError $errLog `
    -PassThru

  Set-Content -LiteralPath $pidFile -Encoding ASCII -Value $proc.Id

  $ready = $false
  for ($i = 0; $i -lt 60; $i++) {
    if (Test-Health -Port $Port) {
      $ready = $true
      break
    }
    Start-Sleep -Milliseconds 500
  }

  if (-not $ready) {
    throw "learn-with-ai did not become ready on http://127.0.0.1:$Port. Check $errLog and $outLog."
  }

  if (-not $NoBrowser) {
    Start-Process "http://127.0.0.1:$Port/" | Out-Null
  }
  exit 0
} catch {
  Show-LauncherError $_.Exception.Message
  exit 1
}
