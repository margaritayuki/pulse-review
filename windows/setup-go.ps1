$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $PSScriptRoot
$EnvFile = Join-Path $RootDir ".env.local"
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

Write-Host "`nPulse Review v0.2.0 - Go setup" -ForegroundColor Cyan
Write-Host $GoVersion
$KeepConfiguration = $false
if (Test-Path $EnvFile) {
    $Keep = Read-Host "`n.env.local already exists. Keep it and only rebuild the application? [Y/n]"
    $KeepConfiguration = $Keep -notmatch '^(n|no)$'
}

if (-not $KeepConfiguration) {
    $GitLabUrl = Read-Host "`nGitLab URL [https://gitlab.com]"
    if ([string]::IsNullOrWhiteSpace($GitLabUrl)) { $GitLabUrl = "https://gitlab.com" }
    $GitLabUrl = $GitLabUrl.TrimEnd("/")
    if ($GitLabUrl -notmatch '^https?://') { Stop-WithError "GitLab URL must start with http:// or https://" }
    $SecureToken = Read-Host "GitLab access token (input is hidden)" -AsSecureString
    $TokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureToken)
    try { $GitLabToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($TokenPointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($TokenPointer) }
    if ([string]::IsNullOrWhiteSpace($GitLabToken)) { Stop-WithError "Token cannot be empty." }
    $AppPort = Read-Host "Port [4567]"
    if ([string]::IsNullOrWhiteSpace($AppPort)) { $AppPort = "4567" }
    if ($AppPort -notmatch '^\d+$' -or [int]$AppPort -lt 1 -or [int]$AppPort -gt 65535) { Stop-WithError "Port must be a number from 1 to 65535." }
    $Contents = @("GITLAB_URL=$GitLabUrl", "GITLAB_TOKEN=$GitLabToken", "GITLAB_PROJECTS=", "PORT=$AppPort") -join "`n"
    [IO.File]::WriteAllText($EnvFile, "$Contents`n", [Text.UTF8Encoding]::new($false))
    $GitLabToken = $null
}

Write-Host "`nBuilding Go application..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Binary) | Out-Null
Push-Location $RootDir
try {
    & go build -trimpath -o $Binary .
    if ($LASTEXITCODE -ne 0) { Stop-WithError "Go build failed." }
} finally { Pop-Location }
Write-Host "`nSetup complete. Run start.cmd." -ForegroundColor Green
