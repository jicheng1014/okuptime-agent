# Windows first-install bootstrap; no administrator rights or PATH changes required.
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$target = Join-Path $env:LOCALAPPDATA 'OKUptime\bin\okuptime.exe'
$existing = Get-Command okuptime -CommandType Application -ErrorAction SilentlyContinue
if ($existing) { & $existing.Source version --json; if ($LASTEXITCODE -ne 0) { throw 'Existing CLI failed' }; return }
if (Test-Path -LiteralPath $target) { & $target version --json; if ($LASTEXITCODE -ne 0) { throw 'Existing CLI failed' }; return }
$arch = (Get-CimInstance -ClassName Win32_Processor | Select-Object -First 1).Architecture
$architecture = switch ($arch) { 9 { 'amd64' }; 12 { 'arm64' }; default { throw "Unsupported architecture: $arch" } }
# No redirects: metadata and downloads must originate at the official host.
function Download([string]$url, [string]$path, [long]$limit) {
    $request = [Net.HttpWebRequest]::Create($url)
    $request.AllowAutoRedirect = $false
    $request.Timeout = 30000
    $request.ReadWriteTimeout = 30000
    $response = $request.GetResponse()
    try {
        if ([int]$response.StatusCode -ne 200) { throw 'Expected HTTP 200 without redirects' }
        if ($response.ContentLength -gt $limit) { throw 'Download exceeds size limit' }
        $inputStream = $response.GetResponseStream()
        $outputStream = [IO.File]::Open($path, [IO.FileMode]::CreateNew)
        try {
            $buffer = New-Object byte[] 65536
            [long]$total = 0
            while (($count = $inputStream.Read($buffer, 0, $buffer.Length)) -gt 0) {
                $total += $count
                if ($total -gt $limit) { throw 'Download exceeds size limit' }
                $outputStream.Write($buffer, 0, $count)
            }
        } finally { $outputStream.Dispose(); $inputStream.Dispose() }
    } finally { $response.Dispose() }
}
$work = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
[IO.Directory]::CreateDirectory($work) | Out-Null
try {
    $metadataPath = Join-Path $work 'metadata.json'
    Download "https://www.okuptime.com/api/v1/cli/releases/latest?platform=windows&architecture=$architecture" $metadataPath 65536
    $release = (Get-Content -LiteralPath $metadataPath -Raw | ConvertFrom-Json).data
    if ($release.platform -ne 'windows' -or $release.architecture -ne $architecture) { throw 'Platform mismatch' }
    if ($release.version -notmatch '^\d+\.\d+\.\d+$' -or $release.sha256 -cnotmatch '^[0-9a-f]{64}$') { throw 'Invalid release metadata' }
    [long]$size = $release.size
    if ($size -le 0 -or $size -gt 67108864) { throw 'Invalid release size' }
    if ($release.url.Contains('/../') -or $release.url.Contains('/./')) { throw 'Invalid download path' }
    if ($release.url -cnotmatch '^https://www\.okuptime\.com/cli/releases/[A-Za-z0-9._/-]+\.exe$') { throw 'Invalid official download URL' }
    $binary = Join-Path $work 'okuptime.exe'
    Download $release.url $binary $size
    if ((Get-Item -LiteralPath $binary).Length -ne $size) { throw 'Size mismatch' }
    if ((Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant() -ne $release.sha256) { throw 'Checksum mismatch' }
    $versionOutput = & $binary version --json
    if ($LASTEXITCODE -ne 0) { throw 'Downloaded CLI failed version check' }
    if (($versionOutput | ConvertFrom-Json).data.version -ne $release.version) { throw 'Version mismatch' }
    Write-Output $versionOutput
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target)) | Out-Null
    # Stage on the destination volume, then atomically move without overwriting.
    $staged = Join-Path ([IO.Path]::GetDirectoryName($target)) ([Guid]::NewGuid().ToString() + ".install.exe")
    try {
        [IO.File]::Copy($binary, $staged, $false)
        [IO.File]::Move($staged, $target)
    } finally { if (Test-Path -LiteralPath $staged) { Remove-Item -LiteralPath $staged -Force } }
    Write-Output "Installed $($release.version) at $target"
} finally { Remove-Item -LiteralPath $work -Recurse -Force }
