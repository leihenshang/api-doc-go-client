# 客户端 E2E 测试手册

Playwright + devserver + testfixtures 组成的端到端测试：在**真实构建产物**上驱动**真实 App 门面方法**
（前端 IPC 走 `POST /api/App/<Method>` 反射桥），因此同一条用例可以同时断言 **UI / 集合文件 / 真实响应** 三侧。

设计依据（为什么这么分层、AI 放在哪一步）见 `.doc/客户端UI与功能自动化测试设计.md`；AI 操作规程见 `ai/`。

## 运行

```bash
cd frontend
npm i && npx playwright install chromium   # 首次：装依赖与浏览器
npm run test:e2e                            # 有头 + slowMo=300（看得见操作）
npm run test:e2e:headless                   # 无头（CI、快速）
npm run test:e2e -- --grep @smoke           # 只跑冒烟
npm run test:e2e:headless -- cases/regression-exec.spec.ts   # 单个文件
E2E_SLOWMO=0 npm run test:e2e               # 全速有头
npm run test:e2e:ui                         # Playwright UI（交互式调试）
npx playwright show-trace ../.tmp/e2e-artifacts/run-*/<用例>/trace.zip   # 看失败现场
```

门禁入口（与 CI 同源）：**日常只跑 `bash scripts/check.sh`（静态 + 单测，几秒）**；
e2e 相关模式属耗时检查，仅在明确需要时执行：

| 命令 | 内容 | 耗时 |
|---|---|---|
| `bash scripts/check.sh` | gofmt → build → vet → go test → vue-tsc → i18n | 秒级 |
| `bash scripts/check.sh --smoke` | 上面的 + 前端构建 + `@smoke` e2e | ~1 分钟 |
| `bash scripts/check.sh --full` | 上面的 + 全量 e2e（排除 `@demo`） | ~3 分钟 |
| `bash scripts/check.sh --e2e-ui` | 直接开 Playwright UI | 手动 |

- 前置：`frontend/dist` 必须是最新构建（`--smoke` / `--full` 会先 `npm run build`；已构建过可加 `--skip-build`）。
  fixture 会自动 `go build` `cmd/devserver` 与 `cmd/testfixtures`。
- 规模：**49 条回归 + 1 条 @demo**，全量约 2 分钟，`@smoke`（4 条）约 20 秒。
- `@demo` 用例的环境变量：`E2E_DEMO_URL`（必填，如 `https://内网主机/xxx`，未设置则 skip）、`E2E_DEMO_STATUS`（选填，忽略证书后期望的状态码）。运行时：`E2E_DEMO_URL=… npm run test:e2e -- --grep @demo`。
- **隔离**：每个 worker 一套独立环境 —— 随机端口 + `API_DOC_CONFIG_DIR` 指向 `.tmp/e2e/run-*/config`
  （这个变量在 Windows 与类 Unix 都生效；只设 `XDG_CONFIG_HOME` 会在 Windows 上写进用户真实配置目录）。
  产物按运行时间分目录放在 `.tmp/e2e-artifacts/run-*`，不覆盖历史。

## 目录

