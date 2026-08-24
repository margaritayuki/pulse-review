$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $PSScriptRoot
$EnvFile = Join-Path $RootDir ".env.local"

function Stop-WithError([string]$Message) {
    Write-Host "`nError: $Message" -ForegroundColor Red
    exit 1
}

if (-not (Get-Command ruby -ErrorAction SilentlyContinue)) {
    Stop-WithError "Ruby was not found. Install Ruby 3.1+ from https://rubyinstaller.org and reopen PowerShell."
}

$RubyVersionText = (& ruby -e "print RUBY_VERSION").Trim()
$RubyVersion = [version]$RubyVersionText
if ($RubyVersion -lt [version]"2.6") {
    Stop-WithError "Ruby 2.6 or newer is required. Installed version: $RubyVersionText."
}

if (-not (Get-Command bundle -ErrorAction SilentlyContinue)) {
    Stop-WithError "Bundler was not found. Run: gem install bundler"
}

Write-Host "`nPulse Review - Windows setup" -ForegroundColor Cyan
Write-Host "Ruby $RubyVersionText | $(& bundle --version)"
if ($RubyVersion.Major -lt 3) {
    Write-Host "Ruby 3.1+ is recommended; this version is supported in compatibility mode." -ForegroundColor Yellow
}

if (Test-Path $EnvFile) {
    $Overwrite = Read-Host "`n.env.local already exists. Overwrite it? [y/N]"
    if ($Overwrite -notmatch "^(y|yes)$") {
        Write-Host "`nSetup cancelled. The existing configuration was preserved."
        exit 0
    }
}

$GitLabUrl = Read-Host "`nGitLab URL [https://gitlab.com]"
if ([string]::IsNullOrWhiteSpace($GitLabUrl)) {
    $GitLabUrl = "https://gitlab.com"
}
$GitLabUrl = $GitLabUrl.TrimEnd("/")
if ($GitLabUrl -notmatch "^https?://") {
    Stop-WithError "GitLab URL must start with http:// or https://"
}

$SecureToken = Read-Host "GitLab access token (input is hidden)" -AsSecureString
$TokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureToken)
try {
    $GitLabToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($TokenPointer)
} finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($TokenPointer)
}
if ([string]::IsNullOrWhiteSpace($GitLabToken)) {
    Stop-WithError "Token cannot be empty."
}

$AppPort = Read-Host "Port [4567]"
if ([string]::IsNullOrWhiteSpace($AppPort)) {
    $AppPort = "4567"
}
if ($AppPort -notmatch "^\d+$" -or [int]$AppPort -lt 1 -or [int]$AppPort -gt 65535) {
    Stop-WithError "Port must be a number from 1 to 65535."
}

$EnvContents = @(
    "GITLAB_URL=$GitLabUrl"
    "GITLAB_TOKEN=$GitLabToken"
    "GITLAB_PROJECTS="
    "PORT=$AppPort"
) -join "`n"
[IO.File]::WriteAllText($EnvFile, "$EnvContents`n", [Text.UTF8Encoding]::new($false))
$GitLabToken = $null

Write-Host "`nInstalling dependencies..." -ForegroundColor Cyan
Push-Location $RootDir
try {
    & bundle config set --local path vendor/bundle
    if ($LASTEXITCODE -ne 0) { Stop-WithError "Bundler configuration failed." }
    & bundle install
    if ($LASTEXITCODE -ne 0) { Stop-WithError "Dependency installation failed." }
} finally {
    Pop-Location
}

Write-Host "`nSetup complete. Configuration is stored locally in .env.local." -ForegroundColor Green
Write-Host "Open after startup: http://127.0.0.1:$AppPort"
$RunNow = Read-Host "Start Pulse Review now? [Y/n]"
if ($RunNow -notmatch "^(n|no)$") {
    & (Join-Path $PSScriptRoot "start.ps1")
} else {
    Write-Host "`nRun start.cmd later."
}
