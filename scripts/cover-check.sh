#!/usr/bin/env bash
# 测试门禁（P1-6）：全量单测 + 覆盖率下限 + 关键并发包的 -race。
#
#   bash scripts/cover-check.sh           # 全量（覆盖率用普通模式跑，另对关键包补 -race）
#   bash scripts/cover-check.sh --no-race # 跳过 -race（更快，仅本地临时用）
#
# 为什么不全仓 -race：客户端 app/mcp 两个包单跑就要 80 秒，带竞态检测会到十几分钟，
# 门禁太慢就没人跑。改按「并发风险集中在哪」挑包（watcher / 索引 / 同步引擎 / 集合层 /
# MCP 审计），这些正是历史出过数据竞争的地方。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RACE=1
if [[ "${1:-}" == "--no-race" ]]; then
    RACE=0
fi

LOG="$(mktemp)"
trap 'rm -f "$LOG"' EXIT

echo "== go test -cover ./... =="
(cd "$ROOT" && go test -cover ./... 2>&1) | tee "$LOG"
TEST_STATUS=${PIPESTATUS[0]}

if [[ "$TEST_STATUS" != "0" ]]; then
    echo
    echo "❌ 单测失败（exit=$TEST_STATUS），覆盖率门禁不再评估"
    exit "$TEST_STATUS"
fi

# -race 覆盖的包：历史上真正出过并发问题的地方（P1-10/P1-14/P1-15 与 watcher/syncengine）。
if [[ "$RACE" == "1" ]]; then
    RACE_PKGS="./internal/app/ ./internal/syncengine/ ./internal/collection/ ./internal/watch/ ./internal/index/ ./internal/mcpauth/ ./internal/mocksrv/"
    echo
    echo "== go test -race $RACE_PKGS =="
    # shellcheck disable=SC2086
    if ! (cd "$ROOT" && go test -race $RACE_PKGS); then
        echo "❌ 竞态检测失败"
        exit 1
    fi
fi

# 包名（去掉 module 前缀） → 覆盖率下限（百分数整数）
THRESHOLDS=$(cat <<'EOF'
internal/app 45
internal/collection 70
internal/config 60
internal/cookiejar 70
internal/history 70
internal/index 60
internal/keychain 45
internal/mcp 65
internal/mcpaudit 70
internal/mcpauth 75
internal/mocksrv 60
internal/platform 75
internal/proto 75
internal/runner 75
internal/script 75
internal/secrets 75
internal/syncengine 65
internal/varx 75
internal/watch 70
cmd/testfixtures 55
cmd/grpcfixture 35
cmd/devserver 30
EOF
)

# go test 的汇总行是并发写的，多个包会挤在同一行（前面还带着上一个包的残留），
# 所以先按 "ok/FAIL" 把流切开再匹配；逐行解析会漏包。
PARSED="$(awk '{gsub(/(ok|FAIL)[ \t]+/, "\nOK "); print}' "$LOG" \
    | sed -n 's/^OK \([^ \t]*\).*coverage: \([0-9.][0-9.]*\)% of statements.*/\1 \2/p' \
    | sed 's|^api-doc-go-client/||')"

fail=0
echo
echo "== 覆盖率门禁 =="
while read -r pkg min; do
    [[ -z "${pkg:-}" ]] && continue
    got="$(printf '%s\n' "$PARSED" | awk -v p="$pkg" '$1 == p {print $2; found=1} END {if (!found) print "missing"}')"
    if [[ "$got" == "missing" ]]; then
        # 当前平台没有测试文件（如 internal/platform 的测试全部带 !windows 标签）：
        # 不属于“包改名/静默失控”，警告但不染红；有测试却没 coverage 才判失败。
        ntests="$(go list -f '{{len .TestGoFiles}}{{len .XTestGoFiles}}' "./$pkg" 2>/dev/null || echo x)"
        if [[ "$ntests" == "00" ]]; then
            echo "  ⚠️  ${pkg}：当前平台无测试文件，跳过"
            continue
        fi
        echo "  ⚠️  ${pkg}: 本次未产出覆盖率（包名改了？）"
        fail=1
        continue
    fi
    if awk -v g="$got" -v m="$min" 'BEGIN { exit (g + 0 < m + 0) ? 1 : 0 }'; then
        printf '  ✅ %-28s %s%% (下限 %s%%)\n' "$pkg" "$got" "$min"
    else
        printf '  ❌ %-28s %s%% < 下限 %s%%\n' "$pkg" "$got" "$min"
        fail=1
    fi
done <<< "$THRESHOLDS"

if [[ "$fail" != "0" ]]; then
    echo
    echo "覆盖率低于下限。要么补测试，要么在本脚本 THRESHOLDS 里给出下调理由（并写进提交信息）。"
    exit 1
fi
echo
echo "覆盖率门禁通过 ✓"
