#!/usr/bin/env bash
set -euo pipefail
export PATH="/c/Program Files/Go/bin:$PATH"

echo "Generating documentation (dicomtool-manual.md and .docx)..."
go run ./gendoc

echo "Building dicomtool release for all platforms..."

go build -ldflags="-s -w" -o dicomtool.exe .
echo "  windows/amd64: dicomtool.exe"

CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o dicomtool-linux .
echo "  linux/amd64:   dicomtool-linux"

CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dicomtool-mac-x64 .
echo "  darwin/amd64:  dicomtool-mac-x64"

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dicomtool-mac-arm64 .
echo "  darwin/arm64:  dicomtool-mac-arm64"

echo "Done."
