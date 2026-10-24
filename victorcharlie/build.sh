#!/usr/bin/env bash
# Builds the web app into the server, then the server for Windows and Linux.
# Usage: ./build.sh [version]
set -euo pipefail
cd "$(dirname "$0")"
version="${1:-dev}"

(cd web && npm ci --no-audit --no-fund && npm run build)
touch internal/webui/dist/.keep

mkdir -p dist
flags="-s -w -X main.version=$version"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$flags" -o dist/victorcharlie-server.exe .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$flags" -o dist/victorcharlie-server-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$flags" -o dist/victorcharlie-server-linux-arm64 .
ls -la dist
