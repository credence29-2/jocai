#!/usr/bin/env bash
# Builds ProjectEchoMike.exe for Windows from any OS. Usage: ./build.sh [version]
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-1.3.0}"
LDFLAGS="-H windowsgui -s -w -X github.com/credence29-2/jocai/echo-mike/internal/app.Version=${VERSION}"

go test ./...
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o dist/ProjectEchoMike.exe .
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o dist/ProjectEchoMike-arm64.exe .
ls -l dist/*.exe
