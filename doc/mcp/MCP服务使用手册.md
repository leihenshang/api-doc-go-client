# MCP 服务使用手册

面向**用 AI 工具操作接口集合**的人：怎么装、怎么挂、每个工具能干什么、遇到报错怎么办。
设计原理与分层见《MCP服务设计.md》；客户端内嵌服务见文末《附》。

## 一、它能做什么

把 api-doc-go 的**集合目录**（项目）接给 AI（Claude Desktop / Cursor / Cline 等），
让 AI 查接口、读详情、建请求、跑请求、管理环境变量，并把响应留存成示例。共 **36 个工具**：

| 类别 | 工具 | 一句话 |
|---|---|---|
| 查询 | `list_tools` | 工具总览：一次拿到全部工具名 + 一句话用途 + 必填参数（**不必逐个拉 schema**，省上下文）；`filter` 过滤、`mode=read|write` 分类 |
| 查询 | `list_workspaces` / `use_workspace` | 看授权了哪些工作目录；选定**本会话**默认目标（之后 `project` 可省略） |
| 查询 | `list_projects` | 有哪些项目（集合） |
| 查询 | `get_project_modules` | 某项目有哪些模块，每个模块下有哪些请求 |
| 查询 | `get_request_detail` | 某接口的完整详情（含概述 `summary`、请求体字段表 `body.fields`）+ 响应字段文档（respDoc）+ 已保存的响应示例；`uid` 取单个、`uids` 批量 1–10 个；`sections` 可只取分段；返回**完整** `hash` 供冲突保护 |
| 查询 | `search_requests` | 搜索（`exactMatch` 整词、`fields` 限定范围，每条带 `snippet` 命中行） |
| 查询 | `search_docs` | 搜文档条目正文：命中行号 + 片段（每篇最多 5 处）；`includeHash=true` 时可直接拿到 `update_doc.ifMatch` |
| 查询 | `list_envs` | 环境与变量（**敏感值只回掩码**，每个环境带 `hash`）；`limit`/`offset` 分页 |
| 查询 | `list_docs` / `get_doc` | 文档条目：列清单（分页 + `hash`）/ 读正文（`uid` 或 `path` 单篇、`uids` 批量 1–10 篇，支持分段/大纲） |
| 查询 | resource `apidoc://doc-template` / `apidoc://error-codes` / `apidoc://collection-format` | 接口说明模板 / 错误码表 / 集合结构契约（原 `get_doc_template` 工具已迁移为 resource） |
| 写 | `create_request` | 新建请求（手填 / cURL 导入 / 复制现有；可带概述 `summary`、完整 body、字段表 `bodyFields`、settings/respDoc；`uid` 幂等、`onConflict` 默认 error） |
| 写 | `import_collection` | 批量导入：把 Postman / OpenAPI / api-doc-go 集合文件的**正文**一次导入成多条请求（省掉逐个 `create_request` 的往返）；`format` 必填，`parent` 指定落在哪个模块，`dryRun=true` 只解析并回报条数、不写盘 |
| 写 | `create_module` | 新建模块（目录，支持嵌套） |
| 写 | `create_project` | 新建项目（集合目录 + manifest）；`root` 必须是**工作目录**（不能是项目目录本身），错误码区分 `[FORBIDDEN]`/`[READONLY]`/`[CONFLICT]`/`[INVALID]` |
| 写 | `update_request` | 改请求（params/settings/respDoc/概述/字段表均可局部更新；`body` 与 `bodyFields` 相互独立；`patch` 按名合并一行）/ 移动模块（支持 `ifMatch` 冲突保护） |
| 写 | `update_requests` | 批量改 1–10 个接口（每条 `uid` + 要改的字段）；单条失败不影响其它，`failed[]` 带 `code` 与 `uid`；`dryRun` 只校验 |
| 写 | `delete_request` | 移除请求（文件进 `.trash`，可找回） |
| 写 | `create_doc` / `update_doc` / `delete_doc` | 文档条目的新建 / 改正文·改名·移动 / 删除（进 `.trash`） |
| 写 | `save_doc_image` | 保存图片（base64）到项目 `assets/`，返回可写进 Markdown 的 `/doc-assets/<sha256>`（与客户端编辑器同款） |
| 写 | `rename_module` / `delete_module` | 模块改名（显示名）/ 删除（空模块或 `recursive` 整棵，进 `.trash`；`pruneEmpty=true` 清理全部空模块） |
| 写 | `rename_project` / `delete_project` | 项目改名（显示名）/ 删除（必须 `confirm`，目录移入授权根 `.trash` 可恢复） |
| 写 | `save_response_example` / `update_response_example` / `delete_response_example` | 响应示例的手工新增（不发送请求）/ 局部更新（改名·状态码·响应体）/ 删除（进 `.trash`） |
| 执行 | `send_request` | 发请求；默认把响应存成响应示例 |
| 写 | `create_env` / `rename_env` / `delete_env` | 环境的新建 / 改名 / 删除（旧文件进 `.trash`） |
| 写 | `set_env_var` / `delete_env_var` | 单个变量的 upsert / 删除（敏感值回填掩码 = 不改） |

各工具的完整入参与示例见「四、工具详解」；参数速查见「十二、命令与参数速查」。

工具按风险分三级（服务端元数据表锁定）：**low = 11 个只读工具**（随便用，无副作用）；**medium = `send_request`**
（真实网络外发）；**high = 24 个写工具**（落盘修改集合/环境/示例）。客户端**内嵌服务**默认对 medium/high 的每次调用
**弹窗要你确认**（见「六、风险操作确认与调用审计」）；独立 `mcpserver.exe` 默认不弹窗，靠 `-readonly` / 只读授权 /
`-require-consent` 约束。

> **敏感值（`secret: true`）的值永远只回掩码 `••••••`**，这是有意的。轮换就把新值传给 `set_env_var`；
> 不想动它就把掩码原样传回（见 4.10）。
>
> AI 侧看到的返回是一段**可读文本**（不是结构化 JSON），超大内容会被裁剪并明确标注（见「七、输出裁剪」）。

## 二、快速开始

### 1. 准备项目根目录

先把要交给 AI 的目录**授权**给它（白名单）：`-allow-writable D:\collections`（可写）或
`-allow D:\collections`（只读）；客户端内嵌服务在「状态栏 MCP 入口 → MCP 服务分区 → 可访问的工作区」里
对当前打开的工作区逐条勾选授权。
**白名单之外一律拒绝**（读不到、也写不了）；没授权任何目录时客户端内嵌服务不会启动监听。

「项目」= 一个集合目录 = **含 `opencollection.yml` 的目录**。把若干集合放进同一个父目录：

```
D:\collections\                 ← 授权给它（-allow-writable / 内嵌服务勾选）
├─ shop-api\                     ← 项目（集合）
│  ├─ opencollection.yml         ← 有这个文件才算项目
│  ├─ api\ping.yml
│  └─ environments\dev.yml
└─ admin-api\                    ← 另一个项目
   └─ opencollection.yml
```

目录里没有 `opencollection.yml` 会被跳过；Bruno 集合（有 `bruno.json`）也会跳过并说明原因
（需先用客户端的「导入」功能转换）。

### 2. 构建并挂载

```bash
cd api-doc-go-client
GOPROXY=https://goproxy.cn,direct go build -o mcpserver.exe ./cmd/mcpserver
```

以 Claude Desktop 为例（Cursor 用项目内 `.cursor/mcp.json`，Cline 在 MCP Servers 设置页填同样的命令与参数）：

```json
{ "mcpServers": { "api-doc-go": { "command": "D:\\tools\\mcpserver.exe", "args": ["-allow-writable", "D:\\collections"] } } }
```

配置后**重启该 AI 工具**（stdio 型 MCP 在进程启动时握手）。然后在 AI 里问一句
**「列出我的接口项目」**，能看到 `list_projects` 的调用即挂载成功。

