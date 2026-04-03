@echo off
setlocal
set "CSC_IDENTITY_AUTO_DISCOVERY=false"
cd /d "%~dp0\..\..\"
call "%ProgramFiles%\nodejs\npm.cmd" run build
exit /b %ERRORLEVEL%
