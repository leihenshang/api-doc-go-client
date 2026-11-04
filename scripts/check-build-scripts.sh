#!/usr/bin/env bash
# 出包脚本自检：这里列的每一条都是踩过的坑，回归一次要重装/重打一轮。
# 只做静态断言（读脚本内容），不依赖 Windows 环境，所以能进日常门禁。
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
bad() { echo "  ✗ $*" >&2; fail=1; }

# 1) 安装包必须由**仓库里的 installer.nsi** + makensis 产出。
#    退回 `wails build -nsis` 会换成 wails 每次重写的默认模板，
#    丢掉自定义加固：升级前关进程、先静默卸载旧版、记住安装目录、WebView2 未就绪提示。
grep -q 'installer\.nsi' scripts/build-win.ps1 || bad "build-win.ps1 未使用 build/windows/installer/installer.nsi"
grep -q 'Makensis' scripts/build-win.ps1 || bad "build-win.ps1 未调用 makensis（安装包会退回默认模板）"

# 2) 发布构建必须忽略 go.work：本地 share 副本落后时，只有非 workspace 构建才会暴露
grep -q "GOWORK = 'off'" scripts/build-win.ps1 || bad "build-win.ps1 未设置 GOWORK=off"
grep -q 'GOWORK=off' scripts/build-win.sh || bad "build-win.sh 未设置 GOWORK=off"

# 3) 产物名必须显式带 .exe —— wails 只在「未传 -o」时才自动补后缀
grep -q "'-o', 'api-doc-client.exe'" scripts/build-win.ps1 || bad "build-win.ps1 正式版未显式 -o api-doc-client.exe"

# 4) -clean 只允许出现在第一条构建：它清空 build\bin，后一条带的话会把前一条产物删掉
n_clean=$(grep -v '^[[:space:]]*#' scripts/build-win.ps1 | grep -c -- '-clean' || true)
if [ "${n_clean:-0}" -gt 1 ]; then
  bad "build-win.ps1 有 $n_clean 处 -clean（只允许第一条构建带，否则后一条清掉前一条产物）"
fi

# 5) 版本一致性脚本的口径：前端版本看 lib/version.ts
grep -q 'lib/version.ts' scripts/check-versions.sh || bad "check-versions.sh 未读取 frontend/src/lib/version.ts"

if [ "$fail" != 0 ]; then
  echo "❌ 出包脚本自检失败" >&2
  exit 1
fi
echo "出包脚本自检通过（installer.nsi + makensis，GOWORK=off，$n_clean 处 -clean）"
