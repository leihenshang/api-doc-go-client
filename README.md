# API-DOC 客户端

基于 **Wails v2** 的桌面客户端：Go 内核 + Vue3/TS 前端。集合以纯文本 YAML 文件存储（文件即真相源），支持接口调试、环境变量、Mock、代码生成、导入 / 导出（OpenAPI / Postman / Bruno）与双向同步。

## 技术栈

- Wails v2（Go 内核 + webview）
- 前端：Vue 3 + TypeScript + Vite + Pinia + Vue Router + Naive UI + axios
- 共享规则：[api-doc-go-share](https://github.com/leihenshang/api-doc-go-share)（`require v0.3.0`）

## 目录结构

```
cmd/        App 门面（internal/app 的 Wails 绑定入口）
internal/   Go 内核（app 门面、collection、varx、mocksrv、repository/model）
frontend/   Vue3 前端（src/{api,router,stores,views,components,i18n}）
scripts/    check.sh 工程门禁
wails.json  Wails 构建配置（outputfilename: api-doc-client）
doc/        功能规划与问题记录
```

## 快速开始

### 依赖

- Go 1.26+
- Node 18+
- Wails v2 CLI：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### 开发（桌面窗口 + 前端热更新）

```bash
./scripts/dev.ps1          # Windows：推荐（一次 Ctrl+C 结束全部，见下）
./scripts/dev.sh           # macOS / Linux
wails dev                  # 或直接用 wails dev（Ctrl+C 可能残留进程，见下）
```

> **为什么推荐用脚本**：`wails dev` 在 Windows 上的进程树清理不可靠 —— 实测把 CLI 连同整棵树
> (`taskkill /F /T`) 杀掉之后，它拉起的应用进程（`api-doc-client-dev.exe`，仍占 34115）与前端
> vite（node，仍占 vite 端口）都还活着，终端提示符也不回来，看起来就是「Ctrl+C 停不掉开发服务」。
> `scripts/dev.ps1` 在退出时按 **CLI → 进程名 → 端口** 三层兜底清扫，并复查端口后打印结果。
> 手动兜底：`taskkill /F /IM api-doc-client-dev.exe`、`taskkill /F /IM wails.exe`。
>
> 应用侧也做了兜底：收到 Ctrl+C / SIGTERM 后会等 1.5s，若这段时间没人来收尾就自己关闭窗口
> （`internal/app` 的 `watchSignals`），并通过 `OnShutdown` 释放内嵌 MCP 服务的端口。

> **启动日志里的 `Wails is now using the new Go WebView2Loader…` 是信息提示，不是错误**：
> Wails v2.16 起用纯 Go 实现替换了原生的 `WebView2Loader.dll`（不再需要 CGO 与随包 DLL），
> 只影响 WebView2 的**初始化**，与页面内的交互（输入、拖拽、渲染）无关。
> 只有遇到「启动即崩 / 白屏 / WebView2 创建失败」这类初始化问题时才需要处理：
> 临时回退旧 loader 用 `./scripts/dev.ps1 -tags native_webview2loader`（或 `wails build -tags …`），
> 想长期固定就写进 `wails.json` 的 `"build:tags": "native_webview2loader"`（该 loader 上游已计划废弃）。
> 新 loader 的问题请报到 https://github.com/wailsapp/wails/issues/2004。

### 仅前端（浏览器，无桌面窗口）

```bash
cd frontend && npm install && npm run dev
```

### 构建桌面应用

```bash
wails build          # 产出 bin/api-doc-client
```

## 门禁（本地与 CI 共用）

```bash
bash scripts/check.sh   # gofmt + build + vet + go test + vue-tsc + i18n（秒级）
```

## 配置

- 界面中文优先，暗色主题令牌集中在 `frontend/src/styles/base.css`，组件不写死颜色。
- 集合为本地 YAML 目录（文件即真相源）；设置落盘于用户配置目录，各集合相互独立。

## 共享模块

变量解析、集合格式、代码生成等两端共用的规则见 [api-doc-go-share](https://github.com/leihenshang/api-doc-go-share)。

## 许可证

MIT（与 api-doc-go / api-doc-go-share 一致）。
