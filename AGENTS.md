# AGENTS.md

面向 AI 编码助手的项目指南（API-DOC 桌面客户端）。改动前先读本文件；细则见 `frontend/e2e/README.md`、`.doc/客户端UI与功能自动化测试设计.md`。

## 项目概览

- Wails v2 桌面客户端：Go 内核（`internal/*`）+ Vue3/TS 前端（`frontend/src`），**集合是纯文本文件（YAML），文件即真相源**。
- 分层单向：`handler(HTTP 层) → service → repository → model`；客户端没有 HTTP 层，改为 **App 门面**（`internal/app/app.go`，约 32 个导出方法），前端经 IPC 调用（Wails 绑定 / devserver 反射桥 `/api/App/<Method>`）。
- 界面中文优先（`frontend/src/i18n/locales/{zh-CN,en-US}.ts`，默认 zh-CN）；暗色主题令牌集中在 `frontend/src/styles/base.css`（`:root` 浅色 / `:root[data-theme='dark']` 暗色），**组件不写死颜色**。

## 常用命令

```bash
# 门禁（与 CI 同源，见 scripts/check.sh）
bash scripts/check.sh            # 日常默认（= --fast）：gofmt + build + vet + go test + vue-tsc + i18n，几秒
bash scripts/check.sh --fast     # 同上（显式写法）
bash scripts/check.sh --smoke    # 需要时：再叠加 e2e 冒烟 4 条（≈1min）
bash scripts/check.sh --full     # 需要时：全量 24 条 e2e 回归（≈3min）
bash scripts/check.sh --e2e-ui   # 需要时：只跑 e2e（调试用例）

cd frontend && npm run test:e2e            # 有头 + slowMo，便于旁观
cd frontend && npm run test:e2e:headless   # 无头（CI / 快速）
```

## 门禁时机（什么时候跑哪一层）

**默认只跑 `--fast`。** 其余耗时的检查（e2e、全量回归、真窗口核对）**只在用户明确要求时才执行**，不要自行启动浏览器或长任务。

| 时机 | 跑什么 | 期望耗时 |
|---|---|---|
| **日常（默认，每完成一小步 / 改完即跑）** | `bash scripts/check.sh`（等价于 `--fast`：gofmt + build + vet + `go test` + vue-tsc + i18n） | 几秒 |
| 用户明确要求「跑回归 / 跑测试 / check 一下」 | `bash scripts/check.sh --smoke`（+ e2e @smoke 4 条）或 `--full`（24 条） | ≈1min / ≈3min |
| 用户要求调试某个用例 / 想看操作过程 | `cd frontend && npm run test:e2e -- --grep "关键字"`（有头） | 10–30s |
| PR（CI，仓库接入 remote 后生效） | `--smoke` | ≈2min |
| 合入 main/master（CI） | `--full` + 失败上传 trace/截图/集合快照 | ≈4min |
| 发版前（人工，L3） | 真窗口清单：无边框拖动、透明圆角观感、窗口控制、缩放/字体渲染；按需 `--full` 与 `@demo`（需外部接口，地址用 `E2E_DEMO_URL` 传入） | 10min |

原则：**越贵的层越少主动跑**。当前仓库尚无 git remote，CI 未生效，本地 `--fast` 是日常唯一要跑的；CI 侧仍按上表自动跑 @smoke/全量（CI 的耗时由 CI 承担，与本机无关）。

## 必须遵守（改动相关）

1. **改 UI 交互就补测试钩子与用例**：可交互元素加 `data-testid`（命名 `模块.子域.元素`，如 `resp.tab`、`tree.row.plus`）；新增功能点在 `frontend/e2e/cases/` 补一条用例，标题前缀功能点编号（如 `[E23]`），并在 `doc/客户端功能规划与完成情况.md` §3 登记。
2. **改完只跑 `bash scripts/check.sh`（= `--fast`，秒级）**；e2e / 全量回归 / 真窗口核对等耗时检查**等用户明确要求再跑**，不要主动执行。
3. **用例只从语言包取文案**（`e2e/helpers/i18n.ts` 的 `t()`/`tEn()`），不硬编码中文/英文；定位只用 `data-testid` / `role` / `title` / 语言包文案，**禁用 scoped 类名与 nth-child**（`e2e/helpers/ui.ts` 已有常用动作）。
4. **用例之间必须隔离**：集合走 `app.newCollection(seed)`（临时目录 + 独立 `XDG_CONFIG_HOME`），设置由 `openCollection()` 自动复位；禁止读写用户真实 `~/.config/api-doc-client`。
5. **失败先分类再改**（`e2e/ai/triage.md`）：产品回归 → 修产品；用例脆弱 → 修用例；环境抖动 → 记录下来 + 重试。**不要**用 `waitForTimeout` 掩盖竞态、不要放宽断言换取绿灯。
6. **测试数据只放本地**：产物写 `.tmp/`（已 gitignore），不要把临时集合/截图提交进仓库。

## 已知坑（踩过）

| 现象 | 原因 / 做法 |
|---|---|
| 侧栏行内操作点不到 | `.row .actions` 是 `display:none` + `:hover`，集合重载会替换行元素导致 hover 丢失 → 用 `hoverAndClick(row, testid)`；行内编辑态下名称不在文本里 → 用 `treeRowByUid()` |
| `data-testid` 上 `.fill()` 报「不是 input」 | naive-ui 的 `NInput/NSelect` 把属性透传到**根 div**，要再 `.locator('input'/'textarea')` |
| toast 断言 strict 冲突 | 多条 toast 同时可见 → `page.locator('.n-message').last()` |
| md-editor 预览 DOM 找不到 | 要点工具栏「仅预览」（`[title="仅预览"]`），`.md-editor-preview` 才出现 |
| e2e 产物目录删除被安全策略拦 | Playwright 会清空 `outputDir`；已改为每次运行独立目录（`playwright.config.ts`） |
| 布局尺寸断言受缩放影响 | `uiScale` 是持久化的，用例开头依赖 `openCollection()` 的 `resetSettings()` |

## 文档约定

| 目录 | 放什么 |
|---|---|
| `.doc/` | 方案 / 设计文档（如 `客户端UI与功能自动化测试设计.md`） |
| `doc/` | 功能进展（`客户端功能规划与完成情况.md`）、问题记录（`客户端问题记录.md`） |
| `frontend/e2e/README.md` | e2e 操作手册（人读）；`frontend/e2e/ai/` 放 AI 规程与 prompt |
| `.codebuddy/skills/client-e2e-testing/` | 项目内 skill（AI 按需加载的测试规程） |
