#!/usr/bin/env bash
# 一键发版：版本号只在一个入口改，改完全部对齐 → 提交 → 打 tag → 推送交给 CI 出包。
#
# 为什么要有这个脚本：版本号原先散落三处（wails.json / frontend/package.json /
# TitleBar.vue），手工改必然漂移（0.1.8 时标题栏还停在 v0.0.1），而「检查更新」
# 正是靠这个版本号判断要不要升级 —— 漂移 = 更新永远不触发。
#
# 用法：scripts/release.sh 0.2.0            # 对齐 → 提交 → tag → 推送
#       scripts/release.sh 0.2.0 --dry-run # 只展示会改哪些文件
set -euo pipefail
cd "$(dirname "$0")/.."

VER="${1:-}"
[ -n "$VER" ] || { echo "用法：scripts/release.sh <版本号，如 0.2.0>" >&2; exit 2; }
echo "$VER" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo "版本号须形如 0.2.0" >&2; exit 2; }
[ -z "$(git status --porcelain)" ] || { echo "工作区不干净，先提交或 stash" >&2; exit 1; }
! git rev-parse -q --verify "refs/tags/v$VER" >/dev/null || { echo "tag v$VER 已存在" >&2; exit 1; }

# 三处对齐：wails.json → deb 包版本；package.json → npm 生态；TitleBar → 界面显示
sed -i -E "s/(\"productVersion\"[[:space:]]*:[[:space:]]*\")[^\"]*/\1$VER/" wails.json
sed -i -E "0,/^[[:space:]]*\"version\"[[:space:]]*:/s/(\"version\"[[:space:]]*:[[:space:]]*\")[^\"]*/\1$VER/" frontend/package.json
sed -i -E "s/(APP_VERSION = ')v?[^']*/\1v$VER/" frontend/src/lib/version.ts

if [ "${2:-}" = "--dry-run" ]; then
  git --no-pager diff --stat
  git checkout -- wails.json frontend/package.json frontend/src/lib/version.ts
  exit 0
fi

bash scripts/check-versions.sh
git add wails.json frontend/package.json frontend/src/lib/version.ts
git commit -q -m "chore(release): v$VER"
git tag -a "v$VER" -m "v$VER"
git push origin master
git push origin "v$VER"
echo "✅ 已推送 v$VER：CI 自动构建 deb 并发布 Release（.github/workflows/release.yml）"
echo "   客户端「设置 → 检查更新」会读到这个版本。"
