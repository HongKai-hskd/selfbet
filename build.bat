@echo off
setlocal
chcp 65001 >nul
set "GO=C:\Users\Kay\.workbuddy\binaries\go\versions\go\bin\go.exe"
set "NPM=C:\Users\Kay\.workbuddy\binaries\node\versions\22.22.2-3\npm.cmd"
set "ROOT=%~dp0"

echo [1/3] Building frontend...
cd /d "%ROOT%frontend"
call "%NPM%" run build
if errorlevel 1 (
  echo FRONTEND BUILD FAILED
  pause
  exit /b 1
)

echo [2/3] Copying dist to backend/web/dist ...
if exist "%ROOT%backend\web\dist" rmdir /s /q "%ROOT%backend\web\dist"
xcopy "%ROOT%frontend\dist" "%ROOT%backend\web\dist\" /e /i /q /y >nul

echo [3/3] Building backend ...
cd /d "%ROOT%backend"
"%GO%" build -o server.exe .\cmd\server
if errorlevel 1 (
  echo BACKEND BUILD FAILED
  pause
  exit /b 1
)

echo.
echo DONE. Start app with: backend\server.exe
pause
