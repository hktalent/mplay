#!/usr/bin/env bash
# 桌面安装包：mplay + urlbox + 双击启动脚本（可独立分发，不需要用户装 Go/Rust）
#
# 依赖已编译好的两个产物：
#   MPLAY_BIN   默认 bin/mplay
#   URLBOX_BIN  默认 ../urlbox/target/release/urlbox
# 可选：
#   VERSION / GOOS / GOARCH / LABEL  用于命名
#   MACOS_UNIVERSAL=1  已用 lipo 合好通用二进制（CI 用）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-1.0.0}"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
LABEL="${LABEL:-$GOOS-$GOARCH}"
MPLAY_BIN="${MPLAY_BIN:-$ROOT/bin/mplay}"
URLBOX_BIN="${URLBOX_BIN:-$ROOT/../urlbox/target/release/urlbox}"

EXE=""
[ "$GOOS" = "windows" ] && EXE=".exe"
case "$GOOS" in
  windows) URLBOX_BIN="${URLBOX_BIN%.exe}.exe" ;;
esac

[ -x "$MPLAY_BIN" ] || { echo "[desktop] 缺少 mplay 可执行文件：$MPLAY_BIN（先跑 scripts/build.sh）" >&2; exit 1; }
[ -x "$URLBOX_BIN" ] || { echo "[desktop] 缺少 urlbox 可执行文件：$URLBOX_BIN（先跑 ../urlbox/scripts/build.sh）" >&2; exit 1; }

STAGE="$ROOT/dist/mplay-desktop-$VERSION-$LABEL"
rm -rf "$STAGE"
mkdir -p "$STAGE/bin"

cp "$MPLAY_BIN" "$STAGE/bin/mplay$EXE"
cp "$URLBOX_BIN" "$STAGE/bin/urlbox$EXE"
chmod +x "$STAGE/bin/mplay$EXE" "$STAGE/bin/urlbox$EXE"

# ── 启动脚本：优先用同目录的 urlbox 打开窗口，找不到就退回系统浏览器 ──
cat > "$STAGE/start-mplay.sh" <<'SH'
#!/usr/bin/env bash
# mplay 桌面启动器：本地起服务 → urlbox 开窗口 → 关闭窗口即退出
DIR="$(cd "$(dirname "$0")" && pwd)"
export MPLAY_URLBOX="$DIR/bin/urlbox"
exec "$DIR/bin/mplay" -desktop "$@"
SH
chmod +x "$STAGE/start-mplay.sh"

cat > "$STAGE/start-mplay.command" <<'SH'
#!/usr/bin/env bash
DIR="$(cd "$(dirname "$0")" && pwd)"
export MPLAY_URLBOX="$DIR/bin/urlbox"
exec "$DIR/bin/mplay" -desktop "$@"
SH
chmod +x "$STAGE/start-mplay.command"

printf '@echo off\r\nsetlocal\r\nset MPLAY_URLBOX=%%~dp0bin\\urlbox.exe\r\n"%%~dp0bin\\mplay.exe" -desktop %%*\r\n' > "$STAGE/start-mplay.cmd"

cat > "$STAGE/使用说明.txt" <<TXT
mplay 桌面版 v${VERSION}（${LABEL}）

怎么用
  macOS    : 双击 start-mplay.command（或 start-mplay.sh）
  Linux    : ./start-mplay.sh
  Windows  : 双击 start-mplay.cmd

第一次打开
  - 会弹出窗口；窗口里点「设置」添加媒体目录（音乐/视频文件夹），会自动扫描。
  - 同名歌词/字幕（.lrc .srt .vtt .ass）会被自动识别；没有的会按歌名去歌词库匹配。
  - 也可直接把音频/视频文件拖进窗口播放。

说明
  mplay 是本机的一个 HTTP 服务 + 网页界面；urlbox 是 Rust+wry 的窗口外壳。
  关掉窗口，mplay 会自动退出（不会常驻后台）。
  桌面版默认只监听 127.0.0.1，外部访问不到；想让局域网用请自行加 -lan 参数。
TXT

