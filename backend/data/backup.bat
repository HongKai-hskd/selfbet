@echo off
setlocal
chcp 65001 >nul

rem ============================================
rem  SelfBet DB backup
rem  copies selfbet.db -> backups\selfbet-backup-YYYYMMDD-HHMM.db
rem  relative paths only (%~dp0 = this script's folder)
rem ============================================

cd /d "%~dp0"

rem timestamp YYYYMMDD-HHMM via powershell (locale-independent)
for /f %%i in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd-HHmm"') do set TS=%%i

if "%TS%"=="" (
    echo [FAIL] cannot build timestamp
    pause
    exit /b 1
)

if not exist "backups" mkdir "backups"

copy /y "selfbet.db" "backups\selfbet-backup-%TS%.db" >nul
if errorlevel 1 (
    echo [FAIL] backup selfbet.db
    pause
    exit /b 1
)

echo [OK] backups\selfbet-backup-%TS%.db
pause
