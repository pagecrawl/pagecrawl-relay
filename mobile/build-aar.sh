#!/usr/bin/env bash
# Builds the relay as an Android library (relay.aar) for the Android relay app.
#
#   mobile/build-aar.sh [output-dir]
#
# Needs Go, the Android SDK and NDK, and gomobile:
#   go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init
# Real phones are arm64; x86_64 is there so the app also runs on the Android emulator.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
out="${1:-$here/../android/app/libs}"

export ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
if [[ -z "${ANDROID_NDK_HOME:-}" ]]; then
  ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/* | sort -V | tail -1)"
  export ANDROID_NDK_HOME
fi
export PATH="$PATH:$(go env GOPATH)/bin"

mkdir -p "$out"
cd "$here"
gomobile bind \
  -target=android/arm64,android/amd64 \
  -androidapi 26 \
  -javapkg io.pagecrawl.relay.go \
  -trimpath \
  -ldflags "-s -w" \
  -o "$out/relay.aar" \
  .

echo "Built $out/relay.aar"
