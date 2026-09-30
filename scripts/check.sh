#!/usr/bin/env bash
# 客户端工程门禁（本地与 CI 共用）
#
#   bash scripts/check.sh   # gofmt + build + vet + go test + vue-tsc + i18n（秒级）
#
# 固定只跑静态检查与单测；真窗口核对等耗时检查由人工按需执行。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

step() { echo; echo "== $* =="; }

step "L0-1/5 gofmt（列出未格式化文件，有输出即失败）"
unformatted="$(cd "$ROOT" && gofmt -l cmd internal main.go)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted"
  echo "-> 请运行：gofmt -w cmd internal main.go"
  exit 1
fi

step "L0-2/5 go build"
(cd "$ROOT" && go build ./...)

step "L0-3/5 go vet"
(cd "$ROOT" && go vet ./...)

step "L0-4/5 前端类型检查"
(cd "$ROOT/frontend" && npm run --silent type-check)

step "L0-5/5 i18n key 一致性"
(cd "$ROOT/frontend" && npm run --silent i18n:check)

step "L1 go test ./...（含集合文件层/执行器/配置/历史/Cookie 单测）"
(cd "$ROOT" && go test ./...)

echo
echo "✅ 门禁通过"
