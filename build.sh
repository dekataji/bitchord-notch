#!/bin/sh
# Cross-compile the portable Windows binary.
set -e
export PATH="$HOME/tools/go/bin:$PATH"
cd "$(dirname "$0")"
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags "-H windowsgui -s -w" -o dist/BitChordNotch.exe .
echo "built dist/BitChordNotch.exe"
