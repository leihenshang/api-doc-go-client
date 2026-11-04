#!/usr/bin/env bash
# 版本号一致性门禁：wails.json / frontend/package.json / TitleBar.vue 必须同一个版本。
#
# 为什么需要：三处任一漂移都会让用户看到错的版本（0.1.8 时标题栏还是 v0.0.1），
# 而自动更新又依赖这个版本号判断「要不要升级」—— 漂移 = 更新永远不触发。
set -euo pipefail
cd "$(dirname "$0")/.."

W=$(grep -oP '"productVersion"\s*:\s*"\K[^"]+' wails.json | head -1)
P=$(grep -oP '^  "version"\s*:\s*"\K[^"]+' frontend/package.json | head -1)
T=$(grep -oP "APP_VERSION = '\K[^']+" frontend/src/lib/version.ts | sed 's/^v//')

fail=0
[ "$W" = "$P" ] || { echo "wails.json=$W 与 package.json=$P 不一致" >&2; fail=1; }
[ "$W" = "$T" ] || { echo "wails.json=$W 与 TitleBar.vue=$T 不一致" >&2; fail=1; }
[ -n "$W" ] || { echo "wails.json 缺少 productVersion" >&2; fail=1; }
[ "$fail" = 0 ] || { echo "❌ 版本号漂移：改版本请用 scripts/release.sh <版本>" >&2; exit 1; }
echo "版本号一致：v$W"
