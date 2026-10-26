#!/usr/bin/env bash
# Builds the Go mail engine into app/libs/echomike.aar for the Android app.
# Needs the Android SDK and NDK (ANDROID_HOME, ANDROID_NDK_HOME).
# Usage: android/build-engine.sh [version]
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:-1.2.0}"

# The versions pinned in go.mod.
go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
PATH="$(go env GOPATH)/bin:$PATH"

mkdir -p android/app/libs
gomobile bind \
  -target=android/arm64,android/arm,android/amd64 \
  -androidapi 29 \
  -trimpath \
  -ldflags "-s -w -X github.com/credence29-2/jocai/echo-mike/internal/app.Version=${VERSION}" \
  -o android/app/libs/echomike.aar \
  ./mobile
ls -l android/app/libs/echomike.aar
