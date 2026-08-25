@echo off
setlocal
cd /d "%~dp0"
if not exist ".env.local" (
  echo Настройки не найдены. Сначала запустите setup.cmd.
  pause
  exit /b 1
)
if not exist "pulse-review.exe" (
  echo pulse-review.exe не найден. Распакуйте ZIP полностью.
  pause
  exit /b 1
)
start "Pulse Review" "%~dp0pulse-review.exe"
timeout /t 2 /nobreak >nul
start "" "http://127.0.0.1:4567"
