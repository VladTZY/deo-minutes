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
# The guide as Windows-friendly text (CRLF) so it opens in Notepad.
sed 's/$/\r/' README.md > "$PKG/CITESTE-MA.txt"
sed 's/$/\r/' scripts/package/THIRD-PARTY-NOTICES.txt > "$PKG/THIRD-PARTY-NOTICES.txt"
if [ -n "${NO_MODEL:-}" ] && [ -f build/dist/whisper/whisper-cli.exe ]; then
  # Small package: the app downloads the speech model on first use.
  cp -R build/dist/whisper "$PKG/"
  mkdir -p "$PKG/models" && cp build/dist/models/ggml-silero-v6.2.0.bin "$PKG/models/"
  [ -f build/cache/whisper.cpp/LICENSE ] && sed 's/$/\r/' build/cache/whisper.cpp/LICENSE > "$PKG/whisper/LICENSE-whisper.cpp.txt"
elif [ -f build/dist/models/ggml-whisper-ro.bin ] && [ -f build/dist/whisper/whisper-cli.exe ]; then
  cp -R build/dist/whisper build/dist/models "$PKG/"
  [ -f build/cache/whisper.cpp/LICENSE ] && sed 's/$/\r/' build/cache/whisper.cpp/LICENSE > "$PKG/whisper/LICENSE-whisper.cpp.txt"
else
  echo "note: local transcriber not prepared (scripts/prepare-local-stt.sh); DEMO step 2 will report it missing" >&2
fi

ZIP="build/DEOMinute-${VERSION}-windows-amd64${NO_MODEL:+-lite}.zip"
rm -f "$ZIP"
( cd build/pkg && zip -q -r "../$(basename "$ZIP")" DEOMinute )
echo "built $ZIP"
