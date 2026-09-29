#!/usr/bin/env bash
# 客户端工程门禁（本地与 CI 共用）
#
#   bash scripts/check.sh            # 日常默认 = --fast：gofmt + build + vet + go test + vue-tsc + i18n（秒级）
#   bash scripts/check.sh --fast     # 同上（显式写法）
#   bash scripts/check.sh --smoke    # + L1 单测 + e2e @smoke（≈1min，需要时才跑）
#   bash scripts/check.sh --full     # + 全部 24 条 e2e 回归（≈3min，需要时才跑）
#   bash scripts/check.sh --e2e-ui   # 只跑 e2e（调单个用例时用）
#
# 约定：日常只跑默认（--fast）；e2e 相关模式属于「耗时检查」，仅当用户/CI 明确要求时执行。
# L2 走 frontend/e2e（Playwright + devserver + testfixtures），无头运行，不需要桌面环境。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${1:-fast}"

step() { echo; echo "== $* =="; }

run_l0() {
  step "L0-1/6 gofmt（列出未格式化文件，有输出即失败）"
  local unformatted
  unformatted="$(cd "$ROOT" && gofmt -l cmd internal main.go)"
  if [[ -n "$unformatted" ]]; then
    echo "$unformatted"
    echo "-> 请运行：gofmt -w cmd internal main.go"
    exit 1
  fi

  step "L0-2/6 go build"
  (cd "$ROOT" && go build ./...)

  step "L0-3/6 go vet"
  (cd "$ROOT" && go vet ./...)

  step "L0-4/6 前端类型检查"
  (cd "$ROOT/frontend" && npm run --silent type-check)

  step "L0-5/6 i18n key 一致性"
  (cd "$ROOT/frontend" && npm run --silent i18n:check)
}

run_build() {
  step "L0-6/6 前端构建（L2 用的就是这份 dist）"
  (cd "$ROOT/frontend" && npm run --silent build)
}

run_l1() {
  step "L1 go test ./...（含集合文件层/执行器/配置/历史/Cookie 单测）"
  (cd "$ROOT" && go test ./...)
}

run_l2() {
  local grep_args=("$@")
  step "L2 e2e（Playwright 无头；${grep_args[*]:-全部}）"
  (cd "$ROOT/frontend" && npm run --silent test:e2e:headless -- "${grep_args[@]}")
}

case "$MODE" in
  --fast|fast)
    run_l0
    run_l1
    ;;
  --smoke|smoke)
    run_l0
    run_build
    run_l1
    run_l2 --grep @smoke
    ;;
  --e2e-ui)
    run_l0
    run_build
    run_l2
    ;;
  --full)
    run_l0
    run_build
    run_l1
    run_l2 --grep-invert @demo
    ;;
  *)
    echo "未知参数：$MODE（可用：--fast（默认）| --smoke | --full | --e2e-ui）" >&2
    exit 2
    ;;
esac

echo
echo "✅ 门禁通过（模式：${MODE}）"
