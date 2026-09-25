# Build the vpsbox desktop app for Windows.
# Mirror of scripts/build-desktop.sh (macOS) but for PowerShell + Windows.

$ErrorActionPreference = "Stop"

$RootDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$WailsBin = if ($env:WAILS_BIN) { $env:WAILS_BIN } else { Join-Path (& go env GOPATH) "bin\wails.exe" }
$DistDir = Join-Path $RootDir "dist"
$OutputExe = Join-Path $DistDir "vpsbox.exe"

$TmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("vpsbox-build-" + [Guid]::NewGuid())
$TmpRepo = Join-Path $TmpRoot "repo"
New-Item -ItemType Directory -Path $TmpRoot -Force | Out-Null

try {
    New-Item -ItemType Directory -Path $DistDir -Force | Out-Null
    if (Test-Path $OutputExe) { Remove-Item $OutputExe -Force }

    # robocopy returns >=8 on actual error; <8 is success/skip codes.
    & robocopy $RootDir $TmpRepo /E /XD .git dist desktop\build\bin desktop\frontend\node_modules desktop\frontend\dist | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "robocopy failed with exit $LASTEXITCODE" }
    $global:LASTEXITCODE = 0

    Push-Location (Join-Path $TmpRepo "desktop")
    try {
        & go mod tidy
        if ($LASTEXITCODE -ne 0) { throw "go mod tidy failed" }

        Push-Location frontend
        try {
            & npm install
            if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
        } finally {
            Pop-Location
        }

        & $WailsBin build -v 1
        if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
    } finally {
        Pop-Location
    }

    $built = Join-Path $TmpRepo "desktop\build\bin\vpsbox.exe"
    Copy-Item -Path $built -Destination $OutputExe -Force
    Write-Host "Built desktop binary at $OutputExe"
} finally {
    Remove-Item -Recurse -Force $TmpRoot
}
