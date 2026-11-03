# Encode a WAV to WMA with Windows Media Foundation's WMA8 encoder, 48 kHz stereo, 192 kbit/s.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File wma8_encode.ps1 <in.wav> <out.wma>
#
# That profile writes 8192-byte superframes the engine's decoder plays, the same as Konami's
# own streams. Other WMA encoders (ffmpeg's wmav2 among them) do not: their streams play
# garbled in game. Windows PowerShell 5.1 (powershell.exe), which can call WinRT.
param([Parameter(Mandatory)][string]$In, [Parameter(Mandatory)][string]$Out)
$ErrorActionPreference = 'Stop'

Add-Type -AssemblyName System.Runtime.WindowsRuntime
[void][Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
[void][Windows.Storage.StorageFolder, Windows.Storage, ContentType = WindowsRuntime]
[void][Windows.Media.Transcoding.MediaTranscoder, Windows.Media.Transcoding, ContentType = WindowsRuntime]
[void][Windows.Media.MediaProperties.MediaEncodingProfile, Windows.Media.MediaProperties, ContentType = WindowsRuntime]

$ext = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 }
function Await($op, [Type]$result) {
    $m = $ext | Where-Object { $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
    $t = $m.MakeGenericMethod($result).Invoke($null, @($op))
    $t.Wait(-1) | Out-Null
    $t.Result
}
function AwaitProgress($op) {
    $m = $ext | Where-Object { $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncActionWithProgress`1' } | Select-Object -First 1
    $t = $m.MakeGenericMethod([double]).Invoke($null, @($op))
    $t.Wait(-1) | Out-Null
}

$inPath = (Resolve-Path $In).Path
$outFull = [System.IO.Path]::GetFullPath($Out)
$src = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($inPath)) ([Windows.Storage.StorageFile])
$folder = Await ([Windows.Storage.StorageFolder]::GetFolderFromPathAsync([System.IO.Path]::GetDirectoryName($outFull))) ([Windows.Storage.StorageFolder])
$dst = Await ($folder.CreateFileAsync([System.IO.Path]::GetFileName($outFull), [Windows.Storage.CreationCollisionOption]::ReplaceExisting)) ([Windows.Storage.StorageFile])

$profile = [Windows.Media.MediaProperties.MediaEncodingProfile]::CreateWma([Windows.Media.MediaProperties.AudioEncodingQuality]::High)
$profile.Audio.Subtype = 'WMA8'
$profile.Audio.SampleRate = 48000
$profile.Audio.ChannelCount = 2
$profile.Audio.BitsPerSample = 16
$profile.Audio.Bitrate = 192000

$tr = New-Object Windows.Media.Transcoding.MediaTranscoder
$prep = Await ($tr.PrepareFileTranscodeAsync($src, $dst, $profile)) ([Windows.Media.Transcoding.PrepareTranscodeResult])
if (-not $prep.CanTranscode) { throw "cannot transcode: $($prep.FailureReason)" }
AwaitProgress ($prep.TranscodeAsync())
Write-Output "wrote $outFull"
