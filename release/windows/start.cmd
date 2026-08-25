@echo off
setlocal
cd /d "%~dp0"
if not exist ".env.local" (
  echo Configuration was not found. Run setup.cmd first.
  pause
  exit /b 1
)
if not exist "pulse-review.exe" (
  echo pulse-review.exe was not found. Extract the complete ZIP archive first.
  pause
  exit /b 1
)
set "PORT=4567"
for /f "usebackq tokens=1,* delims==" %%A in (".env.local") do if /i "%%A"=="PORT" set "PORT=%%B"
start "Pulse Review" "%~dp0pulse-review.exe"
timeout /t 2 /nobreak >nul
start "" "http://127.0.0.1:%PORT%"