### 3. 启动参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `-allow` | ❌ | 授权的工作目录（逗号分隔，**只读**）：目录自身或其一级子目录含 `opencollection.yml` 即算项目 |
| `-allow-writable` | ❌ | 授权且**可写**的工作目录（逗号分隔）；写工具只在可写授权里生效 |
| `-root` | ❌ | **旧写法**：等价于 `-allow-writable` 传同一个目录（保留兼容） |
| `-http` | ❌ | 留空 = 只走 stdio；给地址（如 `127.0.0.1:8189`）则**同时**提供 streamable HTTP（路径 `/mcp`）。只给端口（`:8189`）按**回环**处理，跨主机必须写 `0.0.0.0:8189` |
| `-http-token` | ❌ | HTTP 的 Bearer 令牌。**监听非回环地址且未给时自动生成并打印到 stderr**（每次启动重新生成，客户端配置要跟着换；要稳定令牌就显式传） |
| `-http-allow-origin` | ❌ | 允许的浏览器来源（Origin），逗号分隔，`*` = 任意。仅浏览器端客户端需要，同时是 DNS rebinding 防护 |
| `-readonly` | ❌ | 只读模式：不注册写工具；`send_request` 仍可用但**不保存**响应示例 |
| `-require-consent` | ❌ | 无头严格模式：`send_request` 与全部写工具**一律拒绝**（独立进程没有界面可弹确认，宁可全拒也不静默放行）；只读工具不受影响 |

两个约定：**stdio 下 stdout 属于 JSON-RPC 协议**（日志一律走 stderr）；`-http` 模式下进程**等信号退出**，
不会因为 stdin 关闭而退出。

## 三、项目模型（搞懂三条就够用）

0. **只有被授权的工作目录（白名单）可见**：`-allow` / `-allow-writable`，或客户端设置里的白名单。
   白名单为空 = 没有任何项目 —— 这是安全默认，不是故障。先用 `list_workspaces` 看授权了什么，
   再用 `use_workspace` 选定默认目标（之后各工具的 `project` 都可以省略）。这个默认目标是**按会话**的：
   同一次连接里选定即可，多个客户端（HTTP 下多个会话）互不影响。
   **什么时候会失效**：客户端重连（HTTP 传输每次握手都是新的 `Mcp-Session-Id`）、MCP 服务重启
   （改设置保存会重启内嵌服务）、客户端退出——都等于换了会话，之前选定的目标就没了。
   那时省略 `project` 会报 `[INVALID] 未指定 project`，报错里会说明原因，重新 `use_workspace` 即可。
   显式传 `project` **不会**清掉已选定的默认值。
1. **一个项目 = 一个集合目录**，由授权目录扫描得到；目录变化后用 `list_projects` 的 `refresh: true` 重扫。
2. **三种方式指定项目**（入参都叫 `project`）：**uid** → **目录名/集合名** → **路径**（绝对，或相对授权目录）。写错会报错并列出候选。
3. **懒加载**：启动只扫目录（只读 manifest，不改动项目），第一次访问某项目才真正打开。因此
   `search_requests` 不带 `project` 时**只搜「已加载」的项目** —— 新项目先 `get_project_modules` 或
   `list_projects`（不带 query）打开一次。

读操作前会自动重扫该项目的集合与索引，所以刚创建的请求立刻能被搜到。

## 四、工具详解

> 下表「参数」列均为 JSON 字段名；标 ✅ 的是必填。`project?` 表示**可省略**：
> 省略时用 `use_workspace` 选定的**本会话**默认工作目录（没选过则报错并提示）。

### 4.0 `list_tools`（工具总览）

不知道有哪些工具、或不想为「查一个参数」拉整份 schema 时先用它：一次拿到**全部工具名 + 一句话用途 +
必填参数**，并按读/写标注（`[读]` / `[写]`）。

| 参数 | 说明 |
|---|---|
| `filter?` | 按工具名或描述关键词过滤（大小写无关） |
| `mode?` | `all`（默认）/ `read`（只读与会话类）/ `write`（会落盘改集合的） |

返回同时带 `structuredContent`：`{total, returned, tools:[{name, description, risk, write, required}]}`，
`required` 从入参结构体推导（与 `inputSchema` 的 `required` 同源）。要某个工具的完整入参再取它的 schema ——
`create_request` / `update_request` 的 schema 最大（约 7–8KB），只问「有哪些工具」时别拉它们。

### 4.0.1 工具组（按需注册）

设置页「MCP 服务 → 启用的工具组」可以只注册用得上的部分，减少 `tools/list` 的体积：

| 组 | 内容 |
|---|---|
| `session`（**恒开，不可关**） | `list_tools`、`list_workspaces`、`use_workspace`、`list_projects` |
| `collection` | 模块/接口的查询、新建、修改、批量修改、删除、移动、批量导入 |
| `docs` | 文档条目与图片 |
| `env` | 环境与变量 |
| `example` | 响应示例 |
| `network` | `send_request` |

空列表 = 全部启用（旧配置默认）。组只影响**注册哪些工具**，不改变单次调用的权限判断
（谁能写仍由只读模式与工作目录授权决定）。关闭某组后，`list_tools` 也不再列出该组工具。

### 4.0.2 `list_workspaces` / `use_workspace`

- `list_workspaces`：列出**授权的工作目录**（含只读/可写）、其中项目与当前默认目标；AI 拿不到项目时先看它。
- `use_workspace`：选定默认工作目录（传工作目录 path，或项目标识：名称/路径/uid）。选定后其余工具的
  `project` 都可省略（**按会话生效**：同一次连接内有效，不会改到别的客户端）；不在白名单内会直接拒绝。
  未选定且未传 `project` 时，工具会报错并提示用 `use_workspace` 或显式传 `project`。

### 4.1 `list_projects`
`project?`（按名/路径模糊过滤）、`refresh?`（重新扫描授权目录）。
返回每个项目的 `路径/名称/uid/请求数/模块数/环境列表`；不可用目录带跳过原因。统计在**首次访问该项目后**才有值（懒加载）。

### 4.2 `get_project_modules`
`project?`、`query?`（过滤模块名与模块下请求名/方法/路径）、`includeRequests?`（附带请求清单含 uid）、
`onlyMatched?`（**默认 true**：带 query 时只返回有命中的模块——模块名命中，或其下有命中请求；传 false 可看全部模块，汇总行始终给出模块总数）。
返回模块树 + 每模块请求数/子模块数；根目录下的请求归到 `(根目录)`。

### 4.3 `get_request_detail`（单个 / 批量）
`project?`、`uid?` / `path?`（单个，二选一）或 `uids?`（批量 1–10 个）、`query?`（在示例名/文档名里再筛）、`includeBody?`、`maxBodyBytes?`（默认 8192）、
`sections?`（只取指定分段：`overview`/`docs`/`bodyFields`/`respDoc`/`examples`；不传 = 全量；未知值报错）。
返回 method/url/params/headers/body/auth/settings/grpc + **概述 `summary`** + 请求内 `docs` + **请求体字段表 `body.fields`**
（5 列：字段名/必填/类型/示例值/说明，空表不出）+ **响应字段文档 `respDoc`**
（返回类型/字段表全量；样例不传 includeBody 时只回前 2048 字节）+ 关联的 `docs/*.md` + **已保存响应示例列表**
+ **完整 `hash`**——改这个接口时把它作为 `update_request.ifMatch` 传入即可获得冲突保护。

批量：传 `uids`（1–10 个，超出直接报错），其余参数与本节相同，结果按 `===== uid =====` 分组。
`uid` 与 `uids` 只能给一个。

### 4.4 `search_requests`
`query` ✅（名称/URL/方法/路径/header/说明/请求体/响应字段/请求体字段表都能命中，中文按子串）、`project?`（省略 = 所有已加载项目）、
`limit?`（默认 50，上限 200）、`exactMatch?`（true = 整词匹配：不走分词/子序列，长标识符用它消误报）、
`fields?`（`name`/`uri`/`docs`/`respDoc`/`bodyFields`/`header`/`body`，不传 = 全部）、
`includeHash?`（true = 每条结果附完整 `hash`，可直接回传 `update_request.ifMatch`；默认不带以省体积）。
返回按相关度降序，每条带 `score`、`matched`（命中的字段）与 `snippet`（命中行±1，≤200 字符）。

配套 `search_docs`：`pattern` ✅、`project?`、`exactMatch?`、`limit?`——按行搜文档条目正文，返回 `uid`/路径/`L<行号>: 片段`。

### 4.5 `create_request`
三种来源（互斥，按顺序判断）：

