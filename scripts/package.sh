#!/usr/bin/env bash
# 三平台打包：macOS(arm64/amd64)、Linux(amd64/arm64)、Windows(amd64)。
# 产出 dist/mplay-<版本>-<os>-<arch>.{tar.gz,zip}，里面就是单个可执行文件 + README。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 1.0.0)}"
DIST="$ROOT/dist"
rm -rf "$DIST"
mkdir -p "$DIST"

build_one() {
  local os="$1" arch="$2" ext="" pkg
  [ "$os" = "windows" ] && ext=".exe"
  local name="mplay-$VERSION-$os-$arch"
  local stage="$DIST/$name"
  mkdir -p "$stage"

  echo "[package] $os/$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
    -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o "$stage/mplay$ext" .
  cp README.md "$stage/README.md"
  cp README.zh-CN.md "$stage/README.zh-CN.md"

  if [ "$os" = "windows" ]; then
    pkg="$DIST/$name.zip"
    (cd "$DIST" && zip -qr "$name.zip" "$name")
  else
    pkg="$DIST/$name.tar.gz"
    (cd "$DIST" && tar -czf "$name.tar.gz" "$name")
  fi
  rm -rf "$stage"
  echo "         -> $pkg ($(ls -lh "$pkg" | awk '{print $5}'))"
}

build_one darwin arm64
build_one darwin amd64
build_one linux amd64
build_one linux arm64
build_one windows amd64

echo
echo "[package] 全部完成："
ls -lh "$DIST" | tail -n +2
