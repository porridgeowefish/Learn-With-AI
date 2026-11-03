param(
  [string]$ShortcutName = 'learn-with-ai-dev',
  [string]$WorkspaceRoot = ''
)
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$appPath = Join-Path $repoRoot 'dist\dev\learn-with-ai-desktop.exe'
$iconPath = Join-Path $repoRoot 'scripts\assets\learn-with-ai.ico'
if (-not (Test-Path -LiteralPath $appPath -PathType Leaf)) { throw 'Run npm run build:desktop before creating the native developer shortcut.' }
$launchArguments = ''
if (-not [string]::IsNullOrWhiteSpace($WorkspaceRoot)) {
  $resolvedWorkspace = (Resolve-Path -LiteralPath $WorkspaceRoot).Path
  $launchArguments = "--workspace `"$resolvedWorkspace`""
}
$desktop = [Environment]::GetFolderPath('DesktopDirectory')
$shortcutPath = Join-Path $desktop "$ShortcutName.lnk"
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = $appPath
$shortcut.Arguments = $launchArguments
$shortcut.WorkingDirectory = $repoRoot
$shortcut.IconLocation = $iconPath
$shortcut.Description = 'learn-with-ai native development verification'
$shortcut.Save()
Write-Output "Created native desktop shortcut: $shortcutPath"
