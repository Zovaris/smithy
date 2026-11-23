@echo off
rem smithy launcher for Windows shells without sh: runs %SMITHY_BIN%, else
rem %USERPROFILE%\.smithy\bin\<VERSION>\smithy.exe, else %USERPROFILE%\.smithy\bin\smithy.exe
rem with a notice. It never downloads anything: the repo's install.sh (README: Install) does.
setlocal enabledelayedexpansion
set "SMITHY_SKILL_DIR=%~dp0"
if defined SMITHY_BIN ( "%SMITHY_BIN%" %* & exit /b !errorlevel! )
if defined SMITHY_HOME ( set "home=%SMITHY_HOME%" ) else ( set "home=%USERPROFILE%\.smithy" )
set /p version=<"%~dp0VERSION"
set "bin=%home%\bin\%version%\smithy.exe"
if exist "%bin%" ( "%bin%" %* & exit /b !errorlevel! )
if exist "%home%\bin\smithy.exe" (
  echo smithy: v%version% is not installed; using %home%\bin\smithy.exe. Ask the user whether to install v%version% now ^(README: Install^) 1>&2
  "%home%\bin\smithy.exe" %* & exit /b !errorlevel!
)
echo smithy: the smithy binary v%version% is not installed. the smithy skills need it. Ask the user whether you install it or they do ^(https://github.com/Zovaris/smithy#install^). 1>&2
exit /b 3
