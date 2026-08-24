$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path (Join-Path $RootDir ".env.local"))) {
    Write-Host "Configuration was not found. Run setup.cmd first." -ForegroundColor Red
    exit 1
}

Set-Location $RootDir
& bundle exec ruby local_server.rb
exit $LASTEXITCODE
