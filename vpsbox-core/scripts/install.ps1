# Windows installer for vpsbox.
# Mirror of scripts/install.sh for PowerShell.
#
# Usage:
#   irm https://raw.githubusercontent.com/stoicsoft/vpsbox/main/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo = if ($env:VPSBOX_GITHUB_REPO) { $env:VPSBOX_GITHUB_REPO } else { "stoicsoft/vpsbox" }
$Version = if ($env:VERSION) { $env:VERSION } else { "latest" }

$arch = (Get-CimInstance Win32_Processor).Architecture
switch ($arch) {
    9  { $archName = "amd64" }   # x64
    12 { $archName = "arm64" }   # ARM64
    default { throw "Unsupported architecture: $arch" }
}

if ($Version -eq "latest") {
    $url = "https://github.com/$Repo/releases/latest/download/vpsbox_windows_${archName}.tar.gz"
} else {
    $url = "https://github.com/$Repo/releases/download/$Version/vpsbox_windows_${archName}.tar.gz"
}

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("vpsbox-install-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

try {
    $tarPath = Join-Path $tmpDir "vpsbox.tar.gz"
    Write-Host "Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $tarPath -UseBasicParsing

    # tar is built into Windows 10 (1803+) and Windows 11.
    & tar -xzf $tarPath -C $tmpDir
    if ($LASTEXITCODE -ne 0) { throw "tar extraction failed" }

    $installDir = Join-Path $env:LOCALAPPDATA "Programs\vpsbox"
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null

    $exe = Join-Path $tmpDir "vpsbox.exe"
    if (-not (Test-Path $exe)) { throw "vpsbox.exe not found in archive" }
    Copy-Item -Path $exe -Destination (Join-Path $installDir "vpsbox.exe") -Force

    # Add install dir to user PATH if not already present.
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$installDir*") {
        $newPath = if ([string]::IsNullOrEmpty($userPath)) { $installDir } else { "$userPath;$installDir" }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        Write-Host "Added $installDir to user PATH (open a new terminal to pick it up)"
    }

    Write-Host "Installed vpsbox to $installDir\vpsbox.exe"
} finally {
    Remove-Item -Recurse -Force $tmpDir
}
