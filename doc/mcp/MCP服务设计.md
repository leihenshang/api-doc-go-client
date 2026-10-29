# MCP 服务设计（cmd/mcpserver + 客户端内嵌）

把 api-doc-go 集合（项目）暴露给外部 AI 工具（Claude Desktop / Cursor / Cline 等），
让 AI 能查接口、读详情、建请求、跑请求并留存响应。

> 使用者看《MCP服务使用手册.md》；本文只讲设计与取舍。

## 一、形态与启动

```bash
mcpserver -allow-writable D:\collections                              # stdio（本地 AI 工具挂载，默认）
mcpserver -allow D:\collections                                       # 只读授权（读工具可用，写工具拒绝）
mcpserver -allow-writable D:\collections -http 127.0.0.1:8189         # 追加 streamable HTTP（与 stdio 同时提供）
mcpserver -allow-writable D:\collections -readonly                    # 只读：不注册写工具，send_request 不落盘
mcpserver -allow-writable D:\collections -require-consent             # 无头严格模式：风险工具一律拒绝（fail-closed）
mcpserver -allow-writable D:\collections -http 0.0.0.0:8189 -http-token <令牌>   # 跨主机（WSL2 / 局域网）
```

| 参数 | 说明 |
|---|---|
| `-allow` / `-allow-writable` | 授权的工作目录（逗号分隔），每条区分只读/可写；其下**含 `opencollection.yml` 的子目录 = 一个项目**。三者全空时直接退出（打印 usage） |
| `-root` | **旧写法**：等价于对同一目录传 `-allow-writable`（保留兼容） |
| `-http` | 留空 = 只走 stdio；给了地址则同时提供 streamable HTTP（路径 `/mcp`）。只给端口（`:8189`）按**回环**处理，跨主机必须显式写 `0.0.0.0:8189` |
| `-http-token` | HTTP 的 Bearer 令牌。**监听非回环且未给时自动生成并打印到 stderr**（不落盘，重启即换）；客户端内嵌服务在启用且为空时自动生成并**落盘** |
| `-http-allow-origin` | 浏览器来源白名单（逗号分隔，`*` = 任意）。带 `Origin` 的请求必须命中，否则 403 |
| `-readonly` | 只注册只读工具（`InReadOnly` 的 11 个）；`send_request` 仍可用但不保存响应示例 |
| `-require-consent` | 无头严格模式（T2）：注入 `mcpauth.DenyAuthorizer`，`send_request` 与全部写工具**一律拒绝**（没有 UI 可应答授权弹窗，fail-closed）；只读工具不受影响 |

**stdio 授权器差异**：独立 mcpserver 默认**不注入**授权器（AI 自动化调用无人可点确认），风险拦截靠
`-readonly` / 白名单只读 / `-require-consent`；客户端内嵌服务默认对非 low 工具**弹窗确认**（见第七节）。

**stdout 纪律**：stdio 下 stdout 属于 JSON-RPC 协议，日志一律走 stderr。`-http` 模式下进程存活等信号，不等 stdio 返回。

**依赖**：官方 `github.com/modelcontextprotocol/go-sdk`。若 `proxy.golang.org` 不可达：
`GOPROXY=https://goproxy.cn,direct go get github.com/modelcontextprotocol/go-sdk@v1.8.0`。

Claude Desktop 配置：

```json
{ "mcpServers": { "api-doc-go": { "command": "D:\\path\\to\\mcpserver.exe", "args": ["-allow-writable", "D:\\collections"] } } }
```

### 跨主机（WSL2 / 局域网）

