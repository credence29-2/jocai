#!/usr/bin/env bash
# Builds CPC.exe for Windows from any OS. Usage: ./build.sh [version]
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-1.3.0}"
LDFLAGS="-H windowsgui -s -w -X main.version=${VERSION}"

go test ./...
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o dist/CPC.exe .
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o dist/CPC-arm64.exe .
ls -l dist/*.exe
