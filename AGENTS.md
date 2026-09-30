# AGENTS.md

面向 AI 编码助手的项目指南（API-DOC 桌面客户端）。改动前先读本文件。

## 项目概览

- Wails v2 桌面客户端：Go 内核（`internal/*`）+ Vue3/TS 前端（`frontend/src`），**集合是纯文本文件（YAML），文件即真相源**。
- 分层单向：`handler(HTTP 层) → service → repository → model`；客户端没有 HTTP 层，改为 **App 门面**（`internal/app/app.go`，约 32 个导出方法），前端经 IPC 调用（Wails 绑定 / devserver 反射桥 `/api/App/<Method>`）。
- 界面中文优先（`frontend/src/i18n/locales/{zh-CN,en-US}.ts`，默认 zh-CN）；暗色主题令牌集中在 `frontend/src/styles/base.css`（`:root` 浅色 / `:root[data-theme='dark']` 暗色），**组件不写死颜色**。

## 常用命令

```bash
# 门禁：gofmt + build + vet + go test + vue-tsc + i18n，秒级
bash scripts/check.sh
```

## 必须遵守（改动相关）

1. **改 UI 交互就补测试钩子**：可交互元素加 `data-testid`（命名 `模块.子域.元素`，如 `resp.tab`、`tree.row.plus`），便于人工核对与后续自动化。
2. **改完即跑 `bash scripts/check.sh`（秒级）**；真窗口核对（无边框拖动、透明圆角观感、窗口控制、缩放/字体渲染）只在用户明确要求时做，不要自行启动长任务。
3. **临时数据只放本地**：产物写 `.tmp/`（已 gitignore），不要把临时集合/截图提交进仓库。

## 文档约定

| 目录 | 放什么 |
|---|---|
| `.doc/` | 方案 / 设计文档 |
| `doc/` | 功能进展（`客户端功能规划与完成情况.md`）、问题记录（`客户端问题记录.md`） |