| 项 | 设计 | 理由 |
|---|---|---|
| 鉴权 | 校验 `Authorization: Bearer <token>`：前缀大小写不敏感 + `subtle.ConstantTimeCompare` 定长比较 | 监听全网卡 = 同网段任意进程可调工具（含删除），不设令牌等于把集合交出去 |
| 来源校验 | 带 `Origin` 的请求必须命中白名单（默认拒绝一切 Origin）；命中则回 CORS 头并处理 `OPTIONS` | MCP 规范的 DNS rebinding 防护；顺带让浏览器端客户端可用。无 `Origin` 的非浏览器客户端不受限 |
| 健康检查 | `/healthz` 恒开、不鉴权，只回 `{"ok":true,"service","version"}` | 先探通端口再谈鉴权，且不泄露项目/路径 |
| 超时 | 只设 `ReadHeaderTimeout` | SSE 是长连接，写超时会掐断正常会话 |
| WSL 连法 | mirrored（Win11 22H2+）用 `127.0.0.1`；NAT 用 `ip route show default` 的网关地址（mirrored 下拿到的是路由器） | 见手册第七节 |

## 二、项目模型

- **扫描范围 = 授权的工作目录白名单**（`Config.Allow`，每条区分只读/可写）：项目只能来自白名单目录自身
  或它的一级子目录；白名单外的集合既扫不到、也定位不到、更不能新建项目。只读授权下写工具一律拒绝
  （错误信息会说明「只读授权」），读工具照常可用。
- **白名单为空 = 不授权任何目录**（安全默认）：服务照常装配，`list_workspaces` 如实回报「没有可用项目」。
  内嵌服务在白名单为空时**不监听端口**（设置页给出「尚未授权任何工作目录」提示）。
- **默认工作目录**：`use_workspace` 选定的目标会作为后续工具省略 `project` 时的兜底，且是**会话级**的 ——
  HTTP 传输下每个客户端一个会话（各自一个 `Mcp-Session-Id`），各选各的、互不影响；stdio 与内嵌服务只有一个会话，
  等价于「进程里只有一份」。实现上由工具层按会话 id 保存，`Service` 本身不持有默认目标（保持无状态、可单测）。
- 一个项目 = 一个集合目录（含 `opencollection.yml`）。
- 三种定位方式（优先级从高到低）：**uid → 目录基名 → 路径**；歧义时报错并列候选。入参统一叫 `project`。
- **扫描只读清单**：`list_projects` 只解析 manifest（`collection.ReadMeta`），不建索引、不写盘、不起监听。
- **懒加载**：首次访问某项目才 `OpenCollection` 并起该项目的 fsnotify 监听；`refresh=true` 重新扫描。
- 读操作前 `ReloadCollection`（重扫 + 重建索引），因为扫描与索引是「打开时快照」。

## 三、工具清单（36 个）

> 约定：失败文本统一 `失败: [CODE] 说明`；列表/搜索工具与文档/接口写工具（get_doc / create_doc / update_doc / delete_doc / create_request / update_request / delete_request）另外返回 `structuredContent`（声明 `outputSchema`，数组元素写明具体字段与 required）；文档写工具支持 `dryRun`；容器删除进 `.trash`。

名称/描述/风险级别/读写标记的**单一事实源是 `meta.go`**（T1）：`Risk`（low/medium/high）、`Write`（落盘）、
`NetEgress`（真实外发）、`EnvWrite`（触碰 `*.secrets.yml`）、`InReadOnly`（只读模式仍注册）。
`ValidateMetas` 锁定「正式工具恒 36 个 + 风险标记自洽」，`assertRegistered` 在装配期双向核对「注册集合 == meta 表在该模式下应有的集合」。
风险分级是 T2 授权与 T3 审计的判定依据：**low = 10 个只读工具；medium = `send_request`（网络外发）；high = 16 个落盘写工具**（请求 5 + 文档 3 + 响应示例 3 + 环境 5）。