# ── macOS：额外产出一个可双击的 .app ──
APP=""
if [ "$GOOS" = "darwin" ]; then
  APP="$ROOT/dist/MPlay.app"
  rm -rf "$APP"
  mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
  cp "$MPLAY_BIN" "$APP/Contents/MacOS/mplay"
  cp "$URLBOX_BIN" "$APP/Contents/MacOS/urlbox"
  chmod +x "$APP/Contents/MacOS/mplay" "$APP/Contents/MacOS/urlbox"

  cat > "$APP/Contents/MacOS/launch-mplay" <<'SH'
#!/bin/sh
DIR="$(cd "$(dirname "$0")" && pwd)"
export MPLAY_URLBOX="$DIR/urlbox"
exec "$DIR/mplay" -desktop "$@"
SH
  chmod +x "$APP/Contents/MacOS/launch-mplay"

  cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>MPlay</string>
  <key>CFBundleDisplayName</key><string>mplay</string>
  <key>CFBundleIdentifier</key><string>com.51pwn.mplay</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>launch-mplay</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSRequiresAquaSystemAppearance</key><false/>
</dict>
</plist>
PLIST

  # 未配置 Developer ID 时用 ad-hoc 签名，至少满足本机运行
  if command -v codesign >/dev/null 2>&1; then
    codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "[desktop] ad-hoc 签名失败，忽略"
  fi
fi

# ── 归档 ──
ARCHIVE=""
# 压 zip 时优先用能正确写 UTF-8 文件名的那几个工具：
# Windows 自带的 Compress-Archive 会把中文文件名压成乱码（实测 使用说明.txt → ??），
# 所以把它放到最后，前面用 7z / Python zipfile（会设置 UTF-8 flag）。
make_zip() { # $1=源路径 $2=输出 zip
  local src="$1" dst="$2"
  if command -v zip >/dev/null 2>&1; then
    (cd "$(dirname "$src")" && zip -qry "$dst" "$(basename "$src")")
  elif command -v 7z >/dev/null 2>&1; then
    7z a -tzip -bso0 -bsp0 "$dst" "$src" >/dev/null
  elif command -v python3 >/dev/null 2>&1 || command -v python >/dev/null 2>&1; then
    local py="python3"; command -v python3 >/dev/null 2>&1 || py="python"
    "$py" -c "import os, shutil, sys; src, dst = sys.argv[1], sys.argv[2]; base = dst[:-4] if dst.lower().endswith('.zip') else dst; shutil.make_archive(base, 'zip', os.path.dirname(src), os.path.basename(src))" "$src" "$dst"
  elif command -v powershell.exe >/dev/null 2>&1; then
    powershell.exe -NoProfile -Command "Compress-Archive -Path '$src' -DestinationPath '$dst' -Force"
  else
    echo "[desktop] 没有 zip/7z/python/powershell，无法生成 zip" >&2; exit 1
  fi
}

if [ "$GOOS" = "darwin" ] && [ -n "$APP" ]; then
  ARCHIVE="$ROOT/dist/mplay-desktop-$VERSION-$LABEL.zip"
  rm -f "$ARCHIVE"
  if command -v ditto >/dev/null 2>&1; then
    ditto -c -k --keepParent "$APP" "$ARCHIVE"
  else
    make_zip "$APP" "$ARCHIVE"
  fi
  # 同时给不用 .app 的人一份解压即用的目录（含启动脚本）
  (cd "$ROOT/dist" && tar -czf "mplay-desktop-$VERSION-$LABEL-cli.tar.gz" "mplay-desktop-$VERSION-$LABEL")
  echo "[desktop] $ARCHIVE"
  echo "[desktop] $ROOT/dist/mplay-desktop-$VERSION-$LABEL-cli.tar.gz"
elif [ "$GOOS" = "windows" ]; then
  ARCHIVE="$ROOT/dist/mplay-desktop-$VERSION-$LABEL.zip"
  rm -f "$ARCHIVE"
  make_zip "$STAGE" "$ARCHIVE"
  echo "[desktop] $ARCHIVE"
else
  ARCHIVE="$ROOT/dist/mplay-desktop-$VERSION-$LABEL.tar.gz"
  (cd "$ROOT/dist" && tar -czf "$(basename "$ARCHIVE")" "mplay-desktop-$VERSION-$LABEL")
  echo "[desktop] $ARCHIVE"
fi

ls -lh "$ROOT/dist" | tail -n +2
