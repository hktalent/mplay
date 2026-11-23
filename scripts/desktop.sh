#!/usr/bin/env bash
# 桌面模式：本地起服务，再用 urlbox（Rust + wry 的 URL 外壳）打开窗口。
# 同一份 web 前端既跑在浏览器里，也跑在这个窗口里。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

"$ROOT/scripts/build.sh"

URLBOX="${MPLAY_URLBOX:-$ROOT/../urlbox/target/release/urlbox}"
if [ ! -x "$URLBOX" ]; then
  echo "[desktop] 未找到 urlbox：$URLBOX" >&2
  if [ -d "$ROOT/../urlbox" ]; then
    echo "[desktop] 尝试在 ../urlbox 里编译（需要 Rust 工具链）…" >&2
    (cd "$ROOT/../urlbox" && ./scripts/build.sh)
    URLBOX="$ROOT/../urlbox/target/release/urlbox"
  else
    echo "[desktop] 请先设置 MPLAY_URLBOX=/path/to/urlbox" >&2
    exit 1
  fi
fi

export MPLAY_URLBOX="$URLBOX"
echo "[desktop] urlbox: $URLBOX"
exec "$ROOT/bin/mplay" -desktop "$@"
