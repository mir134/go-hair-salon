@echo off
rem ============================================================
rem Hair Salon Server - Windows stop script
rem Stops ONLY the hair-salon-server.exe located in this folder:
rem process name AND full executable path must both match, so
rem unrelated processes are never touched.
rem Tries a graceful close first, then force-kills after 5s.
rem SQLite runs in WAL mode, so a forced stop does not corrupt data.
rem Prefer Ctrl+C in the start.bat window for a graceful shutdown.
rem ============================================================
setlocal
set "APPDIR=%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -Command "$exe = [System.IO.Path]::GetFullPath((Join-Path $env:APPDIR 'hair-salon-server.exe')); $targets = @(Get-Process -Name 'hair-salon-server' -ErrorAction SilentlyContinue | Where-Object { try { $_.Path -eq $exe } catch { $false } }); if ($targets.Count -eq 0) { Write-Host '[INFO] hair-salon-server is not running.'; exit 0 }; foreach ($p in $targets) { Write-Host ('[INFO] Stopping PID {0} - {1}' -f $p.Id, $p.Path); taskkill /PID $p.Id 2>$null | Out-Null; if (-not $p.WaitForExit(5000)) { Write-Host '[WARN] Graceful stop timed out, forcing.'; taskkill /F /PID $p.Id 2>$null | Out-Null } }; Write-Host '[INFO] Stopped.'; exit 0"
set "EXITCODE=%ERRORLEVEL%"
echo [INFO] stop.bat exit code %EXITCODE%
exit /b %EXITCODE%
