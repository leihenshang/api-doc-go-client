# AGENTS.md

面向 AI 编码助手的项目指南（API-DOC 桌面客户端）。改动前先读本文件。

## 项目概览

- Wails v2 桌面客户端：Go 内核（`internal/*`）+ Vue3/TS 前端（`frontend/src`），**集合是纯文本文件（YAML），文件即真相源**。
- 分层单向：`handler(HTTP 层) → service → repository → model`；客户端没有 HTTP 层，改为 **App 门面**（`internal/app` 包，跨 `app/grpc/multicoll/mcp/cross/reveal` 等文件，136 个导出方法；清单以 `frontend/src/lib/ipc.ts` 为准），前端经 IPC 调用（Wails 绑定 / devserver 反射桥 `/api/App/<Method>`）。
- 界面中文优先（`frontend/src/i18n/locales/{zh-CN,en-US}.ts`，默认 zh-CN）；暗色主题令牌集中在 `frontend/src/styles/base.css`（`:root` 浅色 / `:root[data-theme='dark']` 暗色），**组件不写死颜色**。

## 常用命令

```bash
# 门禁：gofmt + build + vet + go test（-cover + 关键包 -race）+ vue-tsc + ESLint + i18n；
# 含 -race 约 3–4 分钟（CI 用这个；本地可加 --no-race 加快）
bash scripts/check.sh
# 只跑前端静态检查：cd frontend && npm run type-check && npm run lint && npm run i18n:check

# 加上 e2e（Playwright + devserver + cmd/testfixtures，跑真实构建产物）：
bash scripts/check.sh --smoke    # 前端构建 + @smoke 冒烟（约 1 分钟）
bash scripts/check.sh --full     # 前端构建 + 全量 e2e（约 3 分钟；--skip-build 可跳过构建）
bash scripts/check.sh --e2e-ui   # Playwright UI 交互调试
# 前置：cd frontend && npm i && npx playwright install chromium；用例与夹具说明见 frontend/e2e/README.md

# 集合运行器（RM-02）：CLI/CI 按树顺序执行脚本/断言，输出 text|json|junit
go run ./cmd/clientctl run <collection-dir> --env dev --reporter junit --output report.xml
# 桌面端同一执行层：概览「运行集合」或 Ctrl+K → Run collection；报告可导出 JUnit
# 参数：-filter/-bail/-timeout/-output；示例见 ci/README.md
# CI 示例 .github/workflows/collection-tests.yml 刻意只保留 workflow_dispatch，推送不会自动运行
```

## 必须遵守（改动相关）

