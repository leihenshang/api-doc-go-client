#!/usr/bin/env bash
# 静默吞错棘轮（P2-4）：统计「`_ = x.Save/Create/Delete/Update/Upsert/Append/Exec/WriteFile/...`」
# 且**同行没有注释**的调用点数，与基线比较。只许减少，不许增加。
#
#   bash scripts/silent-error.sh            # 检查
#   bash scripts/silent-error.sh --update    # 确认新增的每一处都属可接受后更新基线
#
# 为什么卡这个：`_ =` 本身不是错，但「写操作失败被静默忽略」是最难排查的一类问题
# （案例：删除失败被吞 → 本地与服务端不一致；冲突副本写失败被吞 → 用户改动直接丢）。
# 现有每一处都已在行上方注释「为什么可以忽略」；新增的必须显式处理或同样注明。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE="$ROOT/scripts/silent-error.baseline"

# 关注的动词：会改动持久化状态或索引的调用。
PATTERN='_ = [A-Za-z0-9_.]*(Save|Create|Update|Delete|Upsert|Append|Exec|WriteFile|Rename|Chmod|MarkSynced|Add)[A-Za-z0-9]*\('

# 只统计「本行或上一行没有 // 注释」的行：有注释说明作者已判断过可忽略。
risky_lines() {
    find "$ROOT" -name '*.go' ! -name '*_test.go' -print0 2>/dev/null \
        | xargs -0 -r awk -v pat="$PATTERN" '
            {
                if ($0 ~ pat && $0 !~ /\/\// && prev !~ /^[[:space:]]*\/\//) {
                    print FILENAME ":" FNR ":" $0
                }
                prev = $0
            }' 2>/dev/null
}

count_risky() { risky_lines | wc -l | tr -d " "; }

current="$(count_risky)"
if [[ "${1:-}" == "--update" ]]; then
    printf '%s\n' "$current" > "$BASE"
    echo "已更新基线：$current 处"
    exit 0
fi

baseline="$(cat "$BASE" 2>/dev/null || echo 0)"
echo "静默吞错（无注释）：当前 $current 处，基线 $baseline 处"

if [[ "$current" -gt "$baseline" ]]; then
    echo "❌ 新增了未加注释的静默吞错调用，未通过。"
    echo "   请二选一：① 显式处理错误；② 若确实可忽略，在上方写清「为什么可忽略」并在本脚本 --update 基线。"
    echo "   当前未加注释的调用点："
    risky_lines | sed "s|^$ROOT/||" | sed "s/^/     /"
    exit 1
fi
if [[ "$current" -lt "$baseline" ]]; then
    echo "ℹ️  比基线少了 $((baseline - current)) 处——记得 --update 收紧基线。"
fi
echo "通过 ✓"
