@echo off
rem smithy launcher for Windows shells without sh: runs %SMITHY_BIN%, else
rem %LOCALAPPDATA%\smithy\versions\<VERSION>\smithy.exe (or the pre-0.21
rem %USERPROFILE%\.smithy\bin\<VERSION>\smithy.exe), else %LOCALAPPDATA%\Programs\smithy\smithy.exe
rem or %USERPROFILE%\.smithy\bin\smithy.exe with a notice. It never downloads anything: the
rem repo's install.sh (README: Install) does.
setlocal enabledelayedexpansion
set "SMITHY_SKILL_DIR=%~dp0"
if defined SMITHY_BIN ( "%SMITHY_BIN%" %* & exit /b !errorlevel! )
if defined SMITHY_HOME ( set "home=%SMITHY_HOME%" ) else ( set "home=%USERPROFILE%\.smithy" )
if defined LOCALAPPDATA ( set "lad=%LOCALAPPDATA%" ) else ( set "lad=%USERPROFILE%\AppData\Local" )
set /p version=<"%~dp0VERSION"
for %%b in ("%lad%\smithy\versions\%version%\smithy.exe" "%home%\bin\%version%\smithy.exe") do (
  if exist %%b ( %%b %* & exit /b !errorlevel! )
)
for %%b in ("%lad%\Programs\smithy\smithy.exe" "%home%\bin\smithy.exe") do (
  if exist %%b (
    echo smithy: v%version% is not installed; using %%~b. Ask the user whether to install v%version% now ^(README: Install^) 1>&2
    %%b %* & exit /b !errorlevel!
  )
)
echo smithy: the smithy binary v%version% is not installed. the smithy skills need it. Ask the user whether you install it or they do ^(https://github.com/Zovaris/smithy#install^). 1>&2
exit /b 3
