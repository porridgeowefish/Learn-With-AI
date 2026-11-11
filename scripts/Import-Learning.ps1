param(
    [Parameter(Mandatory=$true, Position=0)][string]$Archive,
    [Parameter(Position=1)][string]$ServerUrl
)
$ErrorActionPreference = 'Stop'
try {
    $zip = Get-Item -LiteralPath $Archive
    if ($zip.PSIsContainer) { throw 'Expected a ZIP file.' }
    if (!$ServerUrl) {
        $endpoint = Join-Path $env:APPDATA 'Learn-With-AI/transfer-endpoint.json'
        $ServerUrl = (Get-Content -Raw -Encoding UTF8 -LiteralPath $endpoint | ConvertFrom-Json).url
    }
    if ($ServerUrl -cnotmatch '^http://127\.0\.0\.1:[0-9]{1,5}/?$') { throw 'Expected a local Learn-With-AI server URL.' }
    Add-Type -AssemblyName System.Net.Http
    $handler = [System.Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $handler.UseProxy = $false
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [System.Threading.Timeout]::InfiniteTimeSpan
    $client.DefaultRequestHeaders.Add('X-LWA-Workspace','1')
    $stream = [IO.File]::OpenRead($zip.FullName)
    $body = [System.Net.Http.StreamContent]::new($stream)
    $body.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::new('application/zip')
    $url = $ServerUrl.TrimEnd('/') + '/api/workspace/import'
    $response = $client.PostAsync($url,$body).GetAwaiter().GetResult()
    $reply = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    if (!$response.IsSuccessStatusCode) { throw "HTTP $([int]$response.StatusCode) $reply" }
    Write-Output $reply
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
} finally {
    if ($response) { $response.Dispose() }
    if ($body) { $body.Dispose() }
    if ($stream) { $stream.Dispose() }
    if ($client) { $client.Dispose() }
}
