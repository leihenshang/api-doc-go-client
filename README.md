# API-DOC 客户端

基于 **Wails v2** 的桌面客户端：Go 内核 + Vue3/TS 前端。集合以纯文本 YAML 文件存储（文件即真相源），支持接口调试、环境变量、Mock、代码生成、导入 / 导出（OpenAPI / Postman / Bruno）与双向同步。

## 技术栈

- Wails v2（Go 内核 + webview）
- 前端：Vue 3 + TypeScript + Vite + Pinia + Vue Router + Naive UI + axios
- 端到端测试：Playwright（无头运行，无需桌面环境）
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
wails dev
```

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
bash scripts/check.sh            # 默认 --fast：gofmt + build + vet + go test + vue-tsc + i18n（秒级）
bash scripts/check.sh --smoke    # + e2e @smoke（约 1min，按需）
bash scripts/check.sh --full     # 全量 24 条 e2e 回归（约 3min，按需）
cd frontend && npm run test:e2e  # 有头 / headless 调试用例
```

约定：日常只跑 `--fast`；e2e 等耗时检查仅在明确要求时执行。

## 配置

- 界面中文优先，暗色主题令牌集中在 `frontend/src/styles/base.css`，组件不写死颜色。
- 集合为本地 YAML 目录（文件即真相源）；设置落盘于用户配置目录，各集合相互独立。

## 共享模块

变量解析、集合格式、代码生成等两端共用的规则见 [api-doc-go-share](https://github.com/leihenshang/api-doc-go-share)。

## 许可证

MIT（与 api-doc-go / api-doc-go-share 一致）。
