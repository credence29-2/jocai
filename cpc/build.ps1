# Builds Kempt.exe. Usage: .\build.ps1 [-Version 1.0.0]
param([string]$Version = "1.5.0")
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$ldflags = "-H windowsgui -s -w -X main.version=$Version"

go test ./...
if ($LASTEXITCODE -ne 0) { throw "Tests failed" }

New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:GOOS = "windows"
$env:GOARCH = "amd64"; go build -trimpath -ldflags $ldflags -o dist\Kempt.exe .
if ($LASTEXITCODE -ne 0) { throw "Build failed" }
$env:GOARCH = "arm64"; go build -trimpath -ldflags $ldflags -o dist\Kempt-arm64.exe .
if ($LASTEXITCODE -ne 0) { throw "Build failed" }
Remove-Item Env:GOOS, Env:GOARCH

Get-ChildItem dist\*.exe | Format-Table Name, Length
