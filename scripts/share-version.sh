#!/usr/bin/env bash
# 三仓版本一致性门禁（P2-2）：客户端视角。两个下游仓库必须 require 同一个 share 版本，
# 且该版本必须是 share 仓库里的最新 tag。
#
#   bash scripts/share-version.sh
#
# 为什么要卡：两个消费方各自手改 go.mod 时极易漂移（一个升了、一个没升），
# 表现是「同一段代码在 Web 与桌面端行为不同」，排查成本极高。
# 只在三个仓库并排存在时生效；单独 clone 的 CI 环境会自动跳过。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_GOMOD="$ROOT/go.mod"
CLIENT_GOMOD="$ROOT/../api-doc-go/server/go.mod"
SHARE_DIR="$ROOT/../api-doc-go-share"

req_of() {
    # 取 require 行里的版本（形如 v0.10.3）
    grep -E '^\s*github\.com/leihenshang/api-doc-go-share v' "$1" | head -1 | awk '{print $2}'
}

mine="$(req_of "$SERVER_GOMOD")"
if [[ -z "$mine" ]]; then
    echo "❌ $ROOT/go.mod 里找不到 api-doc-go-share 的 require"
    exit 1
fi
echo "客户端 require: $mine"

fail=0

# 1) 与 share 仓库最新 tag 对比
if [[ -d "$SHARE_DIR/.git" ]]; then
    tag="$(git -C "$SHARE_DIR" describe --tags --abbrev=0 2>/dev/null || true)"
    if [[ -n "$tag" ]]; then
        if [[ "$mine" == "$tag" ]]; then
            echo "✅ 与 share 最新 tag 一致：$tag"
        else
            echo "❌ 客户端 require $mine ≠ share 最新 tag $tag"
            echo "   -> 若 share 刚改过：先打 tag，再在两个仓库 go get。"
            echo "   -> 若尚在开发中（go.work 已生效）：发布前必须补齐这一步。"
            fail=1
        fi
    fi
else
    echo "（跳过）未找到 $SHARE_DIR，无法比对最新 tag"
fi

# 2) 与另一个消费方对比
if [[ -f "$SERVER_GOMOD" ]]; then
    other="$(req_of "$SERVER_GOMOD")"
    if [[ -z "$other" ]]; then
        echo "❌ $SERVER_GOMOD 里找不到 api-doc-go-share 的 require"
        fail=1
    elif [[ "$other" == "$mine" ]]; then
        echo "✅ 与服务端一致：$other"
    else
        echo "❌ 服务端 $mine ≠ 客户端 $other（两端会跑不同版本的共享逻辑）"
        fail=1
    fi
else
    echo "（跳过）未找到 $SERVER_GOMOD，无法比对服务端版本"
fi

# 工作区模式提示：go.work 存在时实际用的是本地代码，版本号只影响发布
if [[ -n "$(cd "$ROOT" && go env GOWORK 2>/dev/null)" && "$(cd "$ROOT" && go env GOWORK 2>/dev/null)" != "off" ]]; then
    echo "ℹ️  当前处于 go.work 工作区模式：联调用的是本地 share 代码，版本号仅作发布前校验。"
fi

if [[ "$fail" != "0" ]]; then
    echo
    echo "版本一致性检查未通过。"
    exit 1
fi
echo "版本一致性检查通过 ✓"