1. **改 UI 交互就补测试钩子**：可交互元素加 `data-testid`（命名 `模块.子域.元素`，如 `resp.tab`、`tree.row.plus`），便于人工核对与后续自动化。
2. **改完即跑 `bash scripts/check.sh`（含 -race 约 3–4 分钟）**；真窗口核对（无边框拖动、透明圆角观感、窗口控制、缩放/字体渲染）只在用户明确要求时做，不要自行启动长任务。
3. **临时数据只放本地**：产物写 `.tmp/`（已 gitignore），不要把临时集合/截图提交进仓库。
4. **请求体字段的两套数据**：`body.fields`（字段名/类型/必填/示例值/说明）是**请求体字段表格的唯一真源** —— 生成接口文档、Web 详情、MCP `bodyFields`、同步键 `bodyFields` 都只认它；`body.descs`（path → 备注）自本改动起只作「首次生成表格」的描述种子与老数据 round-trip，**不再参与文档生成**。新增/调整 `Body` 字段时必须同步 `share/collection/request.go` 的 `HTTPBlock.IsZero()`（漏判会让 `http:` 段被 omitempty 整段丢掉）。API 概述走顶层 `summary`（落盘 `RequestFile.summary`、同步键 `summary`，gRPC 请求同样需要）。
5. **三仓联动与版本一致性（P2-2 / P2-28）**：共享库改动后用 `bash ../api-doc-go-share/scripts/gowork.sh` 在父目录生成 `go.work`（三个模块走本地代码，改完立即可见）；`go.work` 不进任何仓库，`--off` 取消。**workspace 模式与 `GOFLAGS=-mod=mod` 互斥**，不要再设该开关。发布前：share 打 tag → `GOWORK=off go get github.com/leihenshang/api-doc-go-share@vX` → `bash scripts/share-version.sh` 通过（已接入 `scripts/check.sh`）。
6. **前端大组件已拆分，改动优先放对应子件**：`RequestEditor.vue` 只保留参数/头/认证/脚本/断言与分段编排，响应字段在 `ResponsePane.vue`、Docs 在 `RequestDocsPane.vue`；`Sidebar.vue` 的三个弹窗在 `SidebarDialogs.vue`；`App.vue` 的页签关闭/草稿保存流程在 `composables/useCloseFlow.ts`。样式已外置为 `styles/sidebar.css` / `styles/app-shell.css`（`<style scoped src>` 仍保持组件作用域），不要再把大段样式塞回 SFC。
7. **Markdown 图片只落本地集合**：编辑器选图/粘贴走 `lib/md-image.ts` → `App.SaveDocImage`（PNG/JPEG/GIF/WebP、≤5MB、**文件头嗅探**）→ `<集合>/assets/<sha256>`（内容寻址去重），正文引用 `/doc-assets/<sha256>`；只读路由由桌面壳 `AssetServer.Handler` 与 devserver 共用 `App.DocAssetHandler()`（固定 Content-Type + `nosniff`，文件名只接受 64 位 hex）。MCP 对应工具 `save_doc_image`。同步由 `internal/syncengine/docassets.go` 在边界转换：推送前 `/doc-assets/<hash>` → 上传后服务端 URL（映射存 `.sync.json` 的 `assetUrls`），拉取后 `/uploads/*`、`/api/file/<hash>` → 下载到 `assets/` 并改写成本地引用；外链一律不动。导出打包与冲突「采用服务器版本」的图片本地化仍是后续项，不要在这里接 `/uploads`。
8. **PAT 只以密文落盘**：`.sync.json` 只写 `tokenEnc`（`internal/patvault`，密钥在用户配置目录 `pat_vault.key`，0600），`BindInfo.Token` 仅内存且 `json:"-"`；旧版明文 `token` 首次 `LoadBind` 自动迁移。不要改回明文，也不要把令牌回传前端（`GetSyncBind` 已不返回）。

## 文档约定

| 目录 | 放什么 |
|---|---|
| `.doc/` | 方案 / 设计文档 |
| `doc/` | 功能进展（`客户端功能规划与完成情况.md`）、问题记录（`客户端问题记录.md`） |
| `CHANGELOG.md`（根目录） | 对外更新日志：当前 dev / prod 版本 + 按日期记录的变更 |

## 更新日志（根目录 `CHANGELOG.md`）

根目录 `CHANGELOG.md` 是唯一的对外更新日志。**每次修复 Bug、或新增/调整用户可感知的功能，都要在文件顶部补一条（1–3 句）并更新版本号**，不能只写提交信息。

- `prod:` = 最近一次发布版，必须与 `wails.json` 的 `info.productVersion` 一致（当前 `0.1.0`）；只在发布时抬升。
- `dev:` = 当前开发线版本（`X.Y.Z-dev`，当前 `0.1.3-dev`），**每新增一条日期条目 patch +1**；发布时把它去掉 `-dev` 写入 `wails.json` 作为新的 `prod`，随后 `dev` 继续 +1。
- 条目格式：`## YYYY-MM-DD  版本` + 变更列表；**同一天的多条改动合并进当天条目**（版本只加一次），最新条目在最上方；跨阶段的历史可合并为带日期范围的条目。
- 只写用户能感知的变化（功能 / 行为 / 修复 / 兼容性），不写内部重构、测试细节与文件清单；细节留在提交信息与 `doc/`。
- 顺序：改完代码 → `bash scripts/check.sh` 通过 → 更新 `CHANGELOG.md` → 一起提交；提交信息与 CHANGELOG 措辞保持一致。
