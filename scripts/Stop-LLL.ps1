# Compatibility entry point for existing shortcuts and scripts.
param([int]$Port = 8787)

& (Join-Path $PSScriptRoot 'Stop-LWA.ps1') @PSBoundParameters
exit $LASTEXITCODE
