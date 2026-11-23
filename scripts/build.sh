#!/usr/bin/env bash
# 编译 mplay 单二进制（前端已 go:embed，产物自带页面，无需额外资源）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 1.0.0)}"
OUT="${OUT:-bin}"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
EXE=""
[ "$GOOS" = "windows" ] && EXE=".exe"

mkdir -p "$OUT"
echo "[build] $GOOS/$GOARCH  version=$VERSION  -> $OUT/mplay$EXE"
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$OUT/mplay$EXE" .

echo "[build] 完成：$(ls -lh "$OUT/mplay$EXE" | awk '{print $5}')"
