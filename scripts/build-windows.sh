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

ZIP="build/DEOMinute-${VERSION}-windows-amd64.zip"
rm -f "$ZIP"
( cd build/bin && zip -q "../$(basename "$ZIP")" DEOMinute.exe )
[ -f README.md ] && zip -q -j "$ZIP" README.md
echo "built $ZIP"
