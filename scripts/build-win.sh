#!/usr/bin/env bash
# 在 Linux 上交叉编译 Windows exe（纯 Go，无 CGO）。
#
# 能做什么：产出一个可双击运行的 api-doc-client.exe，用来让人先试。
# 不能做什么：出不了 NSIS 安装包，也没有 wails 生成的 exe 资源（图标/版本信息/DPI manifest）——
#           正式发布请在 Windows 上跑 scripts/build-win.ps1。
#
# 用法：scripts/build-win.sh              # 正式版
#       scripts/build-win.sh --dev        # 开发版（带 devbuild 标签）
#       scripts/build-win.sh --no-frontend# 跳过前端构建，直接复用 frontend/dist
set -euo pipefail
cd "$(dirname "$0")/.."

DEV=""
FRONT=1
for a in "$@"; do
  case "$a" in
    --dev) DEV=1 ;;
    --no-frontend) FRONT=0 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "未知参数：$a" >&2; exit 2 ;;
  esac
done

VER=$(grep -oP '"productVersion"\s*:\s*"\K[^"]+' wails.json | head -1)
[ -n "$VER" ] || { echo "wails.json 缺少 productVersion" >&2; exit 1; }

if [ "$FRONT" = 1 ]; then
  echo "[build-win] 构建前端"
  ( cd frontend && { [ -d node_modules ] || npm ci; } && npm run build )
fi
[ -f frontend/dist/index.html ] || { echo "缺少 frontend/dist（先跑 npm run build）" >&2; exit 1; }

TAGS="desktop,production"
OUT="build/bin/api-doc-client.exe"
if [ -n "$DEV" ]; then
  TAGS="$TAGS,devbuild"
  OUT="build/bin/api-doc-client-dev.exe"
fi

mkdir -p build/bin
echo "[build-win] 交叉编译 $OUT（v$VER, tags=$TAGS）"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -tags "$TAGS" -trimpath \
  -ldflags "-H windowsgui -s -w" \
  -o "$OUT" .

ls -lh "$OUT" | awk '{print "[build-win] 产物: " $9 "  " $5}'
echo "[build-win] ⚠ 交叉编译的 exe 没有图标/版本资源，也没有安装包；正式发版请用 Windows + scripts/build-win.ps1"
