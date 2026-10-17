# AGENTS.md

面向 AI 编码助手的项目指南（API-DOC 桌面客户端）。改动前先读本文件。

## 项目概览

- Wails v2 桌面客户端：Go 内核（`internal/*`）+ Vue3/TS 前端（`frontend/src`），**集合是纯文本文件（YAML），文件即真相源**。
- 分层单向：`handler(HTTP 层) → service → repository → model`；客户端没有 HTTP 层，改为 **App 门面**（`internal/app` 包，跨 `app/grpc/multicoll/mcp/cross/reveal` 等文件，约 115 个导出方法；清单以 `frontend/src/lib/ipc.ts` 为准），前端经 IPC 调用（Wails 绑定 / devserver 反射桥 `/api/App/<Method>`）。
- 界面中文优先（`frontend/src/i18n/locales/{zh-CN,en-US}.ts`，默认 zh-CN）；暗色主题令牌集中在 `frontend/src/styles/base.css`（`:root` 浅色 / `:root[data-theme='dark']` 暗色），**组件不写死颜色**。

## 常用命令

```bash
# 门禁：gofmt + build + vet + go test + vue-tsc + i18n，秒级（CI 用这个）
bash scripts/check.sh

# 加上 e2e（Playwright + devserver + cmd/testfixtures，跑真实构建产物）：
bash scripts/check.sh --smoke    # 前端构建 + @smoke 冒烟（约 1 分钟）
bash scripts/check.sh --full     # 前端构建 + 全量 e2e（约 3 分钟；--skip-build 可跳过构建）
bash scripts/check.sh --e2e-ui   # Playwright UI 交互调试
# 前置：cd frontend && npm i && npx playwright install chromium；用例与夹具说明见 frontend/e2e/README.md
```

## 必须遵守（改动相关）

1. **改 UI 交互就补测试钩子**：可交互元素加 `data-testid`（命名 `模块.子域.元素`，如 `resp.tab`、`tree.row.plus`），便于人工核对与后续自动化。
2. **改完即跑 `bash scripts/check.sh`（秒级）**；真窗口核对（无边框拖动、透明圆角观感、窗口控制、缩放/字体渲染）只在用户明确要求时做，不要自行启动长任务。
3. **临时数据只放本地**：产物写 `.tmp/`（已 gitignore），不要把临时集合/截图提交进仓库。
4. **三仓联动与版本一致性（P2-2 / P2-28）**：共享库改动后用 `bash ../api-doc-go-share/scripts/gowork.sh` 在父目录生成 `go.work`（三个模块走本地代码，改完立即可见）；`go.work` 不进任何仓库，`--off` 取消。**workspace 模式与 `GOFLAGS=-mod=mod` 互斥**，不要再设该开关。发布前：share 打 tag → `GOWORK=off go get github.com/leihenshang/api-doc-go-share@vX` → `bash scripts/share-version.sh` 通过（已接入 `scripts/check.sh`）。

## 文档约定

| 目录 | 放什么 |
|---|---|
| `.doc/` | 方案 / 设计文档 |
| `doc/` | 功能进展（`客户端功能规划与完成情况.md`）、问题记录（`客户端问题记录.md`） |