```
frontend/e2e/
├─ cases/            用例（按功能域分文件，标题前缀功能点编号）
│   regression-exec.spec.ts        §5-1/2：证书策略、重定向、超时、状态码、响应形态、Cookie、认证、multipart
│   regression-collection.spec.ts  §5-3/4/5/16：round-trip、自写回环、树操作、参数表与 Bulk Edit
│   regression-response.spec.ts    §5-17：响应字段只追加、保存响应示例
│   regression-shell.spec.ts       §5-6/8/9/10/11/12/13/14/15：外壳、字体、弹窗、分栏、命令面板
│   regression-theme.spec.ts       §5-18/19/7：主题、圆角/遮罩、语言、Markdown 转义
│   regression-autosave.spec.ts    保存模型：手动/自动、冲突条两个出口、外部改动回填
│   regression-session.spec.ts     会话恢复、快捷键、取消发送、Cookie 面板、撤销重做
│   response-save-vars.spec.ts     保存▾ 菜单与字段表变量书签
│   req-body-highlight.spec.ts     JSON 体着色与高亮层对齐
│   tree-dnd.spec.ts               集合树指针拖拽
│   tree-guides.spec.ts            集合树连接线：缩进 / 祖先竖线延续 / └ 收线 / 横线落在行中线
│   multi-root.spec.ts             多工作目录：多根并存 / 切换活动根 / 标签随根切换 / 跨目录搜索 / 跨根拖动（请求单搬、分组整棵搬、目标重名拦截）/ 启动恢复上限与「最近打开」补开
│   mcp-allow.spec.ts             MCP 工作目录白名单：空白名单安全默认 / 加入目录 / 可写开关 / 移除
│   createDnsRegions.spec.ts       @demo：打真实外部 https 接口，未设 E2E_DEMO_URL 即 skip
├─ fixtures/         集合种子（basic / bruno-sample；`__FIXTURE__`、`__TLS__` 由 fixture 运行期替换）
├─ helpers/
│   app.ts           worker fixture：临时集合 + 独立配置目录 + 随机端口 + devserver/testfixtures/TLS
│   ui.ts            常用动作：openCollection/openRequest/send/openSettings/saveSettings/saveNow/rawJSON…
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
| `/json/flat` · `/json/nested` · `/json/withArray` · `/json/fewer` · `/json/big?rows=` | 固定形状 JSON；`withArray`/`fewer` 是一对（叶子字段为子集关系），配合「字段只追加」 |
| `/status/{code}` | 任意状态码（404 带 `{"status":404}`；204/304 不带体） |
| `/redirect/{n}` | n 跳到 n-1，`n=0` 返回 `{"hops":0}`；`?abs=1` 绝对 Location、`?loop=1` 自环（超上限分支） |
| `/delay?ms=` | 延迟（超时分支） |
| `/binary?kb=` | 二进制（客户端判二进制 → base64 + 提示） |
| `/gzip` | gzip 响应（Go client 自动解压） |
| `/charset/latin1` · `/text/latin1` | 中性类型 + 非法 UTF-8（→二进制） / 声明 `text/*`（→按文本）；两者字节一致 |
| `/cookies/set?name=&value=` · `/cookies/echo` | Cookie 罐写入（host-only）与回显 |
| `/auth/echo` · `/auth/require` | 认证头回显 / 无头即 401 |
| `/multipart` | multipart 表单回显（`contentType` 为去参数的媒体类型，可验证 boundary） |
| devserver `-tls-echo` | 自签 HTTPS 回显（证书策略开关） |

> `Content-Type` 请求头显式存在时，runner 不会再用 body 推导的头覆盖它 —— **唯一例外是 multipart**
> （那个头必须带发送时生成的 boundary，所以一律以派生值为准）。

夹具端点本身有 Go 契约测试（`cmd/testfixtures/fixtures_test.go`），跑在门禁里：
改了端点就能立刻发现与用例的期待不一致。

## 写用例的规矩（硬性）

1. **定位**：`data-testid` > `role`/`title`/语言包文案；禁止 scoped 类名、禁止 `nth-child`。
   元素没有稳定句柄时，先在组件上加 `data-testid="模块.子域.元素"`。
2. **文案**：`t('resp.fieldsTab')`（`helpers/i18n.ts`），不写死中英文；英文断言用 `tEn()`。
3. **断言优先级**：IPC 返回值 / 集合文件落盘 / `localStorage` / `getComputedStyle` > DOM 文本 > 截图。
   JSON 内容用 `rawJSON(page)` 后按字段断言，别去比字符串空格。
4. **保存模型是「手动」**（默认）：改完要断言磁盘的用例必须显式 `saveNow(page)`（`Ctrl+S` + 等保存指示）；
   只有开了「自动保存」的设置项才会编辑即落盘。
5. **等待**：用 `expect.poll` / `toBeVisible` 等条件等待；**禁止** `waitForTimeout` 掩盖竞态
   （唯一例外是「等一轮文件监听」这类确有必要处，并写注释）。
6. **树/拖拽类用例**：一次移动会触发集合重载 → 树重排，拖拽前必须等到元素位置**稳定**
   （`tree-dnd.spec.ts` 的 `waitBox` 就是干这个），否则会偶发落错行。
7. **隔离**：`app.newCollection(seed)` 建临时集合；`openCollection()` 会复位设置；不得依赖其它用例的残留状态。
8. **命名**：用例标题以功能点编号开头（`[E23]`、`[H19]`…），与 `doc/客户端功能规划与完成情况.md` §3 对应；
   需要外部网络的用例标 `@demo`。
9. **留证据**：需要人工看结果时 `console.log` 关键产物（集合文件清单、响应体），失败时 Playwright 自动留
   trace/截图到 `.tmp/e2e-artifacts/run-*`。

## 排障

| 现象 | 处理 |
|---|---|
| 启动即报 `.tmp/e2e-artifacts` 删除被拦 | 产物按运行时间分目录；若手动清空过 `.tmp` 可直接重跑 |
| 提示缺浏览器 | `cd frontend && npx playwright install chromium` |
| 配置/历史被写进真实用户目录 | 确认 fixture 传了 `API_DOC_CONFIG_DIR`（只设 `XDG_CONFIG_HOME` 在 Windows 无效） |
| 用例卡在 hover 类按钮 | 用 `hoverAndClick()`；行内编辑态按 uid 定位（`treeRowByUid`） |
| `n-input` 上 `.fill()` 报错 | naive 把属性落在根 div → `.locator('input')`；但 `req.url` 的 testid 就在 `input` 上，直接 fill |
| 拖拽用例偶发失败 | 树在集合重载后会重排：确认用了 `waitBox`（等位置稳定）而不是单次 `boundingBox` |
| 需要看运行中的界面 | `npm run test:e2e`（有头）；单条用 `--grep "关键字"` |
| 端口/进程残留 | fixture 结束时 kill；手工起过的 devserver 用 `pgrep -f devserver` 查看 |