| 工具 | 风险 | 作用 | 关键入参 |
|---|---|---|---|
| `list_workspaces` | low | 授权的工作目录 + 其中项目 + 当前默认目标 | — |
| `use_workspace` | low | 选定**本会话**的默认工作目录（选定后可省略 `project`） | `workspace` |
| `list_projects` | low | 项目列表（模糊过滤/刷新） | `project?`、`refresh?` |
| `get_project_modules` | low | 模块结构 + 每模块请求数 | `project?`、`query?`、`includeRequests?` |
| `get_request_detail` | low | 接口详情（请求/headers/docs/respDoc/示例）；`docs` 段可分段/大纲 | `project?`、`uid`、`query?`、`includeBody?`、`maxBodyBytes?`、`sections?`、`docsStartLine?`、`docsMaxLines?`、`docsOutline?` |
| `search_requests` | low | 搜索（可跨项目，支持整词/字段限定/片段） | `query`、`project?`、`limit?`、`exactMatch?`、`fields?`、`includeHash?` |
| `get_doc_template` | low | 接口说明（docs）的 Markdown 分节结构契约（对齐前端 buildApiDoc） | — |
| `list_docs` | low | 文档条目清单（名称/路径/字节数/hash；默认带 hash） | `project?`、`query?`、`limit?`、`offset?`、`includeHash?` |
| `get_doc` | low | 读文档正文（Markdown 原文 + hash，供 `update_doc.ifMatch`）；支持 `outline` 大纲与 `startLine`/`maxLines` 分段 | `project?`、`uid`、`startLine?`、`maxLines?`、`outline?` |
| `list_envs` | low | 环境与变量（敏感值掩码 + hash） | `project?`、`query?` |
| `send_request` | medium | 发送并可选保存示例（唯一网络外发工具，只读模式也注册） | `project?`、`uid` 或 `method`+`url`、`env?`、`saveExample?`、`includeBody?`、`maxBodyBytes?` |
| `create_request` | high | 创建请求（手填 / cURL / 复制；可带完整 body/settings/respDoc） | `project?`、`name`、`folder?` + (`method`+`url`+…) / `curl` / `copyFromUid` |
| `create_module` | high | 创建模块（支持嵌套） | `project?`、`name`、`parent?` |
| `create_project` | high | 新建项目（目录 + manifest，只能在白名单内的可写目录里建） | `name`、`dirName?`、`root?`（留空 = 第一个可写目录） |
| `update_request` | high | 局部修改（含 params/settings/respDoc）/ 移动模块 | `project?`、`uid`、各可选字段、`ifMatch?` |
| `delete_request` | high | 移除请求（进 `.trash`） | `project?`、`uid` |
| `create_doc` | high | 新建文档；冲突默认报错（可 suffix/overwrite），支持 uid 幂等 | `project?`、`name`、`folder?`、`content?`、`uid?`、`onConflict?` |
| `update_doc` | high | 改正文 / 改名 / 移动分组（局部更新） | `project?`、`uid`、`content?`、`name?`、`folder?`、`ifMatch?` |
| `delete_doc` | high | 移除文档（进 `.trash`） | `project?`、`uid` |
| `save_doc_image` | high | 保存图片（base64，PNG/JPEG/GIF/WebP，≤5MB，内容寻址去重）到项目 `assets/`，返回 `/doc-assets/<sha256>` | `project?`、`data`、`name?` |
| `get_docs` | low | 批量读文档（1–10，按 uid 分组） | `project?`、`uids`、`startLine?`、`maxLines?`、`outline?` |
| `rename_module` | high | 改模块显示名（目录/uid 不变） | `project?`、`path`、`name` |
| `delete_module` | high | 删除模块（进 `.trash`，可递归） | `project?`、`path`、`recursive?` |
| `rename_project` | high | 改项目显示名（清单 info.name） | `project`、`name` |
| `delete_project` | high | 删除项目（confirm 二次确认，目录移入授权根 `.trash`） | `project`、`confirm` |
| `prune_empty_modules` | high | 清理空模块目录 | `project?` |
| `save_response_example` | high | 手工存一条响应示例（不发送请求；contentType 空则按 body 推断） | `project?`、`uid`、`name?`、`status?`、`headers?`、`body?`、`contentType?` |
| `update_response_example` | high | 局部更新示例（改名同步改文件名与 path；body 变则 size 跟着变） | `project?`、`uid`、`exampleUid`、`name?`、`status?`、`headers?`、`body?`、`contentType?` |
| `delete_response_example` | high | 删除示例（进 `.trash`，不动请求本身） | `project?`、`uid`、`exampleUid` |
| `create_env` | high | 新建环境（可带初始变量） | `project?`、`name`、`vars?` |
| `rename_env` | high | 环境改名 | `project?`、`name`、`newName`、`ifMatch?` |
| `delete_env` | high | 删除环境 | `project?`、`name` |
| `set_env_var` | high | 单条变量 upsert | `project?`、`env`、`name`、`value`、`secret?`、`enabled?`、`ifMatch?` |
| `delete_env_var` | high | 删一个变量 | `project?`、`env`、`name`、`ifMatch?` |

