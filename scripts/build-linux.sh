#!/usr/bin/env sh
# Linux向け claude-tts-tray をビルドする(cgo不要・静的リンク)。
# Windows/Mac/Linux いずれの Go 環境でもクロスビルド可能。
set -e
cd "$(dirname "$0")/.."
mkdir -p dist
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/claude-tts-tray-linux-amd64 .
echo "built: $(pwd)/dist/claude-tts-tray-linux-amd64"
