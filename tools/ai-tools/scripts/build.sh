#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$root/dist"
cd "$root/local"
for target in darwin/arm64 darwin/amd64 windows/amd64 linux/amd64; do
 os="${target%/*}"; arch="${target#*/}"; name="aitools-$os-$arch"; if [ "$os" = windows ]; then name="$name.exe"; fi
 CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags '-s -w -X main.version=0.1.0-preview' -o "$root/dist/$name" ./cmd/aitools
 done
python3 "$root/scripts/package.py"
