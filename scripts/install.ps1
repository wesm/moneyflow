#Requires -Version 5.0
# Install a moneyflow Go release on Windows amd64 or arm64.
# MONEYFLOW_VERSION pins vX.Y.Z or vX.Y.Z-rc.N; otherwise use the latest release.
# MONEYFLOW_RELEASE_BASE_URL overrides the releases root, including /latest and /download.

$ErrorActionPreference = 'Stop'

$architecture = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}
switch ($architecture) {
    'AMD64' { $architecture = 'amd64' }
    'ARM64' { $architecture = 'arm64' }
    default { throw "supported Windows architectures are amd64 and arm64" }
}
$releaseRoot = if ($env:MONEYFLOW_RELEASE_BASE_URL) {
    $env:MONEYFLOW_RELEASE_BASE_URL.TrimEnd('/')
} else {
    'https://github.com/wesm/moneyflow/releases'
}

Add-Type -AssemblyName System.Net.Http
Add-Type -AssemblyName System.IO.Compression.FileSystem
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$client = New-Object System.Net.Http.HttpClient
$downloadDirectory = $null
$staged = $null
try {
    $version = $env:MONEYFLOW_VERSION
    if (-not $version) {
        $response = $client.GetAsync("$releaseRoot/latest").GetAwaiter().GetResult()
        try {
            $response.EnsureSuccessStatusCode() | Out-Null
            $finalUrl = $response.RequestMessage.RequestUri.AbsoluteUri
            if ($finalUrl -notmatch '/tag/([^/]+)$') {
                throw 'latest release did not resolve to a tag'
            }
            $version = $Matches[1]
        } finally {
            $response.Dispose()
        }
    }
    if ($version -cnotmatch '\Av[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?\z') {
        throw 'MONEYFLOW_VERSION must be vX.Y.Z or vX.Y.Z-rc.N'
    }
    $archiveName = "moneyflow_$($version.Substring(1))_windows_$architecture.zip"
    $downloadUrl = "$releaseRoot/download/$version"
    $downloadDirectory = Join-Path ([IO.Path]::GetTempPath()) "moneyflow-install-$([guid]::NewGuid())"
    New-Item -ItemType Directory -Path $downloadDirectory | Out-Null
    $archive = Join-Path $downloadDirectory $archiveName
    $checksums = Join-Path $downloadDirectory 'SHA256SUMS'
    Write-Host "Installing moneyflow $version for windows/$architecture..."
    try {
        [IO.File]::WriteAllBytes($archive, $client.GetByteArrayAsync("$downloadUrl/$archiveName").GetAwaiter().GetResult())
    } catch {
        throw "could not download binary archive $archiveName; older Python releases have no binaries, so choose a Go release with MONEYFLOW_VERSION"
    }
    try {
        [IO.File]::WriteAllBytes($checksums, $client.GetByteArrayAsync("$downloadUrl/SHA256SUMS").GetAwaiter().GetResult())
    } catch {
        throw 'could not download checksum manifest SHA256SUMS; installation unchanged'
    }
    $checksumPattern = '^([0-9a-fA-F]{64})\s+\*?' + [regex]::Escape($archiveName) + '$'
    $expected = @(Get-Content -LiteralPath $checksums | ForEach-Object {
        if ($_ -match $checksumPattern) { $Matches[1] }
    })
    if ($expected.Count -ne 1) {
        throw "checksum manifest must contain exactly one entry for $archiveName"
    }
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
    if ($actual -ne $expected[0]) {
        throw "checksum mismatch for $archiveName; installation unchanged"
    }

    $zip = [IO.Compression.ZipFile]::OpenRead($archive)
    try {
        if ($zip.Entries.Count -ne 1 -or $zip.Entries[0].FullName -cne 'moneyflow.exe') {
            throw 'binary archive must contain only moneyflow.exe at its root'
        }
    } finally {
        $zip.Dispose()
    }
    [IO.Compression.ZipFile]::ExtractToDirectory($archive, $downloadDirectory)
    $installDirectory = if ($env:MONEYFLOW_INSTALL_DIR) {
        $env:MONEYFLOW_INSTALL_DIR
    } else {
        Join-Path $env:LOCALAPPDATA 'Programs\moneyflow\bin'
    }
    $installDirectory = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($installDirectory)
    New-Item -ItemType Directory -Path $installDirectory -Force | Out-Null
    $destination = Join-Path $installDirectory 'moneyflow.exe'
    $staged = Join-Path $installDirectory ".moneyflow-install-$([guid]::NewGuid()).exe"
    [IO.File]::Copy((Join-Path $downloadDirectory 'moneyflow.exe'), $staged)
    if ([IO.File]::Exists($destination)) {
        [IO.File]::Replace($staged, $destination, [NullString]::Value)
    } else {
        [IO.File]::Move($staged, $destination)
    }
    $staged = $null
    Write-Host "Installed $destination"
    Write-Host "Add $installDirectory to PATH, then run moneyflow."
} finally {
    $client.Dispose()
    if ($staged -and (Test-Path -LiteralPath $staged)) {
        Remove-Item -LiteralPath $staged -Force
    }
    if ($downloadDirectory -and (Test-Path -LiteralPath $downloadDirectory)) {
        Remove-Item -LiteralPath $downloadDirectory -Recurse -Force
    }
}
