# 客户端 E2E 测试手册

Playwright + devserver + testfixtures 组成的端到端测试：在**真实构建产物**上驱动**真实 App 门面方法**（前端 IPC 走 `POST /api/App/<Method>` 反射桥），因此同一条用例可以同时断言 **UI / 集合文件 / 真实响应** 三侧。

设计依据（为什么这么分层、AI 放在哪一步）见 `.doc/客户端UI与功能自动化测试设计.md`；AI 操作规程见 `ai/`。

## 运行

```bash
cd frontend
npm run test:e2e                      # 有头 + slowMo=300（看得见操作）
npm run test:e2e:headless             # 无头（CI、快速）
npm run test:e2e:headless -- --grep @smoke        # 只跑冒烟
npm run test:e2e:headless -- cases/regression-exec.spec.ts   # 单个文件
E2E_SLOWMO=0 npm run test:e2e         # 全速有头
npx playwright show-trace ../.tmp/e2e-artifacts/run-*/<用例>/trace.zip   # 看失败现场
```

门禁入口（与 CI 同源）：**日常只跑 `bash scripts/check.sh`（= `--fast`，几秒）**；e2e 相关模式（`--smoke` / `--full` / `--e2e-ui`）属耗时检查，仅在明确需要时执行。

- 前置：`frontend/dist` 必须是最新构建（`npm run build`；`check.sh` 里已含）。fixture 会自动编译 `cmd/devserver` 与 `cmd/testfixtures`。
- 规模与耗时：**24 条回归 + 1 条 @demo**，全量约 2 分钟，@smoke 约 20 秒。
- `@demo` 用例的环境变量：`E2E_DEMO_URL`（必填，如 `https://内网主机/xxx`，未设置则 skip）、`E2E_DEMO_STATUS`（选填，忽略证书后期望的状态码）。运行时：`E2E_DEMO_URL=… npm run test:e2e -- --grep @demo`。

## 目录

```
frontend/e2e/
├─ cases/            用例（按功能域分文件，标题前缀功能点编号）
│   regression-exec.spec.ts        §5-1/2：证书策略、重定向、超时、状态码、形态、Cookie、认证、multipart
│   regression-collection.spec.ts  §5-3/4/5/16：round-trip、自写回环、树操作、参数表与 Bulk Edit
│   regression-response.spec.ts    §5-17：响应字段只追加、保存响应示例
│   regression-shell.spec.ts       §5-6/8/9/10/11/12/13/14/15：外壳、令牌、字体、弹窗、分栏、命令面板
│   regression-theme.spec.ts       §5-18/19/7：主题、圆角/遮罩、语言渗透、Markdown 转义
│   createDnsRegions.spec.ts       @demo：打真实外部 https 接口的新建→发送（地址由 E2E_DEMO_URL 传入，未设置即 skip；--smoke/--full 都以 --grep-invert @demo 排除）
├─ fixtures/         集合种子（basic / bruno-sample；`__FIXTURE__`、`__TLS__` 由 fixture 运行期替换）
├─ helpers/
│   app.ts           worker fixture：临时集合 + 独立 XDG_CONFIG_HOME + 随机端口 + devserver/testfixtures/TLS
│   ui.ts            常用动作：openCollection/openRequest/send/openSettings/saveSettings/rawJSON…
│   api.ts           IPC 造数据：readRequest/patchRequest（SaveRequest 是整对象覆盖）
│   dom.ts           hoverAndClick（悬停才显示的行内操作）
│   fs.ts            集合读盘断言：readCollectionFile/listCollectionFiles
│   i18n.ts          t()/tEn()：文案一律取语言包
│   settings.ts      resetSettings：用例之间互不影响
└─ ai/               AI 规程与 prompt：gen-cases.md（生成用例）、triage.md（失败归因）
```

## 夹具服务（`cmd/testfixtures`）

| 端点 | 用途 |
|---|---|
| `/echo` | 回显方法/查询/头/原始体（断言请求组装与变量替换） |
| `/status/{code}` | 任意状态码（204/304 不带体） |
| `/redirect/{n}` | n 跳重定向；`?abs=1` 绝对 Location；`?loop=1` 自环（超上限分支） |
| `/delay?ms=` | 延迟（超时分支） |
| `/json/{flat,nested,withArray,fewer,big}` | 固定 JSON 形状（`fewer` 配合「字段只追加」） |
| `/binary?kb=` | 二进制（客户端判二进制 → base64 + 提示） |
| `/gzip` | gzip 响应（Go client 自动解压） |
| `/charset/latin1` · `/text/latin1` | 中性类型 + 非法 UTF-8（→二进制） / 声明 text/*（→按文本） |
| `/cookies/set` · `/cookies/echo` | Cookie 罐写入与回显 |
| `/auth/echo` · `/auth/require` | 认证头回显 / 无头即 401 |
| `/multipart` | multipart 表单回显 |
| devserver `-tls-echo` | 自签 HTTPS 回显（证书策略开关） |

> 注意：`Content-Type` 请求头显式存在时，runner 不会再注入由 body 推导的头（multipart 需要带 boundary 的那个），写 multipart 用例要先把请求头清空。

## 写用例的规矩（硬性）

1. **定位**：`data-testid` > `role`/`title`/语言包文案；禁止 scoped 类名（`.seg-tab` 这类带 hash 的语义）、禁止 `nth-child`。元素没有稳定句柄时，先在组件上加 `data-testid="模块.子域.元素"`。
2. **文案**：`t('resp.fieldsTab')`（`helpers/i18n.ts`），不写死中英文；英文断言用 `tEn()`。
3. **断言优先级**：IPC 返回值 / 集合文件落盘 / `localStorage` / `getComputedStyle` > DOM 文本 > 截图。JSON 内容用 `rawJSON(page)` 后按字段断言，别去比字符串空格。
4. **等待**：用 `expect.poll` / `toBeVisible` 等条件等待；**禁止** `waitForTimeout` 掩盖竞态（唯一例外是「等一轮文件监听」这类确有必要处，并写注释）。
5. **隔离**：`app.newCollection(seed)` 建临时集合；`openCollection()` 会复位设置；不得依赖其它用例的残留状态。
6. **命名**：用例标题以功能点编号开头（`[E23]`、`[H19]`…），与 `doc/客户端功能规划与完成情况.md` §3 对应；需要外部网络的用例标 `@demo` 并在默认门禁排除。
7. **留证据**：需要人工看结果时 `console.log` 关键产物（集合文件清单、响应体），失败时 Playwright 自动留 trace/截图到 `.tmp/e2e-artifacts/run-*/`。

## 排障

| 现象 | 处理 |
|---|---|
| 启动即报 `.tmp/e2e-artifacts` 删除被拦 | 产物目录已按运行时间分目录；若手动清空过 `.tmp` 可直接重跑 |
| 用例卡在 hover 类按钮 | 用 `hoverAndClick()`；行内编辑态按 uid 定位（`treeRowByUid`） |
| `n-input` 上 `.fill()` 报错 | naive 把属性落在根 div → `.locator('input')` |
| 需要看运行中的界面 | `npm run test:e2e`（有头）；单条用 `--grep "关键字"` |
| 端口/进程残留 | fixture 结束时 kill；手工起过的 devserver 用 `pgrep -f devserver` 查看 |