**局部更新的实现要点（`body` / `respDoc` 指针化）**：MCP 入参侧另立 `BodyInput` / `RespDocInput`
（`internal/mcp/service.go`），字段全为指针 + `omitempty`：

- `collection.Body.Form` 的 json tag 没有 `omitempty`，直接把它放进工具入参会让 `type`/`raw`/`form` 一起变成
  schema 里的 `required` —— AI 想只改 `raw` 也得编造 `form`，漏了就清空表单行。指针化后 schema 里 `body` 无必填子项。
- 合并语义：**nil = 保留原值；显式零值（`""`/`0`/`[]`/`{}`）= 清空**。旧的 `mergeRespDoc` 把空串/0 当「没传」，
  所以样例与状态码永远清不掉；指针让「清空」成为可表达的意图。
- 落盘/同步格式**完全不变**：`BodyInput` / `RespDocInput` 只活在入参侧，合并完仍是 share 的 `collection.Body` /
  `collection.ResponseDoc`，因此 share 契约（磁盘格式、同步字段）零改动。
- `create_request` 的 `bodyType`/`bodyRaw` 快捷字段改为只覆盖 `draft.Body.Type` / `.Raw`（不再整份替换 `Body`），
  与 `body` 子项合并、`bodyFields` 三者互不影响。
- `update_request` 的 `section` 节点守卫（`validateSection`）在**读请求之前**校验：`request`（请求本体 11 字段）/
  `response`（仅 `respDoc`）/ `docs`（仅 `docs`）/ 省略 = 不限制。越界时中文报错，列出越界字段名与允许清单。

`respDoc` 在 `create_request`/`update_request` 均为**按子项合并**：可只写 `params` 字段表补说明，
样例/响应头/状态码缺省保留原值，显式空值可清空（gRPC 请求一律拒绝 respDoc）。

**响应示例的增/改/删**（`internal/app/app_misc.go`）：`SaveResponseExampleRaw` 用请求落盘快照 + 传入的
status/headers/body/contentType 直接落盘；`UpdateResponseExample` 按 UID 找到示例 → 改名时按**与新建完全相同的口径**
（`collection.ValidEntryName` 先校验 → `collection.SanitizeFileName` 规范化，显示名与文件名同值）把 `Path` 换成同目录
`<规范化名>.yml` → 改响应字段 → `collection.UpsertResponseExampleFile`（重名自动加 `(1)`）；
删除走 `collection.DeleteResponseExample`（进 `.trash`）。

三条一致性约束（评审后加固，均有回归测试）：

1. **示例 uid 必须出现在 `get_request_detail` 的文本里**（`render.go` 的示例行带 `uid=`）：
   update/delete 的 `exampleUid` 只能从这里拿，工具结果没有结构化字段可回引。
2. **`UpsertResponseExampleFile` 一律「先写新文件成功、再删旧落点」**：反过来一旦写入失败
   （名字过长 / 目录不可写 / 磁盘满），示例会整条消失 —— 静默数据丢失。
3. **名字换算只有一个入口**：`collection.SanitizeFileName` / `collection.ValidEntryName`（app 层自带的
   `sanitizeFileName` 仅用于运行报告导出）。另外按**字节**兜一道基名上限（`checkExampleName`，240 字节；
   share 的 sanitize 按 rune 截断到 100，中文即 300 字节，仍会触发 ENAMETOOLONG，故提前报「示例名过长」）。
