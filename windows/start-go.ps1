$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $RootDir ".local\bin\pulse-review.exe"
if (-not (Test-Path $Binary)) {
    Write-Host "Application was not built. Run setup.cmd first." -ForegroundColor Red
    exit 1
}
Set-Location $RootDir
& $Binary
exit $LASTEXITCODE
