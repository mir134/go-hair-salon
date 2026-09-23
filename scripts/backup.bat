@echo off
rem ============================================================
rem Hair Salon Server - Windows manual backup script (calls the API)
rem The backup API is admin-only and needs a login token:
rem   1) Log in as admin, take data.token from POST /api/v1/auth/login
rem   2) set HAIR_SALON_TOKEN=TOKEN_HERE
rem   3) Run this script. Override the port with HAIR_SALON_PORT, default 8080.
rem Without a token nothing is executed - the alternative path is printed.
rem Note: the server also backs up daily at BACKUP_TIME, default 23:00,
rem keeping the last 7 archives under data\backups.
rem ============================================================
setlocal
if "%HAIR_SALON_PORT%"=="" set "HAIR_SALON_PORT=8080"

if "%HAIR_SALON_TOKEN%"=="" (
  echo [INFO] HAIR_SALON_TOKEN is not set - no API call was made.
  echo        Alternative: log in to the web UI and use the Backups page manual backup button.
  echo        Or set a token and retry: set HAIR_SALON_TOKEN=TOKEN_HERE
  echo        Automatic backup runs daily at BACKUP_TIME, keeping the last 7 archives.
  exit /b 1
)

where curl >nul 2>&1
if errorlevel 1 (
  echo [ERROR] curl not found - use the web UI manual backup button instead.
  exit /b 1
)

echo [INFO] POST http://127.0.0.1:%HAIR_SALON_PORT%/api/v1/backups ...
curl -fsS -X POST -H "Authorization: Bearer %HAIR_SALON_TOKEN%" "http://127.0.0.1:%HAIR_SALON_PORT%/api/v1/backups"
if errorlevel 1 (
  echo.
  echo [ERROR] Backup request failed: server down, invalid token, or not an admin.
  exit /b 1
)
echo.
echo [INFO] Backup finished - archive stored under .\data\backups
exit /b 0