4. **长度上限只管「落点变化」**：`UpsertResponseExampleFile` 只在新建/改名/换目录时校验；
   同一 UID 同一路径的原地重写一律放行 —— 否则历史遗留（本版本之前、或外部同步来的）超长名示例
   连「只改状态码」都做不到。
5. **回收站不做长度赌博**：`moveToTrash` 用 `trashSafeBase` 把基名截成「头部 + ~短哈希 + 原扩展名」
   （预算 240 字节，时间戳前缀 16 字节），保证超长条目也删得掉；`ENAMETOOLONG`（以及部分文件系统
   对超长名返回的 EPERM）单独给「文件名过长，请先改名再删除」文案，不再混进「文件可能被其他程序占用」。

## 四、模糊匹配（`fuzzy.go`，纯标准库）

两阶段：

1. **索引阶段**（不读文件）：对 `name`/`url`/`method`/`path` 打分。归一化（全角→半角、大小写折叠、去空白与 `/ . _ - ? & =`）；命中方式：全串子串 100 分 > 分词命中（按比例）30 分 > 有序子序列 8 分；再乘字段权重（name 10 / url 6 / method 4 / path 3…）。中文按普通子串处理，不引分词/拼音依赖。
2. **深读阶段**：索引无命中（query 可能命中 header/docs/body）时，读前 60 个候选补齐这三个字段重打分（只升不降）。

结果带 `score` 与 `matched`，同分按名称升序（结果稳定可复现）。

## 五、输出裁剪与错误

- 响应体默认 **8 KiB**（`maxBodyBytes`），超限明确写「已截断：共 N 字节，本次仅返回 M 字节」，**不静默丢弃**。
- 列表默认 50 条（`limit` 上限 200）。
- 工具结果统一是**可读文本**（不进结构化 JSON）：更省 token、更好读。
- 业务错误用 `isError: true` 返回（AI 能读到并纠正），不是协议级 error。

## 六、环境变量管理的取舍

落盘复用 collection 层（`environments/<env>.yml`；敏感值拆到 `<env>.secrets.yml` 并收权 0600；删除进 `.trash`），工具层只做门面。

| 决定 | 做法 | 理由 |
|---|---|---|
| 敏感值永不出门 | 读一律替换成 `••••••`（掩码取自共享包 `varx.MaskedValue`） | AI 上下文会进日志/第三方模型，真密钥没必要给 AI 看 |
| 掩码回传 = 不改 | 值整串由 `• ● · *` 构成（`isMaskish`）时保留原密钥并说明；**新建敏感变量传掩码直接报错** | 原样回写会把占位符存成密钥（实测废过一次密钥）；新建没有「旧值」可保留，只能报错。刻意不收 `.` 与 `?`：它们是真实密码字符，收进来会让「把密钥改成 `...`」被静默忽略 |
| 空值清空改为报错 | 对已存在的敏感变量传 `value: ""` 报错，给出三条出路（回传掩码 / 传新值 / `secret:false` 转普通变量） | 静默清空密钥是最坏的「帮倒忙」 |
| `enabled` 用 `*bool` | `create_env.vars[].enabled` 与 `set_env_var.enabled` 都是指针 | bool 零值是 false：AI 忘传就「建出来即停用」——列表看得见、`{{name}}` 解析不到，极难排查 |
| 名字规则复用共享包 | 环境名用 `share.ValidEnvName`；变量名 `[A-Za-z_][A-Za-z0-9_]*` | 不建「永远无法被 `{{name}}` 引用」的变量，并排除路径分隔符 |
| 环境名判重大小写不敏感 | `indexOfEnv` 用 `strings.EqualFold` | 环境名即文件名，Windows/macOS 不区分大小写：文本比较与文件系统语义不一致 = 静默覆盖并销毁密钥 |
| 拒绝只改大小写的改名 | `rename_env("dev"→"DEV")` 报错；`Collection.RenameEnv` 用 `EqualFold(oldMain,newMain)` 兜底 | 改名是「写新名 + 删旧名」，同一文件等于先写回再自删 → 环境凭空消失 |
| 改名下沉到 collection 层 | `Collection.RenameEnv(old,new,expectHash)` 一步完成「读旧 → 校验旧名哈希 → 写新 → 旧文件进 .trash」 | 并发校验必须落在**旧名**文件上；服务层拼 `SaveEnv(新)+DeleteEnv(旧)` 时新文件不存在 → 哈希为空 → 校验被静默跳过 |
| 写操作支持 ifMatch | `list_envs` 每环境返回 hash（前 12 位，git 风格前缀比对）；三个写工具收 `ifMatch`，不传 = 最后写者赢 | 环境是「读整份 → 改一处 → 整份写回」，同一文件有界面与其它 AI 会话两个写方 |
| 删除先查存在性 | `delete_env` / `delete_env_var` 对不存在对象报错 | collection 层删除是幂等静默的，但对 AI「删掉了」必须为真 |
| 默认环境 = 排序第一个 | 输出里标注 | 与客户端行为一致（前端也取 `envNames[0]`），AI 不必猜 |

