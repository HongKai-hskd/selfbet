@echo off
setlocal
chcp 65001 >nul
set "GO=C:\Users\Kay\.workbuddy\binaries\go\versions\go\bin\go.exe"
set "NPM=C:\Users\Kay\.workbuddy\binaries\node\versions\22.22.2-3\npm.cmd"
set "ROOT=%~dp0"
rem Go env (fixed, do not rely on session env): CN proxy + isolated caches
set "GOPROXY=https://goproxy.cn,direct"
set "GOPATH=C:\Users\Kay\.workbuddy\binaries\go\gopath"
set "GOMODCACHE=C:\Users\Kay\.workbuddy\binaries\go\gopath\pkg\mod"
set "GOCACHE=C:\Users\Kay\.workbuddy\binaries\go\gocache"

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

echo [3/4] Building backend (Windows) ...
cd /d "%ROOT%backend"
"%GO%" build -o server.exe .\cmd\server
if errorlevel 1 (
  echo BACKEND BUILD FAILED
  pause
  exit /b 1
)

echo [4/4] Building backend (Linux amd64) ...
cd /d "%ROOT%backend"
set "CGO_ENABLED=0"
set "GOOS=linux"
set "GOARCH=amd64"
"%GO%" build -o server-linux-amd64 .\cmd\server
if errorlevel 1 (
  echo LINUX BUILD FAILED
  pause
  exit /b 1
)

echo.
echo DONE.
echo   Windows: backend\server.exe
echo   Linux:   backend\server-linux-amd64  (chmod +x then run on Linux)
pause
