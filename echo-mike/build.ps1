# Builds ProjectEchoMike.exe. Usage: .\build.ps1 [version]
param([string]$Version = "1.0.0")
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$ldflags = "-H windowsgui -s -w -X main.version=$Version"

go test ./...
if ($LASTEXITCODE) { exit $LASTEXITCODE }
New-Item -ItemType Directory -Force dist | Out-Null
$env:GOOS = "windows"
$env:GOARCH = "amd64"; go build -trimpath -ldflags $ldflags -o dist\ProjectEchoMike.exe .
$env:GOARCH = "arm64"; go build -trimpath -ldflags $ldflags -o dist\ProjectEchoMike-arm64.exe .
Remove-Item Env:GOOS, Env:GOARCH
Get-ChildItem dist\*.exe