## 七、交互式授权与调用审计（T2 / T3）

**guard 装饰链**：所有工具在注册处被 `runGuarded` 统一包住（36 个业务 handler 零改动）——
`recover（panic 转工具级错误）→ 风险授权（非 low 才走）→ 业务 → 收尾恰好一条审计`。
拒绝/异常一律 `isError: true` 的工具级错误，AI 能读到并纠正，不是协议级 error。

**授权链（T2，`internal/mcpauth` 纯逻辑叶子包）**：`NewChain` 组装「持久授权表 → 15 分钟缓存 → UI 桥」三级：

| 环节 | 行为 | 理由 |
|---|---|---|
| 触发条件 | meta 表 `Risk != low`（`send_request` + 10 个写工具）才进授权链；low 短路放行 | 只读查询不打扰用户 |
| 持久表 | 命中 `(tool, project)`（支持 `tool="*"` / `project=""` 通配）即放行 `grantedBy=persisted`，跨进程重启存活 | 「始终允许」一次选择长期生效 |
| 15 分钟缓存 | 按 **(会话, 工具, 项目)** 缓存 `allow_15m`；`allow_always` 也顺手写入减少 store 争用 | 连续批量操作不反复弹窗；会话维度避免 A 的选择放行 B |
| UI 桥（App） | `emitAppEvent("mcp:authz-request")` 弹窗 → 前端 `AnswerMCPAuthz(requestID, decision)`；**60s 超时按拒绝**（fail-closed）；同 `(session,tool,project)` 并发调用**共享同一弹窗**，一次决策全部生效 | 不弹两个重复窗、AI 断连不会挂着弹窗悬等 |
| 决策 | `拒绝 / 允许 15 分钟 / 始终允许`（always 立即落盘 `config.json mcp.grants`；写盘失败降级为本次 15 分钟放行） | 「始终允许」失败也不该阻塞用户已同意的操作 |
| 入参脱敏 | 弹窗只展示 `summarizeArgs` 白名单摘要：headers/params 只列**键名**、body 只给类型、`curl` 整段丢弃、疑似敏感键与 `secret=true` 的 value 一律掩码 | 原始入参可能内嵌真令牌，不能送到屏幕/事件通道 |
| 无头严格模式 | `-require-consent` 注入 `DenyAuthorizer`：非 low 一律拒绝（`reason=headless-strict`） | 没有 UI 应答时 fail-closed，不静默放行 |
| 开关 | 内嵌服务 `mcp.interactiveAuth`（默认开；关闭 = 不注入授权器，退回纯静态白名单） | 明确告知用户「关闭后等同旧行为」的风险 |

**调用审计（T3，`internal/mcpaudit` 纯逻辑叶子包）**：

