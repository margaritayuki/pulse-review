$ErrorActionPreference = "Stop"

$RootDir = $PSScriptRoot
$EnvFile = Join-Path $RootDir ".env.local"
$Binary = Join-Path $RootDir "pulse-review.exe"

function Stop-WithError([string]$Message) {
    Write-Host "`nError: $Message" -ForegroundColor Red
    exit 1
}

if (-not (Test-Path $Binary)) {
    Stop-WithError "pulse-review.exe was not found next to setup.cmd. Extract the complete ZIP archive first."
}

Write-Host "`nPulse Review - Windows setup" -ForegroundColor Cyan
$KeepConfiguration = $false
if (Test-Path $EnvFile) {
    $Keep = Read-Host "`n.env.local already exists. Keep the current configuration? [Y/n]"
    $KeepConfiguration = $Keep -notmatch "^(n|no)$"
}

if (-not $KeepConfiguration) {
    $GitLabUrl = Read-Host "`nGitLab URL [https://gitlab.com]"
    if ([string]::IsNullOrWhiteSpace($GitLabUrl)) { $GitLabUrl = "https://gitlab.com" }
    $GitLabUrl = $GitLabUrl.TrimEnd("/")
    if ($GitLabUrl -notmatch "^https?://") { Stop-WithError "GitLab URL must start with http:// or https://" }

    $SecureToken = Read-Host "GitLab access token (input is hidden)" -AsSecureString
    $TokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureToken)
    try { $GitLabToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($TokenPointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($TokenPointer) }
    if ([string]::IsNullOrWhiteSpace($GitLabToken)) { Stop-WithError "Token cannot be empty." }

    $AppPort = Read-Host "Port [4567]"
    if ([string]::IsNullOrWhiteSpace($AppPort)) { $AppPort = "4567" }
    if ($AppPort -notmatch "^\d+$" -or [int]$AppPort -lt 1 -or [int]$AppPort -gt 65535) { Stop-WithError "Port must be a number from 1 to 65535." }

    $Contents = @("GITLAB_URL=$GitLabUrl", "GITLAB_TOKEN=$GitLabToken", "GITLAB_PROJECTS=", "PORT=$AppPort") -join "`n"
    [IO.File]::WriteAllText($EnvFile, "$Contents`n", [Text.UTF8Encoding]::new($false))
    $GitLabToken = $null
}

Write-Host "`nSetup complete." -ForegroundColor Green
$RunNow = Read-Host "Start Pulse Review now? [Y/n]"
if ($RunNow -notmatch "^(n|no)$") {
    & (Join-Path $RootDir "start.cmd")
} else {
    Write-Host "Run start.cmd later."
}
