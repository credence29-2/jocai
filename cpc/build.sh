#!/usr/bin/env bash
# Builds Kempt.exe for Windows from any OS. Usage: ./build.sh [version]
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-1.5.1}"
LDFLAGS="-H windowsgui -s -w -X main.version=${VERSION}"

go test ./...
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o dist/Kempt.exe .
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o dist/Kempt-arm64.exe .
ls -l dist/*.exe