| 方式 | 参数 | 说明 |
|---|---|---|
| 手填 | `name` ✅ + `method`/`url`/`headers`/`params`/`bodyType`+`bodyRaw` 或 `body`/`auth`/`docs`/`settings`/`respDoc`/`folder` | `{"project":"shop","folder":"api/user","name":"login","method":"POST","url":"{{host}}/api/login"}` |
| cURL 导入 | `name` ✅ + `curl` | `{"project":"shop","name":"from-curl","curl":"curl -X POST https://x/api/login -H 'Content-Type: application/json' -d '{\"u\":\"a\"}'"}` |
| 复制现有 | `name` ✅ + `copyFromUid` | `{"project":"shop","name":"login-copy","copyFromUid":"<uid>"}` |

同名或同 uid 默认 **`[CONFLICT]`**（与 `create_doc` 一致）：确需并存传 `onConflict=suffix`（加 `-2`、`-3`…），覆盖已有条目传 `onConflict=overwrite`；`uid` 可固定身份做幂等导入；`headers`/`params` 元素为 `{"name":..,"value":..,"enabled":true,description?,type?}`；
`body` 是完整形态 `{"type":"form","form":[{name,value,enabled,type:"file"}]}`（支持 urlencoded/multipart 行），
**按子项合并**（只传 `raw` 不会动 `form`，只传 `form` 不会动 `raw`；`bodyType`/`bodyRaw` 只覆盖对应的两个子项）；
**`bodyFields`** 是请求体字段表格 `[{"name","type","required","example","description"}]`，与 `body` **相互独立**——只传 `body` 不会清掉表格；
**`summary`** 是 API 概述（纯文本一句话，与 `docs` 分列）；
`settings` 只作用于**本请求**（超时/重定向/忽略证书/URL 编码），不碰全局配置；`respDoc` 写响应字段文档（**按子项合并**，
只给 `params` 字段表时样例/响应头/状态码不动；gRPC 拒绝）；
`docs` 结构先读 resource `apidoc://doc-template`；`method` 仅接受 GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS/TRACE/CONNECT/GRPC（其它值 `[INVALID]`）；返回落盘后的 `uid` 与 `path`。

**文件名**：默认由 `name` 推导（中文名 → 中文文件名，集合是纯文本、文件即真相源，这是有意的）。
要 ASCII 文件名（脚本/工具链友好）就传 **`fileName`**（不含 `.yml`），例如 `{"name":"创建文档","fileName":"create-doc"}`
→ 落 `create-doc.yml`，显示名仍是「创建文档」。`create_doc` 同样支持 `fileName`（不含 `.md`）。

### 4.5.1 `import_collection`（批量导入）

把外部集合**一次**导入成多条请求，省掉逐个 `create_request` 的往返（与客户端「导入」同源，区别是这里传正文、不弹选文件框）。

| 参数 | 说明 |
|---|---|
| `project?` | 目标项目 |
| `format` ✅ | `postman` / `openapi` / `apidocgo`（本客户端导出的集合整包）；Bruno 目录导入请用客户端的导入功能 |
| `text` ✅ | 集合文件的**正文内容**（不支持文件路径：先读出内容再传） |
| `parent?` | 落在哪个模块下（相对集合根），留空 = 项目根 |
| `dryRun?` | `true` = 只解析并回报将导入的条数，**不写盘**（「已存在会跳过」只有真导入时才知道，故预演的 `skipped` 恒为 0） |

Postman 的目录映射为模块、OpenAPI 的 tag 映射为子模块；已存在「同方法同地址」的请求会跳过（`skipped`）。
返回 `{imported, skipped, failures}`（`failures` 最多 10 条）。

### 4.6 `create_module` / `create_project`
- `create_module`：`project?` + `name` ✅（+ `parent` 父模块路径，留空 = 根），支持嵌套（`parent=api` 建 `api/user`）。
- `create_project`：`name` ✅（manifest 显示名）+ `dirName`（目录名，留空 = 与 name 相同）+ `root`（**必须是工作目录**，
  留空 = 第一个可写授权目录）。`root` 指向项目目录本身/项目内子目录会 `[INVALID]` 并提示改传父级（否则新项目不会被扫描到）；
  白名单外 `[FORBIDDEN]`、只读 `[READONLY]`、重名 `[CONFLICT]`。建完 `list_projects` 立刻可见。

### 4.7 `send_request`
`project?` +（`uid` 或 `method`+`url`）、`env?`（留空 = 第一个环境）、`saveExample?`（默认 false；要留存示例时显式传 true）、
`exampleName?`（默认 `"{状态码} 响应"`）、`includeBody?`（不传 = 返回体）、`maxBodyBytes?`。
临时请求（给 `method`+`url`）**不落盘也不存示例**；存下的示例客户端响应面板「本次响应 ▾」能直接回看。

### 4.8 `update_request` / `update_requests`
`project?`、`uid` ✅，以及**只改传入的** `name`/`url`/`method`/`params`/`headers`/`body`/`summary`/`bodyFields`/`auth`/`docs`/`settings`/`respDoc`，
**`folder`（移动请求到别的模块，相对集合根，如 `api/user`；传空串 = 移回项目根）**——请求建错目录时用它挪，
不需要删了重建；模块不存在会 `[NOT_FOUND]`，路径用 `get_project_modules` 给的值。
**`body` / `respDoc` 都是「指针合并」语义（本版重点）**：入参里**为 `null`（不传）的子项 = 保留原值**，
**显式空值 = 清空**。

| 入参 | 不传（null） | 显式空值 | 说明 |
|---|---|---|---|
| `body.type` / `body.raw` / `body.data` | 保留 | `""` 清空 | 只传 `body.raw` 就只改原文，`form`/`fields`/`descs` 全都不动 |
| `body.form` | 保留原表单行 | `[]` 清空 | 与 `raw` 互不影响（终于不用为了改 raw 编造 `form`） |
| `body.descs` / `body.fields` | 保留 | `{}` / `[]` 清空 | 与 `bodyFields` 是同一份表格的两条入口 |
| `respDoc.params` | 保留 | `[]` 清空 | 可只写字段表补说明 |
| `respDoc.sample` | 保留 | `""` 清空 | 旧版传空串清不掉，本版可以 |
| `respDoc.status` | 保留 | `0` 清零 | 同上 |
| `respDoc.type` / `headers` / `contentType` | 保留 | `""` / `[]` 清空 | — |

`bodyFields` 显式传了才覆盖表格（空数组 = 清空）；`summary` 传空串即清空概述。
`params` 整表覆盖（传空数组 = 清空）；`settings` 仅单请求级，不碰全局配置。

**只加/改一行用 `patch`（避免先抄全表）**：`patch: {params?, headers?, bodyFields?}` 按 `name`
（字段表按字段名）合并 —— 同名**只覆盖传入的子字段**、没有则追加；与对应的整表字段（`params` / `headers` / `bodyFields`）互斥。
三个子表都是「不传 = 不动，显式传空数组 = 清空整表」，与 `params: []` 的整表覆盖语义一致。
`enabled` 不传按 `true`、`required` 不传按 `false`，不必为了「加一行」编造这两个值。
典型用法：`{"patch":{"bodyFields":[{"name":"id","type":"string","description":"用户ID"}]}}`；
后续只想改说明再传 `{"patch":{"bodyFields":[{"name":"id","description":"新说明"}]}}`，类型/必填/示例都保留。

**批量**：`update_requests` 传 `items: [{uid 或 path, ...同上字段}]`（1–10 条），逐条应用；
单条失败只出现在 `failed[]`（含 `index` / `uid` / `path` / `code` / 原因），不影响其它条目；`dryRun=true` 只校验。
按 `path` 定位时，成功条目的 `updated[]` 回填的是解析后的 uid。

**长文档局部改 `replaces`**：`replaces: [{old, new, replaceAll?}]` 只改 `docs` 里出现的片段（与 `docs` 互斥）；
任一 `old` 找不到即整单拒绝、不落盘。动过 `docs`/`replaces` 时，成功响应附落盘确认：
能按行定位改动时给「第 N–M 行 ±1 行」片段（长文档改一段也能看到），定位不到时回落前 5 行。
`ifMatch` 不匹配时返回明确错误 + 当前完整 hash（不会静默覆盖；不传 = 最后写者赢）。

