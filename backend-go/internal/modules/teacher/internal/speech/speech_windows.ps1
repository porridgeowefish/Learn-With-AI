$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$request = [Console]::In.ReadToEnd() | ConvertFrom-Json
Add-Type -AssemblyName System.Speech
$recognizers = @([System.Speech.Recognition.SpeechRecognitionEngine]::InstalledRecognizers())
$languages = @()
foreach ($language in @('zh-CN', 'en-US')) {
    $prefix = $language.Split('-')[0]
    if (@($recognizers | Where-Object { $_.Culture.Name -like "$prefix-*" }).Count -gt 0) { $languages += $language }
}
if ($request.mode -eq 'probe') { @{ languages = $languages } | ConvertTo-Json -Compress; exit 0 }
$prefix = $request.language.Split('-')[0]
$recognizer = $recognizers | Where-Object { $_.Culture.Name -eq $request.language } | Select-Object -First 1
if ($null -eq $recognizer) { $recognizer = $recognizers | Where-Object { $_.Culture.Name -like "$prefix-*" } | Select-Object -First 1 }
if ($null -eq $recognizer) { @{ error = 'language-unavailable' } | ConvertTo-Json -Compress; exit 0 }
$engine = [System.Speech.Recognition.SpeechRecognitionEngine]::new($recognizer)
try {
    $engine.LoadGrammar([System.Speech.Recognition.DictationGrammar]::new())
    $engine.SetInputToWaveFile($request.path)
    $engine.InitialSilenceTimeout = [TimeSpan]::FromSeconds(60)
    $engine.BabbleTimeout = [TimeSpan]::FromSeconds(60)
    $lines = [Collections.Generic.List[string]]::new()
    $confidenceSum = 0.0
    $wordCount = 0
    while ($true) {
        try { $result = $engine.Recognize([TimeSpan]::FromSeconds(65)) }
        catch {
            # SAPI detaches a completed file stream after returning its last phrase.
            # A following synchronous Recognize then reports no input rather than null.
            if ($lines.Count -gt 0 -and $_.Exception.InnerException -is [InvalidOperationException]) { break }
            throw
        }
        if ($null -eq $result) { break }
        if ($result.Text) {
            $lines.Add($result.Text)
            foreach ($word in $result.Words) { $confidenceSum += $word.Confidence; $wordCount++ }
        }
    }
    $confidence = if ($wordCount -gt 0) { $confidenceSum / $wordCount } else { 0.0 }
    @{ text = ($lines -join "`n"); confidence = $confidence } | ConvertTo-Json -Compress
} finally { $engine.Dispose() }
