# Compatibility entry point for existing shortcuts and scripts.
param(
  [int]$Port = 8787,
  [string]$WorkspaceRoot = '',
  [switch]$NoBrowser,
  [switch]$SkipBuild
)

& (Join-Path $PSScriptRoot 'Start-LWA.ps1') @PSBoundParameters
exit $LASTEXITCODE
