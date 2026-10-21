#!/usr/bin/env bash
# 将 api-doc-client 的 .desktop 安装到当前用户的 Deepin/DDE 应用目录，
# 使任务栏按 StartupWMClass 正确匹配窗口图标、分组与固定。
#
# 用法：
#   build/linux/install.sh            # 安装 prod + dev 两个入口
#   build/linux/install.sh --prod     # 仅 prod
#   build/linux/install.sh --dev      # 仅 dev
#
# 说明：桌面文件里的 Exec/Icon 使用本项目的绝对路径（运行时项目必然存在），
#      因此即便移动项目目录，只要重新跑一次本脚本即可刷新路径。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APPS_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"

mkdir -p "$APPS_DIR"

install_entry() {
  local name="$1" exec_path="$2" icon_path="$3" wm_class="$4"
  local desktop="$APPS_DIR/$name.desktop"
  cat > "$desktop" <<EOF
[Desktop Entry]
Type=Application
Name=${name#api-doc-client}
GenericName=API Client
Comment=AI-native、local-first 的 API 客户端
Exec=$exec_path
Icon=$icon_path
Terminal=false
StartupWMClass=$wm_class
Categories=Development;Utility;
StartupNotify=true
EOF
  echo "已写入 $desktop"
}

DO_PROD=1
DO_DEV=1
case "${1:-}" in
  --prod) DO_DEV=0 ;;
  --dev)  DO_PROD=0 ;;
esac

if [ "$DO_PROD" = "1" ]; then
  install_entry "api-doc-client" \
    "$ROOT/build/bin/api-doc-client" \
    "$ROOT/build/appicon.png" \
    "ApiDocClient"
fi

if [ "$DO_DEV" = "1" ]; then
  install_entry "api-doc-client-dev" \
    "$ROOT/build/bin/api-doc-client-dev-linux-amd64" \
    "$ROOT/build/windows/dev_api.png" \
    "ApiDocClientDev"
fi

# 刷新桌面数据库/图标缓存（尽力而为，失败不影响核心功能）
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APPS_DIR" >/dev/null 2>&1 || true
fi

echo "完成。重新从启动器/文件管理器打开应用后，任务栏图标将按 StartupWMClass 正确显示。"
