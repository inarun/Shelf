@echo off
REM One-click Shelf update. Double-click from Explorer or run from any shell.
REM
REM Stops any running shelf.exe (safe: all writes are atomic + SQLite WAL),
REM fast-forwards main from origin, rebuilds, and relaunches.
REM
REM If anything fails the script pauses so you can read the error.

cd /d %~dp0

echo === Stopping Shelf if running ===
taskkill /F /IM shelf.exe >nul 2>&1

echo === Pulling from origin/main ===
git pull --ff-only
if errorlevel 1 (
    echo.
    echo git pull failed. Resolve manually and retry.
    pause
    exit /b 1
)

echo === Rebuilding shelf.exe ===
go build -o shelf.exe ./cmd/shelf
if errorlevel 1 (
    echo.
    echo Build failed. The old shelf.exe has already been stopped;
    echo fix the error and run update.bat again, or rebuild manually
    echo with: go build -o shelf.exe ./cmd/shelf
    pause
    exit /b 1
)

echo === Launching shelf.exe ===
start "" "%~dp0shelf.exe"

echo.
echo Update complete.
