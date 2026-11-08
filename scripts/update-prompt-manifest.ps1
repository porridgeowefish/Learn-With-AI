param([string]$RepositoryRoot = (Split-Path -Parent $PSScriptRoot))
$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath($RepositoryRoot)
$manifestPath = Join-Path $root 'learning-prompts/manifest.json'
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
$resources = @(Get-ChildItem -LiteralPath (Join-Path $root 'learning-prompts') -File -Recurse | Where-Object { $_.FullName -ne $manifestPath } | Sort-Object FullName | ForEach-Object {
    # Match .gitattributes so working-tree and clean-checkout hashes agree.
    $text = [IO.File]::ReadAllText($_.FullName).Replace("`r`n", "`n")
    [IO.File]::WriteAllText($_.FullName, $text, [Text.UTF8Encoding]::new($false))
    $variables = @([regex]::Matches([IO.File]::ReadAllText($_.FullName), '\{\{([a-zA-Z0-9_]+)\}\}') | ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
    @{ variables = $variables; path = $_.FullName.Substring($root.TrimEnd('\','/').Length + 1).Replace('\','/'); sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
})
$manifest.resources = $resources
[IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 20) + "`n", [Text.UTF8Encoding]::new($false))
Write-Output "Updated $($resources.Count) prompt resource hashes."
