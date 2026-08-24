@echo off
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0windows\setup.ps1"
exit /b %ERRORLEVEL%