**`section` 节点守卫（可选）**：一次调用只想动一个节点时传 `section`，混用其它节点的字段会**直接报错**
（报错里会点名越界的字段名 + 该 section 的允许清单，照着改就行）：

| `section` | 允许的字段 | 典型用法 |
|---|---|---|
| `request` | `name`/`method`/`url`/`params`/`headers`/`body`/`bodyFields`/`summary`/`auth`/`settings`/`folder` | 只改请求本身，防止顺手写坏响应文档 |
| `response` | 仅 `respDoc` | 只补响应字段表/样例 |
| `docs` | 仅 `docs` | 只写接口说明 |
| 省略 | 不限制（兼容旧调用） | — |

```json
// 只改请求体原文（form/字段表格/备注一律不动）
{"project":"shop","uid":"<uid>","section":"request","body":{"raw":"{\"u\":\"a\"}"}}
// 只清空响应样例与状态码
{"project":"shop","uid":"<uid>","section":"response","respDoc":{"sample":"","status":0}}
```
`ifMatch` 传详情里的 `hash`（12 位前缀即可，服务端按 git 风格前缀比对，≥8 位有效）→ 磁盘文件在此期间被外部改动就
**拒绝写入**；**不传 = 最后写者赢**（与桌面端一致）。改名会自动同步文件名。

### 4.9 `delete_request`
`project?` + `uid` / `path`（二选一）；`ifMatch`、`confirmName`、`dryRun` 可选。文件**移入集合的 `.trash/`**（带时间戳），不会真正删除。
误删恢复：把文件从 `.trash/` 挪回原位并 `list_projects(refresh: true)`。

### 4.9.1 `delete_requests`（批量）
`project?` + `items`（1–10 条，每条 `uid` 或 `path` + **必填 `ifMatch`** + 可选 `confirmName`）+ `confirm?`（填项目显示名/目录名，不一致则整批拒绝）+ `dryRun?`。
`confirmName` 是逐条二次确认：非空时必须与该条请求的显示名一致，否则只拒绝这一条（同名请求较多时能防「删错那一个」）。
`failed[]` 带 `index` / `uid` / `code` / `error`，单条失败不影响其它条。

### 4.10 环境变量工具（6 个）

环境 = `environments/<env>.yml`，请求里用 `{{变量名}}` 引用。

| 工具 | 参数 | 说明 |
|---|---|---|
| `list_envs` | `project?`、`query?`、`limit?`、`offset?` | 列出环境与变量；`query` 对环境名与变量名都过滤；每个环境带 `hash` |
| `create_env` | `project?`、`name` ✅、`vars[]?` | 新建环境（重名报错，不覆盖）。每条变量 `{name,value,secret,enabled}`，**`enabled` 不传 = 启用** |
| `rename_env` | `project?`、`name` ✅、`newName` ✅、`ifMatch?` | 两个文件一起搬，旧文件进 `.trash`；目标名被占用报错；**只改大小写（`dev`→`DEV`）被拒绝** |
| `delete_env` | `project?`、`name` ✅ | 删环境（两个文件进 `.trash`）；环境不存在报错 |
| `set_env_var` | `project?`、`env` ✅、`name` ✅、`value` ✅、`secret?`、`enabled?`、`ifMatch?` | 按名 upsert，只改这一条；`enabled: false` 可停用（新增变量也认） |
| `delete_env_var` | `project?`、`env` ✅、`name` ✅、`ifMatch?` | 变量不存在报错 |

**名字规则**（服务端强校验）：环境名只允许字母/数字/`-`/`_`，且**判重大小写不敏感**（`DEV` = `dev`，因为环境名就是
文件名）；变量名必须匹配 `[A-Za-z_][A-Za-z0-9_]*`（否则 `{{name}}` 引用不到）。

**敏感值三条约定**：

1. **读不到真值**：返回里一律是 `••••••`。
2. **掩码回传 = 不动这条密钥**：值整串由 `• ● · *` 构成时保留原值并说明「传回的是掩码」；要轮换就传新值。
   只认这几种装饰字符，**`.` 和 `?` 不算掩码**（它们是现实中真实可能出现的密码字符）。
3. **空值被拒**：对已存在的敏感变量传 `value: ""` 会报错而不是清空；要清空就 `secret: false` 并给新值（会变成普通变量）。
   新建敏感变量时传掩码同样报错（没有旧值可保留，存下去就是假密钥）。

**并发保护（`ifMatch`）**：环境是**整份重写**的文件，所以 `list_envs` 每个环境带 `hash`，
`set_env_var`/`delete_env_var`/`rename_env` 传 `ifMatch: "<hash>"` 即可在「读之后被别人改过」时拒写；
不传 = 最后写者赢。成功后会返回**新 hash**，连续操作可链式带，不用反复 `list_envs`。

停用的变量（`enabled: false`）仍在文件里、也会被 `list_envs` 列出，但不参与 `{{name}}` 解析（输出里标「已停用」）。

> 客户端第一次打开集合会自动建 `dev` 环境（含 `host=http://127.0.0.1:8080`），所以直接 `create_env("dev")` 会报已存在。

### 4.11 resource：`apidoc://doc-template` / `apidoc://error-codes` / `apidoc://collection-format`

三个静态 resource（无参数、内容固定）：

- `apidoc://doc-template`：接口说明模板（见下）
- `apidoc://error-codes`：错误码表（`NOT_FOUND` / `CONFLICT` / `INVALID` … 及处理建议）
- `apidoc://collection-format`：集合结构契约（请求文件 YAML 段、`body.fields` 唯一真源、`.trash` 语义）

原 `get_doc_template` 工具已迁移为 MCP **resource**（无参数、内容固定的静态文本，不该占工具位与一次调用往返）。
客户端支持 resources 时直接读 `apidoc://doc-template`；不支持时按其协议能力说明会拿不到。
内容是接口说明（`docs` 字段）的 **Markdown 分节结构契约**：
固定小节顺序（简要描述 / 请求URI / 请求方式 / 请求头 / 请求参数表 / 请求体 / 请求示例 / 响应字段表 / 响应示例 / 备注）。
表列：请求参数 / 请求体字段为「参数名 | 必选 | 类型 | 说明」，**响应字段表为「参数名 | 必选 | 类型 | 示例值 | 说明」（必选按 `respDoc.params[].required` 输出「是/否」；表为空则整节省略）**
（示例值取自 `respDoc.params[].example`）；「请求体」一节在无请求体时整节省略（json 列类型 + 字段表，form/multipart 同理）；
「请求示例」一节在 curl 之后，JSON 请求体时另附一段缩进美化的请求数据示例。
均与客户端一键「生成接口文档」（`lib/genDocs.ts#buildApiDoc`）逐节对齐，并有 `{{host}}` 掩码规则。
`docs` 本身是自由文本不做强校验，但 AI 写 docs 前先取本模板可保证客户端展示与导出渲染一致；
模板里「响应字段」一节与 `respDoc` 同源，两边都写时内容需一致（含示例值列）。

### 4.12 文档条目工具：`list_docs` / `get_doc` / `create_doc` / `update_doc` / `delete_doc`

先分清两种「文档」，它们的载体完全不同：

| | 载体 | 用什么工具 |
|---|---|---|
| 接口说明 `docs` | 存在**请求文件**的 frontmatter 里，跟着接口走 | `create_request` / `update_request` 的 `docs` 字段（结构取 resource `apidoc://doc-template`） |
| 文档条目 | 集合树里**独立的 `.md`**，有自己的 uid 与路径，可挂在任意分组目录下 | 本节这 5 个工具 |

