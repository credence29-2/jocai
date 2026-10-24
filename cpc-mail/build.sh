#!/usr/bin/env bash
# Builds CPCMail.exe for Windows from any OS. Usage: ./build.sh [version]
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-1.0.0}"
LDFLAGS="-H windowsgui -s -w -X main.version=${VERSION}"

go test ./...
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o dist/CPCMail.exe .
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o dist/CPCMail-arm64.exe .
ls -l dist/*.exe
