$ErrorActionPreference = "Stop"

$RootDir = $PSScriptRoot
$EnvFile = Join-Path $RootDir ".env.local"
$Binary = Join-Path $RootDir "pulse-review.exe"

function Stop-WithError([string]$Message) {
    Write-Host "`nОшибка: $Message" -ForegroundColor Red
    exit 1
}

if (-not (Test-Path $Binary)) {
    Stop-WithError "Файл pulse-review.exe не найден рядом с setup.cmd. Распакуйте ZIP полностью."
}

Write-Host "`nPulse Review - настройка Windows" -ForegroundColor Cyan
$KeepConfiguration = $false
if (Test-Path $EnvFile) {
    $Keep = Read-Host "`n.env.local уже существует. Сохранить текущие настройки? [Y/n]"
    $KeepConfiguration = $Keep -notmatch "^(n|no|нет)$"
}

if (-not $KeepConfiguration) {
    $GitLabUrl = Read-Host "`nАдрес GitLab [https://gitlab.com]"
    if ([string]::IsNullOrWhiteSpace($GitLabUrl)) { $GitLabUrl = "https://gitlab.com" }
    $GitLabUrl = $GitLabUrl.TrimEnd("/")
    if ($GitLabUrl -notmatch "^https?://") { Stop-WithError "Адрес GitLab должен начинаться с http:// или https://" }

    $SecureToken = Read-Host "GitLab access token (ввод скрыт)" -AsSecureString
    $TokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureToken)
    try { $GitLabToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($TokenPointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($TokenPointer) }
    if ([string]::IsNullOrWhiteSpace($GitLabToken)) { Stop-WithError "Токен не может быть пустым." }

    $AppPort = Read-Host "Порт [4567]"
    if ([string]::IsNullOrWhiteSpace($AppPort)) { $AppPort = "4567" }
    if ($AppPort -notmatch "^\d+$" -or [int]$AppPort -lt 1 -or [int]$AppPort -gt 65535) { Stop-WithError "Порт должен быть числом от 1 до 65535." }

    $Contents = @("GITLAB_URL=$GitLabUrl", "GITLAB_TOKEN=$GitLabToken", "GITLAB_PROJECTS=", "PORT=$AppPort") -join "`n"
    [IO.File]::WriteAllText($EnvFile, "$Contents`n", [Text.UTF8Encoding]::new($false))
    $GitLabToken = $null
}

Write-Host "`nНастройка завершена." -ForegroundColor Green
$RunNow = Read-Host "Запустить Pulse Review сейчас? [Y/n]"
if ($RunNow -notmatch "^(n|no|нет)$") {
    & (Join-Path $RootDir "start.cmd")
} else {
    Write-Host "Позже запустите start.cmd."
}