| 工具 | 参数 | 说明 |
|---|---|---|
| `list_docs` | `project?`、`query?`、`limit?`、`offset?`、`includeHash?` | 列清单（名称/路径/字节数/**hash**）；`query` 按名称与路径过滤；`limit` 默认 200（上限 1000）、`offset` 分页（首行给总数）；`includeHash` 默认 true（可直接回传 `update_doc.ifMatch`；会逐篇读文件算 hash，超大项目可传 false 提速） |
| `get_doc` | `project?`、`uid?` / `path?` / `uids?` / `paths?`（单篇或批量二选一，批量 1–10）、`startLine?`、`maxLines?`、`outline?` | 读正文（Markdown 原文）+ **完整 `hash`**（回传给 `update_doc.ifMatch`）。单个可传 `uid` 或 `path`（`docs/guide.md` 与 `docs/guide` 都认，找不到会列候选）；批量传 `uids` 或 `paths`。默认全文；长文档先 `outline=true` 看标题大纲（带 `L行号`），再 `startLine` + `maxLines` 分段读取（默认 100 行、上限 500）。区间读取带行号，**行号仅供阅读，写回时不要带**；`hash` 始终是全文 hash，只读一段也能安全 `ifMatch` |
| `create_doc` | `project?`、`name` ✅、`folder?`、`content?`、`uid?`、`onConflict?` | 身份以 **uid/path** 为准（`name` 只是显示名，可重复）；同名/同 uid 默认报 `[CONFLICT]`（含已有 uid/path），`onConflict=suffix` 自动加 `-2`、`overwrite` 复用已有条目并覆盖正文；`uid` 可做幂等导入 |
| `update_doc` | `project?`、`uid?` / `path?`、`content?` / `replaces?` / `append?` / `insertAfter?`（正文四选一）、`name?`、`folder?`、`section?`、`ifMatch?` | 只改传入的字段：`content` 覆盖正文（空串=清空）、`replaces` 局部替换、`append` 末尾追加、`insertAfter` 在某段后插入（`anchor` 默认必须唯一、`occurrence=N` 指定第 N 处；也可用 `anchorHeading="## 附录"` 插到该小节末尾，标题按归一化比较：全角空格、连续空格、闭合式 `## 附录 ##`、尾部 `*` 都认，找不到时列出文内标题）—— 四者互斥；`section=content` 只允许正文类字段、`section=meta` 只允许 `name/folder`；成功附「改动行 ±1 / 前 5 行」落盘确认 |
| `delete_doc` | `project?`、`uid` ✅ | 文件进 `.trash`，可人工找回 |

> 集合**根目录**下的 `.md`（含 `README.md`）即使没有 frontmatter 也会作为文档条目出现在 `list_docs` 里：
> uid 由相对路径哈希稳定生成（`md-<16 位 hex>`），首次 `update_doc` 保存时会把这些元信息（含 uid）写回文件顶部。
> 子目录里的 `README.md` 仍视为普通说明文件，不算条目。

```json
// 读一篇再改正文（带冲突保护）
{"project":"shop","uid":"142310aa-ce1f-4c9a-af33-0fa761e68aae"}
{"project":"shop","uid":"142310aa-ce1f-4c9a-af33-0fa761e68aae","content":"# 接入指南\n…","ifMatch":"<get_doc 返回的 hash>"}

// 长文档：先看大纲，再分段精读（search_docs 的 L 行号可直接当 startLine）
{"project":"shop","uid":"142310aa-…","outline":true}
{"project":"shop","uid":"142310aa-…","startLine":51,"maxLines":50}
```

分段读取的输出形如：

```
行 51–100 / 共 1240 行（下一段 startLine=101）；行号仅供阅读，写回时不要带

  51│ ## 请求示例
  52│ ...
```

写回仍用 `replaces`（`old` 取原文、**去掉行号**），`ifMatch` 用同一次响应里的 hash。

`get_request_detail` 返回的 `relatedDocs` 里能看到「与该接口相关的文档条目」，拿其中的 uid 直接 `get_doc` 即可。

> 接口自带说明（`docs` 段）同样可能很长：`get_request_detail` 支持 `docsOutline` / `docsStartLine` + `docsMaxLines`，口径与 `get_doc` 完全一致（`sections=["docs"]` 时最省体积）。

### 4.12.1 图片：`save_doc_image`

客户端 Markdown 编辑器里的「选图 / 粘贴图片」与本工具走同一条链路：校验（PNG / JPEG / GIF / WebP，≤5MB，**按文件头嗅探**）→ 按 sha256 存到 `<集合>/assets/<sha256>`（同图自动去重）→ 返回 `/doc-assets/<sha256>`。

```json
// 传纯 base64 或 data URL 均可
{"project":"shop","data":"iVBORw0KGgoAAAANSUhEUg…","name":"示意图"}
```

返回：

```
已保存图片：/doc-assets/9f2c…（64 位）
可直接写进 Markdown：![示意图](/doc-assets/9f2c…)
```

> 用法：先 `save_doc_image` 拿到 URL，再 `create_doc` / `update_doc`（或 `update_request.docs`）把 `![…](URL)` 写进正文。
> 图片先落本地集合（`assets/`，与离线附件同一目录）；**同步时随文档一起走**：推送会自动上传缺失图片并把正文换成服务端 URL（同一张图只传一次，映射存 `.sync.json` 的 `assetUrls`），拉取会自动下载服务端图片到本地并改写回 `/doc-assets/…`。外链图片原样保留。注意：同步后的图片在服务端走 `/uploads` 公开静态路径（与 Web 编辑器一致），敏感截图请勿入库。
> 仍未覆盖：导出 Markdown/HTML 不打包图片；手动「采用服务器版本」的冲突文档要等下一轮同步才把图片本地化。

### 4.12.1 dryRun（文档写工具）

`create_doc` / `update_doc` / `delete_doc` 支持 `dryRun:true`：

- `create_doc`：只做校验与冲突判定，返回将创建/覆盖的 uid 与 path（冲突仍是 `[CONFLICT]`）；
- `update_doc`：返回改动预览（行级定位）并校验 `ifMatch`，不改正文、不改名/移动；
- `delete_doc`：只返回将移入 `.trash` 的路径。

脚本化批量写之前可以先用它干跑一遍，避免「写一半才发现冲突」。

### 4.12.2 结构化输出（列表类工具）

共 **32 个工具**在文本之外返回 **`structuredContent`** 并声明 `outputSchema`（36 个工具里除 4 个）：

- **列表/搜索（7）**：`list_projects` / `list_docs` / `list_envs` / `get_project_modules` / `search_requests` / `search_docs` / `list_tools`
- **文档（4）**：`create_doc` / `update_doc`（`uid/path/size/hash`，dryRun 带 `planned`）/ `delete_doc` / `save_doc_image`（`url/sha256/size/deduped/markdown`）
- **接口（7）**：`create_request` / `update_request`（`uid/name/path/method/url`）/ `update_requests`（`updated[]/failed[]`）/ `delete_request`（+`deleted/trash/dryRun`）/ `delete_requests` / `import_collection`（`imported/skipped/failures/dryRun`）/ `send_request`
- **容器（6）**：`create_module` / `rename_module` / `delete_module`（含 `pruneEmpty` 的 `count`）/ `create_project` / `rename_project` / `delete_project`（`trashPath`）
- **环境（5）**：`create_env` / `rename_env` / `delete_env` / `set_env_var`（含 `hash` / `keptSecret`）/ `delete_env_var`
- **响应示例（3）**：`save_response_example` / `update_response_example` / `delete_response_example`

`outputSchema` 的数组元素与结果对象都写明**具体字段与 `required`**（可直接生成类型/做字段校验）。
未结构化的 4 个是会话类或"单条/批量双形态"工具（`list_workspaces`、`use_workspace`、`get_request_detail`、`get_doc`），
它们仍以文本为主，避免大正文重复一倍。文本输出保持不变，旧客户端不受影响。

### 4.12.3 写操作契约（统一）

| 能力 | 适用工具 | 说明 |
|---|---|---|
| `uid`（幂等身份） | `create_doc` / `create_request` | 给固定 uid：已存在时按 `onConflict` 处理，可做幂等导入 |
| `onConflict` | `create_doc` / `create_request` | `error`（默认，返回 `[CONFLICT]` + 已有 uid/path）/ `suffix` / `overwrite`；其余创建工具一律默认报冲突，不再静默加序号 |
| `dryRun`（只预演） | `create_doc` / `update_doc` / `delete_doc` / `create_request` / `update_request` / `delete_request` / `save_response_example` / `update_response_example` / `delete_response_example` / `delete_module` / `delete_project` / `delete_env` / `delete_env_var` / `delete_requests` | 返回将发生的动作与路径（结构化带 `planned`），不落盘 |
| `ifMatch`（并发保护） | `update_doc` / `update_request` / `set_env_var` / `rename_env` / `delete_doc` / `delete_request` / `delete_response_example` / `delete_env` / `delete_env_var` | 传读取时拿到的 hash，完整值或 12 位前缀均可（响应示例的 hash 见 `get_request_detail` 的 `examples[].hash=`）；不传 = 最后写者赢 |
### 4.12.3 错误码（所有工具统一）

失败结果的文本统一为 `失败: [CODE] 说明`，并同时返回
`structuredContent = {"error":{"code":"...","message":"..."}}`（错误码直接取字段，不必匹配文本前缀）：

| 码 | 含义 |
|---|---|
| `[NOT_FOUND]` | 项目 / 文档 / 接口 / 示例 / 变量不存在（含 uid 拼错） |
| `[CONFLICT]` | 同名冲突、`ifMatch` 不匹配（文档创建默认就是这条） |
| `[INVALID]` | 参数缺失/取值非法（含 SDK 的 inputSchema 校验失败） |

`[INVALID]` 里的 inputSchema 校验失败会**翻成中文并点名参数与工具**，例如
`失败: [INVALID] 缺少必填参数：name（工具 create_doc）`——不会再出现英文的
`validating "arguments": ... missing properties`（类型/枚举类校验的嵌套错误不带参数名，
此时如实附上精简后的原文并标明工具名）。
| `[READONLY]` | 工作目录是只读授权 |
| `[FORBIDDEN]` | 目录不在 MCP 白名单 / 无写权限（原 `[AUTH]` 已更名为 `FORBIDDEN`，破坏性变更） |
| `[LIMIT]` | 批量/分页超过上限（如一次 >10 个 uid） |
| `[IO]` | 读写磁盘失败 |
| `[INTERNAL]` | 其它内部错误 |

### 4.13 响应示例工具：`save_response_example` / `update_response_example` / `delete_response_example`

响应示例 = 集合里的 `examples/<模块>/<请求名>/<示例名>.yml`（纯文本、可 git diff、不参与目录树扫描）。
`send_request` 只能存「刚发出来的那个响应」；本节三个工具负责**手工补一条**、**改已存的**、**删掉不要的**。

`get_request_detail` 的示例段形如
`- 线上 504：504 · 132 ms · 12 字节（examples/order/查询/线上 504.yml，uid=3f2a…）`——
**`uid=` 就是 `exampleUid`**（改/删都靠它），对 `send_request` 存的、界面存的、上次会话存的示例一视同仁。

**改名规则与新建示例完全一致**：三层口径依次收紧，任一不过都直接报错并**保持原样**（示例不会消失）：

| 关卡 | 规则 | 超限时 |
|---|---|---|
| 条目名校验 | 非空、不含 `\ / : * ? " < > |`、不以点开头/结尾、≤120 字符 | 报「名称含非法字符」/「示例名含非法字符或过长」 |
| 规范化 | trim 首尾空格与点、非法字符换 `-`、截断到 100 字符 | 自动处理，不报错 |
| **字节上限** | 基名 ≤**240 字节**（文件名按**字节**算：ASCII 240 字、**中文约 80 字**） | 报「示例名过长：…字节，超过 240 字节上限」 |

**显示名与文件名永远是同一个值**（同一变量），不会出现「YAML 里叫 a/b、文件叫 a-b.yml」。
换名写盘成功后才会删旧文件，所以中途失败（磁盘满、目录不可写）不会丢数据。

**历史遗留的超长名示例仍可用**：只要文件名没变（比如只改 `status`/`body`），就地重写**不受上面的字节上限约束**；
删除时 `.trash` 里的名字会自动截断成「时间戳 + 截断名 + ~短哈希 + 原扩展名」，不会因为名字太长删不掉。
要换成合规的短名，直接 `update_response_example(name=…)` 即可。

| 工具 | 参数 | 说明 |
|---|---|---|
| `save_response_example` | `project?`、`uid` ✅（请求 uid）、`name?`、`status?`、`headers?`、`body?`、`contentType?` | 不发送请求，手工存一条示例。`name` 默认「响应示例」，`status` 默认 200，`contentType` 省略时按 `body` 首字符推断（`{`/`[` → `application/json`，其余 `text/plain`）；`size` 按 `body` 字节数记录。重名自动加 `(1)(2)` |
| `update_response_example` | `project?`、`uid` ✅、`exampleUid` ✅、`name?`、`status?`、`headers?`、`body?`、`contentType?` | **指针语义**：不传的字段不动，`status: 0` 可清零、`headers: []` 可清空。**改名会把磁盘文件名与 `path` 一起改**（同目录 `sanitize(新名).yml`，旧文件一并清掉）；`body` 变了 `size` 自动跟着变；重名自动加 `(1)` |
| `delete_response_example` | `project?`、`uid` ✅、`exampleUid` ✅ | 只删这一条示例（请求本身不动），文件进 `.trash` 可人工找回 |

```json
// 把外部文档里的响应片段留成一条示例
{"project":"shop","uid":"<请求 uid>","name":"第三方超时样例","status":504,"body":"{\"code\":504}","contentType":"application/json"}
// 给已存示例改名（磁盘文件名同步变）并改状态码/响应体
{"project":"shop","uid":"<请求 uid>","exampleUid":"<示例 uid>","name":"线上 504","status":504,"body":"{\"code\":504}"}
// 删掉不要的示例
{"project":"shop","uid":"<请求 uid>","exampleUid":"<示例 uid>"}
```

命名与落盘规则：示例名 → 文件名走同一套 `sanitize` 规则（与请求/文档一致），所以**改名 = 换文件**；
同一个请求下的示例按保存序号 `seq` 排「新 → 旧」。示例挂在**请求**下，移动/改名请求会让示例目录跟着走。

### 4.14 容器级工具：`rename_module` / `delete_module` / `rename_project` / `delete_project`

`create_project` / `create_module` 是对称面：容器级创建此前没有回收路径，这组工具补齐。

| 工具 | 参数 | 说明 |
|---|---|---|
| `rename_module` | `project?`、`path` ✅、`name` ✅ | 只改 `folder.yml` 的显示名；**目录与 uid 不变**（`path` 仍是原目录名） |
| `delete_module` | `project?`、`path?`、`recursive?`、`pruneEmpty?` | 默认只允许空模块；`recursive=true` 连同子模块/请求/文档整棵移入集合 `.trash`（回收站可恢复）；`pruneEmpty=true` 时忽略 `path`，自底向上清理全部空模块（返回 `count`，`assets`/`environments`/`examples` 不动，不支持 `dryRun`） |
| `rename_project` | `project` ✅、`name` ✅ | 改 `opencollection.yml` 的 `info.name`；目录不变 |
| `delete_project` | `project` ✅、`confirm` ✅ | `confirm` 必须等于项目名或目录名；目录移入**授权根**下 `.trash/<目录名>-<时间戳>`，可手动移回 |

## 五、典型工作流（可以直接对 AI 这么说）

| 目标 | 这么说 | AI 会依次调用 |
|---|---|---|
| 摸清陌生项目 | 「列出我的接口项目，看看 shop-api 里有哪些模块和请求」 | `list_projects` → `get_project_modules(includeRequests=true)` |
| 找接口 | 「找一下登录相关的接口，大概是 POST 提交用户名密码」 | `search_requests(query=login)` →（需要细节时）`get_request_detail` |
| 新建并跑通 | 「在 api/user 下新建 login，POST `{{host}}/api/login`，JSON 体…，然后发一次并存成示例」 | `create_module` → `create_request` → `send_request(saveExample=true)` |
| 从 cURL 导入 | 「把这个命令变成集合里的请求：curl -X POST …」 | `create_request(curl=…, name=…)` |
| 安全地改接口 | 「给 ping 的地址加 `?from=ai`，别覆盖别人的改动」 | `get_request_detail` 拿 hash → `update_request(ifMatch=…)` |
| 搭环境变量 | 「建 staging 环境：host=…、apiKey 是敏感值；把 host 停用」 | `create_env` → `set_env_var(enabled=false)` → `list_envs` |
| 改环境名/删环境 | 「把 staging 改名成 uat，然后把 uat 删掉」 | `rename_env` → `delete_env` |
| 补一篇独立文档 | 「在 docs 下写一篇《接入指南》，把这些内容整理进去」 | `list_docs`（避免重名）→ `create_doc(folder=docs, content=…)`（或 `get_doc` → `update_doc(ifMatch=…)`） |

只读模式（`-readonly`）下 AI 只能查 + 试发（不落盘），适合放行给「只答疑/查文档」的机器人。

## 六、风险操作确认与调用审计（客户端内嵌服务）

独立 `mcpserver.exe` 是无人值守进程，**默认不弹窗**；客户端**内嵌服务**跑在桌面上的真实窗口里，多了一层交互保护：

- **弹窗确认**：AI 每次调用 `send_request` 或任一写工具，客户端会弹「MCP 风险操作确认」——显示工具、来源、
  目标项目、风险类型（网络外发 / 落盘写）和**脱敏后的入参摘要**（headers/params 只列键名、body 只给类型、
  cURL 整段省略、疑似密钥一律掩码）。三个选项：**拒绝 / 允许 15 分钟 / 始终允许**。
- **60 秒不点 = 自动拒绝**（fail-closed），AI 会收到「等待确认超时」的工具错误。
- **允许 15 分钟**：同一会话里该工具（对该项目）15 分钟内不再问。**始终允许**：落盘持久授权（按 工具+项目，
  支持「全部风险工具」「任意项目」通配），在设置 MCP 分区「始终允许的授权」里可查看、逐条移除或清空。
- **开关**：设置 MCP 分区「风险操作前要求确认」默认开；关闭后退回纯静态白名单（等同于独立进程的行为），请谨慎。
- **调用审计**：每次工具调用记一条（成功/业务错误/被拒绝/内部异常，含工具、项目、授权结论、耗时与入参摘要），
  落在 `<用户配置目录>/api-doc-client/audit.log.jsonl`（独立进程同路径各写各的）。设置 MCP 分区的「调用审计」面板
  可按工具/项目/风险/结果过滤查询，支持**导出**与**清空**（清空前自动归档）。排查「AI 到底动过什么」先看这里。

## 七、输出裁剪（保护 AI 上下文）

| 内容 | 默认 | 怎么调 |
|---|---|---|
| 发送响应体 | 裁到 **8192 字节** | `maxBodyBytes`；`includeBody=false` 则完全不返回体 |
| 列表条数 | **50** | `limit`（上限 200） |
| 详情里的示例响应体 | 不返回 | `get_request_detail(includeBody=true, maxBodyBytes=…)` |

被裁剪时返回文本会**明确写**「已截断：共 N 字节，本次仅返回 M 字节」，不会静默丢弃。

## 八、跨主机与 WSL

AI 工具跑在 WSL2、集合放在 Windows（或反过来）时，**不要**在 WSL 里直接跑 mcpserver 读 `/mnt/c/...`
（原因见 8.4），而是 Windows 侧跑服务、WSL 侧走 HTTP。

### 8.1 两步启动

```bash
# Windows 侧：绑 0.0.0.0 并显式给令牌（不给会自动生成一个并打印在 stderr，重启即换）
mcpserver.exe -allow-writable D:\collections -http 0.0.0.0:8189 -http-token my-secret-token
```

### 8.2 WSL 侧连哪个地址

| WSL 网络模式 | 怎么判断 | WSL 里填的地址 |
|---|---|---|
| **mirrored**（推荐，Win11 22H2+ / WSL 2.0+ 默认） | WSL 与 Windows 同一网段 | `http://127.0.0.1:8189/mcp` ✅ 直接通 |
| **NAT**（老模式） | WSL 在 `172.x` 虚拟网段 | `http://<宿主IP>:8189/mcp`，宿主 IP 用 `ip route show default` 的网关地址 |

> ⚠️ mirrored 模式下**别用 `ip route show default` 的结果** —— 那拿到的是路由器地址（实测本机是
> `192.168.31.1`），不是 Windows 宿主，连不上。

### 8.2.1 白名单路径重复段

现象：项目路径出现 `…\dns项目\dns项目`（同一个名字套两层）。
这是**授权目录配置**问题，不是工具缺陷：白名单指到项目目录本身，同时项目目录里还有同名子目录/清单时，
拼接路径就会重复。把 `-allow-writable` / 设置里的白名单改到**上一级**（如 `…\lx_dns`）即可。

先探通再谈业务：`curl http://127.0.0.1:8189/healthz` → `{"ok":true,...}`（不需要令牌）。

### 8.3 客户端配置与自检

```json
{
  "mcpServers": {
    "api-doc-go": {
      "url": "http://127.0.0.1:8189/mcp",
      "headers": { "Authorization": "Bearer my-secret-token" }
    }
  }
}
```

命令行自检（完整握手 + 调一次工具）：

```bash
T=my-secret-token
H1='Content-Type: application/json'; H2='Accept: application/json, text/event-stream'
SID=$(curl -s -D - -o /dev/null -X POST http://127.0.0.1:8189/mcp -H "$H1" -H "$H2" -H "Authorization: Bearer $T" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}' \
  | grep -i mcp-session-id | tr -d '\r' | cut -d' ' -f2)
curl -s -X POST http://127.0.0.1:8189/mcp -H "$H1" -H "$H2" -H "Authorization: Bearer $T" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
curl -s -X POST http://127.0.0.1:8189/mcp -H "$H1" -H "$H2" -H "Authorization: Bearer $T" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}'
```

`notifications/initialized` 不能省（协议要求初始化通知之后才能发其它请求）；每次握手返回新的 `Mcp-Session-Id`，
后续请求都要带上（正规客户端会自动处理）。

### 8.4 为什么不在 WSL 里直接跑服务读 `/mnt/c/...`

`/mnt/c`（drvfs/9p）**不产生可靠的 inotify 事件** → 外部改动监听与索引刷新基本失效，冲突保护要靠手动重读兜底，
跨文件系统的重命名/锁语义也更容易出怪问题（尤其 `.trash` 移动）。要本地跑就把集合放在 WSL 原生文件系统
（`~/collections`）并保证只有一个写者；否则用上面的 HTTP 方式。

## 九、排查

| 现象 | 原因 / 处理 |
|---|---|
| AI 说「找不到工具 / 服务器未连接」 | `command` 路径错、没重启 AI 工具、没传授权目录（`-allow`/`-allow-writable`/`-root` 全空会直接退出） |
| `list_projects` 空但目录明明在 | 该子目录**缺 `opencollection.yml`**；补好后 `refresh: true` 重扫 |
| 写工具报「只读授权」 | 该目录是 `-allow`（只读）授权的；写操作要在 `-allow-writable` / 内嵌服务勾选「可写」 |
| 提示「这是 Bruno 集合目录」 | Bruno 格式需先用客户端「导入」转换 |
| 提示「找不到项目」/「匹配到多个项目」 | `project` 支持 uid/名称/路径；歧义时用返回的候选（推荐 `path`） |
| `search_requests` 少了某些项目 | 不带 `project` 只搜**已加载**项目；先 `get_project_modules` 打开它 |
| 保存被拒「文件已被外部修改」 | 读取之后文件被改过。重新 `get_request_detail` 合并后重试或另存新请求。**偶发误报**：文件监听重建索引有几百毫秒延迟，重新读一次即可 |
| 发送失败 | 地址不通/超时（默认 30s，受全局 `timeoutSec` 约束）/证书问题（`insecureSsl`） |
| 写操作提示 `unknown tool` | 用了 `-readonly`，写工具没注册 |
| 「用户拒绝了该操作」/「等待确认超时（60 秒未响应）」 | 内嵌服务的风险操作弹窗被点了拒绝或没人点（见「六」）；批量操作可选「允许 15 分钟」或「始终允许」 |
| 「无头严格模式禁止风险操作」 | 独立进程加了 `-require-consent`；去掉该参数，或改在客户端内嵌服务下用（有 UI 可确认） |
| HTTP 模式起不来 | 端口被占（换端口）；路径固定 `/mcp` |
| 项目统计是 0 | 懒加载：访问一次该项目后才有统计 |
| `环境 "dev" 已存在` | 新集合自动带 `dev` 环境；换名或直接用 |
| `变量名 "1bad" 不合法` | 变量名须以字母/下划线开头，只含字母数字下划线 |
| `环境 "DEV" 已存在（磁盘上实际是 "dev"）` | 环境名判重大小写不敏感（Windows 上同一个文件） |
| `新环境名 "DEV" 与原环境 "dev" …同一个文件` | 不允许只改大小写改名，换个拼写 |
| `[conflict] 环境 … 的文件已变化` | 重新 `list_envs` 合并后带新 `ifMatch` 再写；或去掉 `ifMatch` 强制覆盖（会丢对方改动） |
| 变量「配了但没生效」 | 看输出里的「已停用」：`enabled: false` 不参与 `{{name}}` 解析 |
| 401 unauthorized | 没带 `Authorization: Bearer <token>`，或令牌与服务端不一致（独立二进制重启不会自动换令牌，但换 `-http-token` 后要同步客户端） |
| 403 origin not allowed | 请求带 `Origin` 但不在 `-http-allow-origin` 列表（浏览器端客户端要显式允许） |
| WSL 连不上 Windows | 先 `curl .../healthz`：`000` = 地址/防火墙（mirrored 用 `127.0.0.1`）；有响应但 401 = 令牌不对 |
| `go build` 拉不到 go-sdk | `GOPROXY=https://goproxy.cn,direct go build …` |

## 十、安全清单

| 项 | 说明 |
|---|---|
| 能做什么 | 在**授权白名单**目录里的集合读写请求、建模块/项目、发请求、删请求（进 `.trash`） |
| 不会做什么 | 不碰白名单之外的路径（集合层拒绝绝对路径与 `..`）；不执行任意命令；不自动提交 git |
| 写保护 | `update_request` 支持 `ifMatch`；删除只进 `.trash`；环境写操作支持 `ifMatch` |
| 交互确认 | 客户端内嵌服务对 `send_request` 与全部写工具弹窗确认（拒绝 / 允许 15 分钟 / 始终允许，60s 超时按拒绝）；独立进程用 `-require-consent` 可切为一律拒绝 |
| 调用审计 | 每次工具调用落一条 `audit.log.jsonl`（含授权结论与脱敏入参摘要），设置 MCP 分区可查询/导出/清空 |
| 远程暴露 | 独立二进制监听非回环且不给令牌时**自动生成并打印**（重启即换，要稳定令牌请显式传）；内嵌服务启用且令牌为空时自动生成并**落盘**（重启不换）；每个请求都校验 Bearer；`/healthz` 免令牌且只回版本号 |
| 防火墙 | 首次启动弹窗选「专用网络」；或管理员 PowerShell：`New-NetFirewallRule -DisplayName mcpserver -Direction Inbound -LocalPort 8189 -Protocol TCP -Action Allow -Profile Private` |
| 暴露面 | 只在受信网络这么做；不要直接映射公网，需要就 `ssh -L 8189:127.0.0.1:8189 user@host` |
| 建议 | ① 对外机器人用 `-readonly` 或只读授权；② 让 AI 指向 **git 工作副本**，改动可 review/回滚；③ 响应示例会进版本库，注意敏感数据；④ 敏感值只回掩码，AI 上下文里拿不到真密钥 |

## 十一、与桌面客户端的关系

- 两者操作**同一份集合文件**：AI 建的请求/示例，客户端里立刻能看到（外部改动检测会提示冲突）。
- 客户端独有：UI 编辑、变量书签、cURL 导入面板、gRPC 定义导入与 Mock 面板、同步绑定面板。
- MCP 独有：跨项目模糊搜索、脚本化批量操作、给其它 AI 复用。
- 推荐分工：AI 负责查、批量整理、跑回归并存示例；人工在客户端精修与提交。

## 附：用客户端内置的 MCP 服务（不用单独跑 mcpserver.exe）

客户端内置同一套服务（适合「AI 与客户端同机、想一键开关」）。入口在**底部状态栏的 MCP 指示**
（运行中 / 未启用 / 出错，每 5 秒刷新），点击直接打开设置弹窗的「MCP 服务」分区。

| 字段 | 说明 |
|---|---|
| 启用内嵌 MCP 服务 | 总开关；关闭时不监听任何端口 |
| 监听地址 | `127.0.0.1（仅本机）` / `0.0.0.0（跨主机）` |
| 端口 | 默认 8189，可改（1024–65535） |
| 只读模式 | 默认开：AI 只能查询与试发 |
| 可访问的工作区 | 列出**客户端当前打开的工作目录**，逐条勾选授权 + 单独开关「可写」；一条都不勾 = 服务不监听（安全默认） |
| 浏览器来源 | Origin 白名单，逗号分隔；留空 = 拒绝一切浏览器来源 |
| 访问令牌 | 启用且为空时**自动生成并落盘**（重启/改设置不会变）；「重新生成」立即换新（旧令牌失效） |
| 风险操作前要求确认 | 默认开：`send_request` 与全部写工具弹窗确认（见「六」）；关闭 = 退回纯静态白名单 |
| 始终允许的授权 | 展示弹窗里点过「始终允许」的条目（工具 × 项目，含通配），可逐条移除或清空 |
| 调用审计 | 内嵌面板：按工具/项目/风险/结果过滤查询 + 导出 + 清空（清空前自动归档） |
| 状态行 | 运行中显示端点 URL 与可见项目数；失败显示原因（如端口被占用） |
| 一键复制示例 | 生成含地址与 Bearer 令牌的 `mcpServers` JSON 片段，粘贴进 AI 工具即可（跨主机做法与第八节相同） |

保存后立即生效（改监听地址/端口/令牌会重启服务）。

| | 客户端内嵌服务 | 独立 `mcpserver.exe` |
|---|---|---|
| 配置位置 | 状态栏 MCP 入口 → 设置 MCP 分区 | 命令行参数 |
| 工作目录 | 勾选客户端**当前打开的工作区**（可写开关逐条独立） | `-allow` / `-allow-writable`（`-root` 旧写法） |
| 生命周期 | 随客户端启停 | 独立进程 |
| 默认只读 | 开 | 关 |
| 风险工具确认 | **默认弹窗**（可关） | 默认不拦截（`-require-consent` 切为全拒绝） |
| 令牌 | 自动生成并**落盘**（稳定） | 非回环未给时自动生成**仅打印**（重启即换） |
| 多开 | 一个客户端一个实例 | 可多实例（注意端口冲突） |

配置存在 `<用户配置目录>/api-doc-client/config.json` 的 `mcp` 段（含 `allow` / `interactiveAuth` / `grants`）。
跨主机时记得把**只读模式关掉**才能让 AI 建/改接口。

## 十二、命令与参数速查

```bash
# 构建（首次需拉 SDK，注意代理）
GOPROXY=https://goproxy.cn,direct go build -o mcpserver.exe ./cmd/mcpserver

# 启动
mcpserver.exe -allow-writable D:\collections                        # stdio（AI 挂载，可写授权）
mcpserver.exe -allow D:\collections                                 # stdio（只读授权）
mcpserver.exe -allow-writable D:\collections -http 127.0.0.1:8189    # + HTTP /mcp
mcpserver.exe -allow-writable D:\collections -readonly               # 只读（不注册写工具）
mcpserver.exe -allow-writable D:\collections -require-consent        # 风险工具一律拒绝（无头严格）
mcpserver.exe -allow-writable D:\collections -http 0.0.0.0:8189 -http-token <令牌>   # 跨主机

# 调试：手工发 JSON-RPC 看服务是否活着（stdio 需保持管道打开）
printf '%s\n' \
 '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}' \
 '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
 '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
 | mcpserver.exe -allow-writable D:\collections
```

**默认约定回顾**：`send_request` 默认**不保存**示例（显式 `saveExample=true` 才留）、响应体裁剪 8192 字节、列表 50/200 条、敏感值只回掩码（掩码回传 = 不改，`.`/`?` 不算掩码）、
省略 `enabled` 即启用、写操作建议带 `ifMatch`、删除进 `.trash`、示例改名与新建同一套命名规则、图片只落本地 `<集合>/assets/`（正文引用 `/doc-assets/<sha256>`）、授权目录（`-allow-writable`/`-allow`）下含
`opencollection.yml` 的子目录 = 项目、`project` 三种写法（uid / 名称 / 路径）、
内嵌服务风险工具默认弹窗确认（拒绝 / 允许 15 分钟 / 始终允许，60s 超时按拒绝）、每次调用都写审计。
