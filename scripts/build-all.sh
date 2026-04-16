#!/bin/bash
# 
# build-all.sh — Cross-compile bale-transport for all platforms
# 
set -e

VERSION=${VERSION:-"0.1.0"}
LDFLAGS="-s -w -X main.version=${VERSION}"
OUTPUT_DIR="dist"

mkdir -p "$OUTPUT_DIR"

echo ""
echo "  Mithra · Building bale-transport v${VERSION}"
echo ""

# Run tests first
echo "[1/5] Running tests..."
go test ./bale/ -v -count=1
echo ""

# Linux AMD64
echo "[2/5] Building linux/amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/bale-transport-linux-amd64" ./cmd/bale-transport/
echo "  ✓ $OUTPUT_DIR/bale-transport-linux-amd64"

# Android ARM64
echo "[3/5] Building linux/arm64 (Android)..."
GOOS=linux GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/bale-transport-linux-arm64" ./cmd/bale-transport/
echo "  ✓ $OUTPUT_DIR/bale-transport-linux-arm64"

# Windows AMD64
echo "[4/5] Building windows/amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/bale-transport-windows-amd64.exe" ./cmd/bale-transport/
echo "  ✓ $OUTPUT_DIR/bale-transport-windows-amd64.exe"

# macOS ARM64 (Apple Silicon)
echo "[5/5] Building darwin/arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "$OUTPUT_DIR/bale-transport-darwin-arm64" ./cmd/bale-transport/
echo "  ✓ $OUTPUT_DIR/bale-transport-darwin-arm64"

echo ""
echo "  Build complete. Binaries in $OUTPUT_DIR/"
ls -lh "$OUTPUT_DIR/"
echo ""
