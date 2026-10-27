#!/usr/bin/env bash
# Builds the portable Windows app: build/bin/DEOMinute.exe and
# build/DEOMinute-<version>-windows-amd64.zip. No installer: the exe runs from
# any folder without admin rights.
set -euo pipefail
cd "$(dirname "$0")/.."

export PATH="$HOME/sdk/go/bin:$HOME/go/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"

go vet ./...
GOOS=windows golangci-lint run ./...
go test -short ./...

wails build -clean -platform windows/amd64 -trimpath -ldflags "-s -w -X main.version=${VERSION}"

# Portable folder: DEOMinute.exe, plus the DEMO-mode local transcriber when
# scripts/prepare-local-stt.sh has prepared it (whisper\ and models\).
PKG="build/pkg/DEOMinute"
rm -rf build/pkg && mkdir -p "$PKG"
cp build/bin/DEOMinute.exe "$PKG/"
[ -f README.md ] && cp README.md "$PKG/"
if [ -f build/dist/models/ggml-whisper-ro.bin ] && [ -f build/dist/whisper/whisper-cli.exe ]; then
  cp -R build/dist/whisper build/dist/models "$PKG/"
else
  echo "note: local transcriber not prepared (scripts/prepare-local-stt.sh); DEMO step 2 will report it missing" >&2
fi

ZIP="build/DEOMinute-${VERSION}-windows-amd64.zip"
rm -f "$ZIP"
( cd build/pkg && zip -q -r "../$(basename "$ZIP")" DEOMinute )
echo "built $ZIP"
