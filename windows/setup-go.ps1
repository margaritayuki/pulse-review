$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $RootDir ".local\bin\pulse-review.exe"

function Stop-WithError([string]$Message) {
    Write-Host "`nError: $Message" -ForegroundColor Red
    exit 1
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Stop-WithError "Go was not found. Install Go 1.27+ from https://go.dev/dl/ and reopen PowerShell."
}
$GoVersion = (& go env GOVERSION).Trim()
if ($GoVersion -notmatch '^go1\.(\d+)' -or [int]$Matches[1] -lt 27) {
    Stop-WithError "Go 1.27 or newer is required. Installed version: $GoVersion."
}

Write-Host "`nBuilding Pulse Review..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Binary) | Out-Null
Push-Location $RootDir
try {
    & go build -trimpath -o $Binary .
    if ($LASTEXITCODE -ne 0) { Stop-WithError "Go build failed." }
} finally { Pop-Location }
Write-Host "`nSetup complete. Run start.cmd and configure GitLab in the browser." -ForegroundColor Green
