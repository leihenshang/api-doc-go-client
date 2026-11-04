#!/usr/bin/env bash
# 出包脚本自检：这里列的每一条都是踩过的坑，回归一次要重装/重打一轮。
# 只做静态断言（读脚本内容），不依赖 Windows 环境，所以能进日常门禁。
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
bad() { echo "  ✗ $*" >&2; fail=1; }

# 1) build-win.ps1：最多一条 wails build 带 -clean。
#    -clean 会清空 build\bin，两条都带的话后一条会把前一条的产物删掉（正式版凭空消失）。
n_clean=$(grep -c 'wails build .*-clean' scripts/build-win.ps1 || true)
if [ "${n_clean:-0}" -gt 1 ]; then
  bad "build-win.ps1 有 $n_clean 处 -clean（只允许第一条构建带，否则后一条清掉前一条产物）"
fi

# 2) 产物名必须显式带 .exe —— Wails 在 Windows 上不会自动补后缀
grep -q -- '-o api-doc-client.exe' scripts/build-win.ps1 || bad "build-win.ps1 正式版未显式 -o api-doc-client.exe"
grep -q -- '-o api-doc-client-dev.exe' scripts/build-win.ps1 || bad "build-win.ps1 开发版未显式 -o api-doc-client-dev.exe"

# 3) 发布构建必须忽略 go.work：本地 share 副本落后时，只有「非 workspace 构建」才会暴露
grep -q "GOWORK = 'off'" scripts/build-win.ps1 || bad "build-win.ps1 未设置 GOWORK=off"
grep -q 'GOWORK=off' scripts/build-win.sh || bad "build-win.sh 未设置 GOWORK=off"

# 4) 直接执行就应出安装包（-NoNsis 才跳过）
grep -q 'NoNsis' scripts/build-win.ps1 || bad "build-win.ps1 缺少 -NoNsis（默认应生成安装包）"

# 5) 改版本写文件必须走 UTF-8 辅助函数：PowerShell 5.1 的 Set-Content 默认 ANSI，
#    会把 version.ts 里的中文注释写乱码。
grep -q 'Write-Utf8' scripts/build-win.ps1 || bad "build-win.ps1 缺少 Write-Utf8（改版本会写乱码）"
if grep -v '^[[:space:]]*#' scripts/build-win.ps1 | grep -q 'Set-Content'; then
  bad "build-win.ps1 仍在用 Set-Content（PS 5.1 下默认 ANSI）"
fi

# 6) 版本一致性脚本的口径：前端版本看 lib/version.ts
grep -q 'lib/version.ts' scripts/check-versions.sh || bad "check-versions.sh 未读取 frontend/src/lib/version.ts"

if [ "$fail" != 0 ]; then
  echo "❌ 出包脚本自检失败" >&2
  exit 1
fi
echo "出包脚本自检通过（$n_clean 处 -clean，GOWORK=off，默认出安装包）"
