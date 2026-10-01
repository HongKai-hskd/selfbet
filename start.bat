@echo off
setlocal
chcp 65001 >nul
set "ROOT=%~dp0"
cd /d "%ROOT%backend"

if not exist server.exe (
  echo server.exe not found. Run build.bat first.
  pause
  exit /b 1
)

echo SelfBet starting: http://localhost:8080
echo Close this window to stop the server.
server.exe
pause