- 每次工具调用**恰好一条** `Event`（出口四分类全覆盖：`ok / biz_error / denied / panic`），字段含
  会话/传输/来源线索/工具/风险/项目/授权结论（`approved`、`grantedBy`、弹窗 `requestId` 对账）/脱敏入参摘要/结果摘要/耗时。
- 落盘 `<配置目录>/audit.log.jsonl`（0600，按 JSON Lines **异步追加写**；单文件 5 MiB 轮转、最多 3 份归档；内存环形缓冲 1000 条加速近期查询）。
- **fail-open**：审计写失败只记日志不阻断工具调用（审计是旁路，不能变成故障点）；授权是 fail-closed，两者方向相反是刻意的。
- stdio 独立进程同样落审计（main 装配时直接构造）；客户端设置 MCP 分区内嵌 `McpAuditPanel`：查询（工具/项目/风险/结果/关键字过滤）+ 导出 + 清空（清空先把当前文件归档再截断）。

## 八、安全与正确性

| 边界 | 行为 |
|---|---|
| 路径/uid | 全部走集合层（`..`/绝对路径拒绝、uid 校验、定义文件必须在集合内），不接受任意磁盘路径 |
| 删除 | 移入集合 `.trash/`，不真正删除 |
| 并发写 | 同项目写操作由 registry 锁串行化 |
| 冲突保护 | 详情/搜索返回 `hash`；`update_request` 传 `ifMatch` 时磁盘被外部改动就拒写（不传则最后写者赢，与桌面端一致） |
| 只读模式 | 不注册 `create_*`/`update_*`/`delete_*`；`send_request` 强制 `saveExample=false` |
| 风险工具确认 | 内嵌服务默认对 `send_request` 与全部写工具弹窗确认（T2，见第七节）；独立 mcpserver 默认不拦截，可用 `-require-consent` 切为全拒绝 |
| 工作目录边界 | 只扫 / 只操作**白名单**目录（路径比较按绝对路径 + 软链复核，拒绝 `..` 逃逸）；`create_project` 只能在白名单内的可写目录里建（以前 `root` 参数可以指向任意路径 = 越权） |
| 授权粒度 | 每条白名单独立只读/可写：只读授权下写工具直接拒绝，`send_request` 的示例也不落盘 |

## 十、客户端内嵌（状态栏 MCP 入口 → 设置 MCP 分区）

配置项 `config.MCPConfig`：`enabled` / `addr` / `port` / `token` / `readOnly` / `allowOrigins` /
`allow`（工作目录白名单，每条 `{path, writable}`）/ `interactiveAuth`（T2 开关，指针，**缺省 = 开**）/
`grants`（「始终允许」持久表），随设置一起落盘。

**入口**：MCP 已从设置左侧导航移出，改在**底部状态栏**显示运行态（运行中/未启用/出错），
点击直达设置弹窗的 MCP 分区（`initialSection='mcp'`，该分区全屏显示、隐藏左列导航）。

| 设计点 | 做法 | 理由 |
|---|---|---|
| 依赖方向 | `internal/mcp` 提供实现（`NewBackend`），`internal/app` 只持有 `MCPBackend` 接口，由 `main.go`/`devserver` 注入 | `internal/mcp` 需要 App 的集合运行时，反向 import 会成环；故抽象 `ProjectApp` 接口 + `NewAppFunc` 工厂 |
| 授权/审计注入 | App 在 `Startup` 组好授权链（`buildMCPAuthorizer`：开关关闭返回 nil = 不拦截）后经 `SetAuthorizer`/`SetAuditor` 注入 backend | mcpauth/mcpaudit 是无内部依赖的叶子包，app 与 mcp 两侧都只依赖它，不成环 |
| 生效时机 | `Startup` / `OpenCollection` / `SaveSettings` 都调 `applyMCP` | 换集合要换项目根；保存设置要重启服务 |
| 端口占用 | 先 `net.Listen` 探一下再 `ServeHTTP` | 把「端口被占用」变成同步错误返回设置页 |
| 令牌 | 启用且为空时自动生成 32 位随机令牌并**落盘**；「重新生成」先落盘再重启 | 开了就有鉴权；不落盘会导致切主题/重启后令牌静默轮换、已连上的客户端 401 |
| 默认值 | 不启用、回环、8189、**只读** | 内嵌服务最容易被随手暴露 |
| 工作目录授权 | 设置页直接**列出客户端当前已打开的工作区**，逐条勾选授权 + 可写开关 | 以前取「当前集合父目录」等于隐式授权一整棵目录树；勾选现成目录避免手输路径出错。白名单为空则服务不监听（安全默认） |
| 连接配置分享 | 「一键复制示例」生成含 URL 与 Bearer 令牌的 `mcpServers` JSON 片段 | 粘贴即用，免去手拼地址与令牌 |

