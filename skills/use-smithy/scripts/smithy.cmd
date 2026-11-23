@echo off
rem smithy launcher for Windows shells without sh: same order as the `smithy` script beside it
rem ($SMITHY_BIN, then %USERPROFILE%\.smithy\bin\<VERSION>\smithy.exe, downloaded once with gh, or
rem curl.exe with timeouts when gh is missing, from the GitHub release, checked against its checksums.txt and its build attestation; with an
rem gh missing or too old for `gh attestation`, nothing is installed unless SMITHY_ALLOW_UNATTESTED=1).
setlocal enabledelayedexpansion
set "SMITHY_SKILL_DIR=%~dp0"
if defined SMITHY_BIN ( "%SMITHY_BIN%" %* & exit /b !errorlevel! )
if defined SMITHY_HOME ( set "home=%SMITHY_HOME%" ) else ( set "home=%USERPROFILE%\.smithy" )
set /p version=<"%~dp0VERSION"
set "bin=%home%\bin\%version%\smithy.exe"
if not exist "%bin%" (
  set "asset=smithy-windows-amd64.exe"
  set "tmp=%TEMP%\smithy-%RANDOM%"
  mkdir "!tmp!" >nul
  echo smithy: downloading v%version% ^(!asset!^), once 1>&2
  set "url=https://github.com/Zovaris/smithy/releases/download/v%version%"
  where gh >nul 2>&1
  if !errorlevel! equ 0 (
    gh release download "v%version%" -R Zovaris/smithy -p "!asset!" -p checksums.txt -D "!tmp!" >nul 2>"!tmp!\err.txt"
  ) else (
    curl.exe -fsSL --proto "=https" --proto-redir "=https" --connect-timeout 10 --max-time 120 -o "!tmp!\!asset!" "!url!/!asset!" 2>"!tmp!\err.txt" && curl.exe -fsSL --proto "=https" --proto-redir "=https" --connect-timeout 10 --max-time 120 -o "!tmp!\checksums.txt" "!url!/checksums.txt" 2>>"!tmp!\err.txt"
  )
  if not exist "!tmp!\!asset!" goto :nodownload
  if not exist "!tmp!\checksums.txt" goto :nodownload
  set "want=" & set "got="
  for /f "tokens=1,2" %%a in (!tmp!\checksums.txt) do if "%%b"=="!asset!" set "want=%%a"
  for /f "tokens=*" %%h in ('certutil -hashfile "!tmp!\!asset!" SHA256 ^| findstr /v ":"') do if not defined got set "got=%%h"
  set "got=!got: =!"
  if not defined want ( echo smithy: checksum mismatch for !asset!, not installed 1>&2 & exit /b 3 )
  if not defined got ( echo smithy: checksum mismatch for !asset!, not installed 1>&2 & exit /b 3 )
  if /i not "!want!"=="!got!" ( echo smithy: checksum mismatch for !asset!, not installed 1>&2 & exit /b 3 )
  gh attestation verify --help >nul 2>&1
  if !errorlevel! equ 0 (
    gh attestation verify "!tmp!\!asset!" -R Zovaris/smithy >nul 2>&1 || ( echo smithy: !asset! has no valid build attestation for Zovaris/smithy, not installed 1>&2 & exit /b 3 )
  ) else if not "%SMITHY_ALLOW_UNATTESTED%"=="1" (
    echo smithy: can't verify the build attestation of !asset!: install or update gh, or set SMITHY_ALLOW_UNATTESTED=1 to accept a checksum-only install; not installed 1>&2
    exit /b 3
  )
  mkdir "%home%\bin\%version%" >nul 2>&1
  move /y "!tmp!\!asset!" "%bin%" >nul
  copy /y "%bin%" "%home%\bin\smithy.exe" >nul
  rmdir /s /q "!tmp!"
)
"%bin%" %*
exit /b %errorlevel%

:nodownload
set "why=network error or release not found"
if exist "!tmp!\err.txt" for /f "usebackq delims=" %%e in ("!tmp!\err.txt") do if not defined reason set "reason=%%e"
if defined reason set "why=!reason!"
echo smithy: can't download v%version% (!asset!): !why! 1>&2
rmdir /s /q "!tmp!" >nul 2>&1
exit /b 3
