@echo off
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0windows\start-go.ps1"
exit /b %ERRORLEVEL%
