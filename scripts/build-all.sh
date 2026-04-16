#!/usr/bin/env bash
#
# build-all.sh — Build and verify the bale-transport repository
#
# This repository ships a Go package (`bale/`), a SingBox adapter
# (`transport/v2raybale/`), and a Node.js server-side unwrapper
# (`server/unwrapper/`). The Go components are consumed as a
# library — there is no standalone Go binary to build here.
#
# This script:
#   1. gofmt check (fails if any file is not gofmt-clean)
#   2. go vet
#   3. go test ./bale/ with race detector
#   4. Node syntax check for the unwrapper
#
# The Cloudflare Worker deployment and the hiddify-sing-box build
# live in their sibling repositories; see the README for pointers.
# 

set -euo pipefail

cd "$(dirname "$0")/.."

echo ""
echo "  Mithra · bale-transport build & verify"
echo ""

# Go: gofmt 
echo "[1/4] gofmt check..."
UNFORMATTED="$(gofmt -l bale/ transport/ patches/ || true)"
if [ -n "$UNFORMATTED" ]; then
  echo "gofmt failures:"
  echo "$UNFORMATTED" | sed 's/^/    /'
  echo "run: gofmt -w bale/ transport/ patches/"
  exit 1
fi
echo "gofmt clean"

# Go: vet 
echo "[2/4] go vet..."
go vet ./bale/
echo "go vet clean"

# Go: tests 
echo "[3/4] go test ./bale/..."
go test ./bale/ -v -count=1 -race
echo ""

# Node: syntax check 
echo "[4/4] node syntax check on unwrapper..."
node --check server/unwrapper/unwrapper.js
echo "unwrapper.js syntax OK"

echo ""
echo "Build & verify complete."
echo ""