## 十一、分层

```
cmd/mcpserver/main.go       flag（-allow/-allow-writable/-root/-http/-readonly/-require-consent…）+ 传输选择（stdio / HTTP）；
                            stdio 形态也装配 mcpaudit（落配置目录）与 -require-consent 的 DenyAuthorizer（T2/T3）
internal/mcp/server.go      装配：NewServer / ServeStdio / ServeHTTP（唯一 import SDK 的入口层；透传 Authorizer/Auditor/Transport）
internal/mcp/meta.go        36 个工具的元数据单一事实源（name/description/risk/write/netEgress/envWrite/InReadOnly，T1；ValidateMetas 自检）
internal/mcp/tools.go       36 个工具的注册（meta 驱动 + assertRegistered 双向核对）与 guard 装饰器 runGuarded（recover + 授权 T2 + 审计 T3）
internal/mcp/doctpl.go      get_doc_template 的 docs 结构契约文案（对齐前端 lib/genDocs.ts，变更需两侧同步）
internal/mcp/authz_args.go  授权弹窗的入参脱敏 summarizeArgs（白名单键 + 敏感值掩码，T2）
internal/mcp/service.go     语义实现（不 import SDK，可单测/复用）
internal/mcp/registry.go    多项目 registry（只读扫描、懒打开、三种定位、创建项目）
internal/mcp/allow.go       工作目录白名单（AllowDir 归一化 / 只读判定 / 越界拒绝）
internal/mcp/env.go         环境变量工具（掩码、hash、写保护）
internal/mcp/fuzzy.go       归一化 + 打分
internal/mcp/render.go      裁剪与文本渲染
internal/mcp/embed.go       客户端内嵌服务的启停实现（SetAuthorizer/SetAuditor 注入；白名单为空不监听）
internal/mcpauth/           T2 纯逻辑：决策/持久授权表/15 分钟缓存/授权链 NewChain/无头 Deny（不依赖 SDK/app）
internal/mcpaudit/          T3 纯逻辑：audit.log.jsonl 追加写/轮转/订阅扇出/查询（不依赖 SDK/app）
internal/app/mcp_authz.go   App 作为授权 UI 桥：弹窗事件/60s 超时/并发去重/始终允许落盘 + Grants 门面
internal/app/mcp_audit.go   App 审计门面：List/Export/Clear + mcp:audit 事件桥
```

## 十二、验证

- 单测：`go test ./internal/mcp/ ./internal/mcpauth/ ./internal/mcpaudit/`（打分、registry 扫描/定位/创建、
  环境变量读写与掩码回传、冲突分支、发送并保存示例、裁剪、工具注册与 schema、只读模式差异、meta 自检、
  授权链（持久/缓存/超时/并发去重/脱敏）、审计（落盘/轮转/查询/四出口）；授权与审计另有集成测试
  `authz_integration_test.go` / `audit_integration_test.go` 走真实 guard 链）。
- stdio 冒烟：管道喂 `initialize` / `tools/list` / `tools/call`，验证「建模块 → 建请求 → 查目录 → 模糊搜索」全链路。
- HTTP 冒烟：`initialize` 拿 `Mcp-Session-Id` → 会话内 `tools/call`；只读模式下调 `delete_request` 返回 `unknown tool`。
