@echo off
rem smithy launcher for Windows shells without sh: runs %SMITHY_BIN%, else
rem %LOCALAPPDATA%\smithy\versions\<VERSION>\smithy.exe, else %LOCALAPPDATA%\Programs\smithy\smithy.exe
rem with a notice naming `smithy update <VERSION>`. It never downloads anything: the repo's
rem install.sh (README: Install) does.
setlocal enabledelayedexpansion
set "SMITHY_SKILL_DIR=%~dp0"
if defined SMITHY_BIN ( "%SMITHY_BIN%" %* & exit /b !errorlevel! )
if defined LOCALAPPDATA ( set "lad=%LOCALAPPDATA%" ) else ( set "lad=%USERPROFILE%\AppData\Local" )
set /p version=<"%~dp0VERSION"
if exist "%lad%\smithy\versions\%version%\smithy.exe" (
  "%lad%\smithy\versions\%version%\smithy.exe" %* & exit /b !errorlevel!
)
if exist "%lad%\Programs\smithy\smithy.exe" (
  echo smithy: v%version% is not installed; using %lad%\Programs\smithy\smithy.exe. Ask the user whether to run: smithy update %version% 1>&2
  "%lad%\Programs\smithy\smithy.exe" %* & exit /b !errorlevel!
)
echo smithy: the smithy binary v%version% is not installed. the smithy skills need it. Ask the user whether you install it or they do ^(https://github.com/Zovaris/smithy#install^). 1>&2
exit /b 3
