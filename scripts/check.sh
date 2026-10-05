#!/usr/bin/env bash
# 客户端工程门禁（本地与 CI 共用）
#
#   bash scripts/check.sh                # 静态检查 + 单测（秒级，默认 / CI 用这个）
#   bash scripts/check.sh --smoke        # 加上前端构建 + e2e 冒烟（@smoke，约 30 秒）
#   bash scripts/check.sh --full         # 加上前端构建 + 全量 e2e（排除 @demo，约 3 分钟）
#   bash scripts/check.sh --e2e-ui       # 打开 Playwright UI（交互式，不跑静态检查）
#   bash scripts/check.sh --skip-build   # 与 e2e 模式组合：跳过前端构建（dist 已是最新时用）
#
# e2e 前置：frontend 下装过依赖与浏览器（npm i && npx playwright install chromium）；
# 用例细节见 frontend/e2e/README.md。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

MODE="fast"
SKIP_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --fast) MODE="fast" ;;
    --smoke) MODE="smoke" ;;
    --full) MODE="full" ;;
    --e2e-ui) MODE="e2e-ui" ;;
    --skip-build) SKIP_BUILD=1 ;;
    -h | --help)
      sed -n '2,12p' "$0"
      exit 0
      ;;
    *)
      echo "未知参数：$arg（可用：--fast / --smoke / --full / --e2e-ui / --skip-build）" >&2
      exit 2
      ;;
  esac
done

step() { echo; echo "== $* =="; }

# ---- 静态检查与单测（所有模式都跑，--e2e-ui 除外） ----

if [[ "$MODE" != "e2e-ui" ]]; then
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

  step "L1 go test ./...（含集合文件层/执行器/e2e 夹具/配置/历史/Cookie 单测）"
  (cd "$ROOT" && go test ./...)
fi

# ---- e2e（可选模式） ----

build_frontend() {
  if [[ "$SKIP_BUILD" == "1" ]]; then
    echo "（--skip-build：沿用现有 frontend/dist）"
    return
  fi
  step "L2-0 前端构建（e2e 跑的是真实构建产物）"
  (cd "$ROOT/frontend" && npm run --silent build)
}

run_e2e() {
  step "L2 e2e（$*）"
  (cd "$ROOT/frontend" && npx playwright test "$@")
}

case "$MODE" in
  e2e-ui)
    step "Playwright UI（退出后手动 Ctrl+C）"
    (cd "$ROOT/frontend" && npx playwright test --ui)
    ;;
  smoke)
    build_frontend
    if ! run_e2e --grep @smoke; then
      echo "-> e2e 失败。缺浏览器时先跑：cd frontend && npx playwright install chromium" >&2
      exit 1
    fi
    ;;
  full)
    build_frontend
    if ! run_e2e --grep-invert @demo; then
      echo "-> e2e 失败。缺浏览器时先跑：cd frontend && npx playwright install chromium" >&2
      exit 1
    fi
    ;;
esac

echo
echo "✅ 门禁通过（模式：$MODE）"
