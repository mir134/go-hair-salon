@echo off
rem ============================================================
rem Hair Salon Server - Windows start script
rem Put this script next to hair-salon-server.exe (deploy folder).
rem Stop: press Ctrl+C in this window, or run stop.bat
rem ============================================================
setlocal
cd /d "%~dp0"

if not exist "hair-salon-server.exe" (
  echo [ERROR] hair-salon-server.exe not found in this folder.
  echo         Copy this script into the deploy folder, next to the exe.
  exit /b 1
)

if not exist "config.yaml" (
  if not exist "config.example.yaml" (
    echo [ERROR] config.yaml not found and no config.example.yaml template.
    echo         Create config.yaml with JWT_SECRET and the other required keys.
    exit /b 1
  )
  copy /y "config.example.yaml" "config.yaml" >nul
  echo [INFO] config.yaml created from config.example.yaml.
  echo [ERROR] Edit config.yaml and set JWT_SECRET, then run this script again.
  exit /b 1
)

if not exist "web\dist\index.html" (
  echo [WARN] web\dist\index.html not found - the web UI will be unavailable, API only.
  echo        Copy the frontend build output to .\web\dist
)

echo [INFO] First run: make sure JWT_SECRET in config.yaml is set, empty means startup is refused.
echo [INFO] Data: .\data - local disk only, never a network share. Backups: .\data\backups. Logs: .\logs
echo [INFO] Local access: http://127.0.0.1:8080
echo [INFO] LAN access:   http://THIS-PC-LAN-IP:8080 - allow TCP 8080 in Windows Firewall first.
echo [INFO] Press Ctrl+C to stop the server.
echo.
"hair-salon-server.exe"
set "EXITCODE=%ERRORLEVEL%"
echo.
echo [INFO] Server exited with code %EXITCODE%
exit /b %EXITCODE%
