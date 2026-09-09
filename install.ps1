[CmdletBinding()]
param(
    [string]$Version = $(if ($env:GIT_BYLINE_VERSION) { $env:GIT_BYLINE_VERSION } else { "latest" }),
    [string]$BinDir = $(if ($env:GIT_BYLINE_BIN_DIR) { $env:GIT_BYLINE_BIN_DIR } else { Join-Path $HOME "bin" }),
    [switch]$NoGitHook
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repository = "https://github.com/comarch/git-byline"
if ($Version -eq "latest") {
    $release = Invoke-RestMethod `
        -Uri "https://api.github.com/repos/comarch/git-byline/releases/latest" `
        -Headers @{ Accept = "application/vnd.github+json" }
    $Version = [string]$release.tag_name
}
if ($Version -notmatch "^v[0-9]+\.[0-9]+\.[0-9]+$") {
    throw "Unsupported release version: $Version"
}

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
switch ($architecture) {
    "X64" { $arch = "amd64" }
    "Arm64" { $arch = "arm64" }
    default { throw "Unsupported architecture: $architecture" }
}

$releaseVersion = $Version.Substring(1)
$archive = "git-byline_${releaseVersion}_windows_${arch}.zip"
$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("git-byline-" + [guid]::NewGuid())
$archivePath = Join-Path $temporary $archive
$checksumsPath = Join-Path $temporary "checksums.txt"
$binaryPath = Join-Path $temporary "git-byline.exe"
$staged = $null

New-Item -ItemType Directory -Path $temporary | Out-Null
try {
    Invoke-WebRequest -Uri "$repository/releases/download/$Version/$archive" -OutFile $archivePath
    Invoke-WebRequest -Uri "$repository/releases/download/$Version/checksums.txt" -OutFile $checksumsPath

    $escapedArchive = [regex]::Escape($archive)
    $checksumLines = @(Get-Content -LiteralPath $checksumsPath | Where-Object {
        $_ -match "^([0-9a-fA-F]{64})\s+\*?$escapedArchive$"
    })
    if ($checksumLines.Count -ne 1) {
        throw "Missing or duplicate checksum for $archive"
    }
    $null = $checksumLines[0] -match "^([0-9a-fA-F]{64})"
    $expected = $Matches[1].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for $archive"
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $archiveFiles = @(
            $zip.Entries |
                Where-Object { -not $_.FullName.EndsWith("/") } |
                ForEach-Object { $_.FullName.TrimStart([char[]]"./") } |
                Sort-Object
        )
        $expectedFiles = @("LICENSE", "README.md", "SECURITY.md", "git-byline.exe") | Sort-Object
        if (Compare-Object -ReferenceObject $expectedFiles -DifferenceObject $archiveFiles) {
            throw "Unexpected files in $archive"
        }
        $entries = @($zip.Entries | Where-Object { $_.FullName -eq "git-byline.exe" })
        if ($entries.Count -ne 1 -or $entries[0].Length -eq 0) {
            throw "Archive must contain exactly one git-byline.exe"
        }
        $source = $entries[0].Open()
        try {
            $destination = [System.IO.File]::Create($binaryPath)
            try {
                $source.CopyTo($destination)
            }
            finally {
                $destination.Dispose()
            }
        }
        finally {
            $source.Dispose()
        }
    }
    finally {
        $zip.Dispose()
    }

    $reportedVersion = & $binaryPath version
    if ($reportedVersion -ne "git-byline $Version") {
        throw "Binary version does not match release"
    }

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $target = Join-Path $BinDir "git-byline.exe"
    if (Test-Path -LiteralPath $target -PathType Container) {
        throw "Destination is a directory: $target"
    }
    $staged = Join-Path $BinDir (".git-byline-" + [guid]::NewGuid() + ".exe")
    Copy-Item -LiteralPath $binaryPath -Destination $staged
    Move-Item -LiteralPath $staged -Destination $target -Force

    if (-not $NoGitHook -and
        (Get-Command git -ErrorAction SilentlyContinue) -and
        (git rev-parse --is-inside-work-tree 2>$null) -eq "true") {
        & $target install-hooks --agent none --git --project
    }

    Write-Output "Installed git-byline $Version to $target"
    if (($env:PATH -split [System.IO.Path]::PathSeparator) -notcontains $BinDir) {
        Write-Output "Add $BinDir to PATH before starting an AI agent."
    }
}
finally {
    if ($staged) {
        Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $temporary -Recurse -Force -ErrorAction SilentlyContinue
}
