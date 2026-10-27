# Build Docket for Windows: venv, tests, PyInstaller (folder + portable), Inno Setup installer.
#   powershell -ExecutionPolicy Bypass -File build.ps1 [-SkipTests] [-NoInstaller] [-Version 0.1.0]
param(
    [switch]$SkipTests,
    [switch]$NoInstaller,
    [string]$Version = ""
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not $Version) {
    $Version = (Select-String -Path "src\docket\__init__.py" -Pattern '__version__ = "(.+)"').Matches[0].Groups[1].Value
}
Write-Host "Building Docket $Version"

if (-not (Test-Path ".venv\Scripts\python.exe")) {
    $py = Get-Command py -ErrorAction SilentlyContinue
    if ($py) { & py -3.12 -m venv .venv } else { & python -m venv .venv }
}
$python = ".venv\Scripts\python.exe"
& $python -m pip install --upgrade pip | Out-Null
& $python -m pip install -r requirements-dev.txt
if ($LASTEXITCODE) { throw "pip install failed" }

if (-not $SkipTests) {
    $env:QT_QPA_PLATFORM = "offscreen"
    & $python -m pytest tests -q
    if ($LASTEXITCODE) { throw "tests failed" }
    Remove-Item Env:QT_QPA_PLATFORM
}

# Windows version resource
$parts = ($Version -split "[.-]") + @("0", "0", "0", "0")
$v4 = "$([int]$parts[0]), $([int]$parts[1]), $([int]$parts[2]), 0"
$vtext = @"
VSVersionInfo(
  ffi=FixedFileInfo(filevers=($v4), prodvers=($v4), mask=0x3f, flags=0x0, OS=0x40004, fileType=0x1,
                    subtype=0x0, date=(0, 0)),
  kids=[
    StringFileInfo([StringTable('040904B0', [
      StringStruct('CompanyName', '804 Industries'),
      StringStruct('FileDescription', 'Docket PDF'),
      StringStruct('FileVersion', '$Version'),
      StringStruct('InternalName', 'Docket'),
      StringStruct('LegalCopyright', 'Copyright 804 Industries'),
      StringStruct('OriginalFilename', 'Docket.exe'),
      StringStruct('ProductName', 'Docket'),
      StringStruct('ProductVersion', '$Version')])]),
    VarFileInfo([VarStruct('Translation', [1033, 1200])])
  ]
)
"@
New-Item -ItemType Directory -Force build | Out-Null
Set-Content -Path build\version.txt -Value $vtext -Encoding UTF8

$common = @(
    "--noconfirm", "--clean", "--windowed",
    "--name", "Docket",
    "--icon", "src\docket\assets\docket.ico",
    "--version-file", "build\version.txt",
    "--paths", "src",
    "--add-data", "src\docket\assets;docket\assets",
    "--add-data", "LICENSE-NOTICE.txt;.",
    "--hidden-import", "docket.core.worker",
    "--exclude-module", "tkinter",
    "--exclude-module", "PySide6.QtQml",
    "--exclude-module", "PySide6.QtQuick",
    "--exclude-module", "PySide6.QtWebEngineCore"
)
& $python -m PyInstaller @common --onedir run.py
if ($LASTEXITCODE) { throw "PyInstaller (folder) failed" }
& $python -m PyInstaller @common --onefile --name Docket-portable run.py
if ($LASTEXITCODE) { throw "PyInstaller (portable) failed" }
Copy-Item LICENSE-NOTICE.txt dist\Docket\ -Force

# Smoke test the built app: opens, renders, exits by itself.
$tmp = Join-Path $env:TEMP "docket-smoke"
New-Item -ItemType Directory -Force $tmp | Out-Null
& $python -c "import pymupdf; d=pymupdf.open(); p=d.new_page(); p.insert_text((72,72),'smoke'); d.save(r'$tmp\smoke.pdf')"
$env:DOCKET_EXIT_AFTER_MS = "4000"; $env:DOCKET_MULTI_INSTANCE = "1"; $env:DOCKET_SMOKE_SCREENSHOT = "$tmp\smoke.png"
$p = Start-Process -FilePath "dist\Docket\Docket.exe" -ArgumentList "`"$tmp\smoke.pdf`"" -PassThru -Wait
Remove-Item Env:DOCKET_EXIT_AFTER_MS, Env:DOCKET_MULTI_INSTANCE, Env:DOCKET_SMOKE_SCREENSHOT
if ($p.ExitCode -ne 0) { throw "smoke test failed (exit $($p.ExitCode))" }
if (-not (Test-Path "$tmp\smoke.png")) { throw "smoke test made no screenshot" }
Write-Host "Smoke test passed"

if (-not $NoInstaller) {
    $iscc = @("${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe", "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
              "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1
    if (-not $iscc) { throw "Inno Setup 6 not found (winget install JRSoftware.InnoSetup)" }
    & $iscc "/DAppVersion=$Version" installer\docket.iss
    if ($LASTEXITCODE) { throw "Inno Setup failed" }
}
Write-Host "Done. See dist\"
