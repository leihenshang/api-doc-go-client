#!/usr/bin/env bash
# 启动 wails dev；Ctrl+C / 退出时结束整个进程组（macOS / Linux）。
# 理由同 scripts/dev.ps1：CLI 自带的清理在部分平台上不可靠，会留下应用与 vite 进程。
# 用法：./scripts/dev.sh [-noreload ...]（参数透传给 wails dev）
set -uo pipefail
cd "$(dirname "$0")/.."

cleanup() {
  echo "[dev] 结束 wails dev 进程组…"
  # 负 pid = 整个进程组（wails + 它拉起的 app/vite 都在组内）
  kill -TERM -$$ 2>/dev/null || true
  sleep 0.5
  kill -KILL -$$ 2>/dev/null || true
}
trap cleanup INT TERM EXIT

echo "[dev] 启动 wails dev $*"
wails dev "$@" &
CLI=$!
wait "$CLI"
echo "[dev] wails dev 已退出"
