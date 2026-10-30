# MCP 工具审查与精简方案

- 审查对象：`internal/mcp`（39 个正式工具，含未提交的 `list_tools`、`import_collection`、`fileName`、入参校验错误中文转述）
- 审查方式：静态审查 + 编译/vet 验证
- 审查日期：2026-10-30

## 0. 验证状态

已完成（Go 1.27.2，由 vfox 管理）：

```bash
source ~/.vfox-env.sh            # 非交互 shell 必须先激活，原因见下
go build ./...                   # OK
go vet ./...                     # OK
bash scripts/check.sh --no-race  # 全绿：gofmt / share 版本 / build / vet / 前端 type-check + ESLint + i18n / 模型契约 / 单测 + 覆盖率
```

- 单测覆盖率：`internal/mcp` 82.8%（下限 65%）、`internal/collection` 77.7%（下限 70%），门禁全部通过。
- 本环境的命令走 `bash -c`（非交互、非登录），`BASH_ENV` 被 IDE 的 safe-delete 脚本占用，vfox 的 PATH 不会自动注入，因此 Go / npm 命令都需先 `source ~/.vfox-env.sh`。

## 1. 结论

新增能力（`import_collection`、`fileName`、`list_tools`、中文错误转述）有测试与 schema 快照，主体逻辑没有发现严重缺陷。但存在 7 处值得修的问题：1 个是「契约与描述不一致」，4 个是行为/可维护性缺陷，2 个是过期文案。

## 2. 问题清单

| 级别 | 位置 | 问题 | 建议 |
|---|---|---|---|
| 高 | `internal/mcp/tools.go:1150`、`:1166` | `rename_project` / `delete_project` 的 `project` 标了 `omitempty`，快照里 `required` 只有 `name` / `confirm`；但参数说明写的是「不能省略，避免改错」。省略时会经 `ts.proj()` 回落到本会话默认目标，`delete_project` 可能删掉默认项目 | 二选一：去掉 `omitempty` 让 `project` 真正必填；或改文案为「省略 = 本会话默认目标」，并同步快照 |
| 中 | `internal/mcp/tools.go:488` | 只读模式下 `mode=write` 的候选集必然为空，输出只有「没有命中的工具：换个关键词或去掉 filter」，未说明当前是只读模式，调用方会误判为没有写权限 | 在 `renderToolCatalog` 中，`ts.readOnly && mode=write` 时明确回「当前服务为只读模式，写工具未注册」 |
| 中 | `internal/mcp/service.go:945` | 用 `strings.Contains(err.Error(), "没有")` / `"版本"` 分类错误：任何含「没有权限」的 IO 错误都会被误判成 `[INVALID]` | 导入层返回哨兵错误（如 `ErrImportFormat` / `ErrImportContent`），用 `errors.Is` 分类 |
| 中 | `internal/mcp/errors.go:108` | `quotedList` 只 `Trim(p, "\"")`，不剥 `[` `]` `,`；多参数缺失时会残留 JSON 片段，例如 `缺少必填参数：["uid、name"]` | 用正则提取全部 `"..."` 或 `json.Unmarshal` 解析；补单参数/多参数/无参数名的表驱动单测（当前 `discover_test.go:121` 只覆盖单参数） |
| 低 | `internal/mcp/meta.go:8`、`:55`、`:113`、`:247`、`meta_test.go:40`、`tools.go:167` | 数量注释全线漂移，正确值见第 3 节。现写 36 / 13 / 17 / 「全模式 38、23 个落盘写、24 个 handler」 | 注释里不再写具体数字，改为指向 `ToolNames()` / `ValidateMetas()`；确需保留的按第 3 节更正 |
| 低 | `internal/collection/doc.go:225`、`request_create.go:83` | 显式 `fileName` 只走 `sanitizeFileName`，不走 `validEntryName`：`fileName: ".."` 会静默变成 `untitled`。无安全风险（路径分隔符与点已被清洗），但与 `name` 的校验口径不一致 | 显式 `fileName` 也走 `validEntryName`，不合法时报 `[INVALID]`，避免静默改名 |
| 低 | `internal/mcp/tools.go:1228` | `create_project.root` 的参数说明写「留空 = 服务启动参数 -root」，实现已改为「第一个可写工作目录」（`Service.CreateProject`，`meta.go` 的描述已是新版） | 更新参数说明，与实现对齐 |

## 3. 工具数量与上下文成本

### 3.1 计数（以 `meta.go` 实际内容为准）

| 分类 | 数量 | 说明 |
|---|---|---|
| `RiskLow`（只读/会话） | 14 | `list_tools`、`list_workspaces`、`use_workspace`、`list_projects`、`get_project_modules`、`get_request_detail(s)`、`search_requests`、`search_docs`、`list_envs`、`get_doc_template`、`list_docs`、`get_doc(s)` |
| `RiskMedium` | 1 | `send_request`（唯一网络外发） |
| `RiskHigh` | 24 | 集合写 19 + 环境写 5 |
| 合计 | 39 | 只读模式注册 15 个（14 low + `send_request`） |

集合写 19 个的构成：请求 5（create/import/update/delete/delete_requests）、容器 6（create/rename/delete 的 module 与 project）、文档 4（create/update/delete/save_image）、示例 3、空模块清理 1。

### 3.2 成本量化

| 指标 | 数值 | 口径 |
|---|---|---|
| 工具数 | 39 | `ToolNames()` 长度 |
| 描述文案 | 约 9.5k 字符 | `meta.go` 中 `Description` 合计 9507 字符 |
| 契约快照 | 31,148 字节 | `testdata/schema/*.json`，**已剔除 description** |
| 最大的两个 schema | `update_request` 7935B、`create_request` 7503B | 同上 |

真实 `tools/list` 还要叠加每个参数字段的中文说明（快照里不含），估算总量 60–70KB。结论是：**工具数量不是主要成本，描述与参数文案才是**。

另有一个自相矛盾点：`list_tools` 宣称「省上下文」，但 `catalog` 把完整 `Description` 原样返回（`tools.go:513-519`），调一次等于把 9.5k 字符重新塞回上下文。

## 4. 精简方案

### A. 零破坏（建议先做）

1. `ToolMeta` 增加 `Brief string`（≤30 字），`list_tools` 只输出 `Brief`。
2. 39 条 `Description` 从 9.5k 压到 ~3k 字符：只保留「做什么 + 何时用」，「为什么要这样」的说明挪到 `doc/mcp/` 手册。
3. 修第 2 节的高/中优先级问题。

不改任何工具名，预计让 `tools/list` 体积下降约四分之一到三分之一。

### B. 需要一次契约窗口（破坏性，建议并到 0.8/1.0）

| 合并动作 | 工具数变化 | 说明 |
|---|---|---|
| `get_request_detail` + `get_request_details` 合为一个（`uid` / `uids` 二选一） | -1 | 两者参数集几乎相同，纯往返优化 |
| `get_doc` + `get_docs` 同上 | -1 | 同上 |
| `prune_empty_modules` 并入 `delete_module`（加 `pruneEmpty` 参数） | -1 | 低频维护工具，独立价值低 |
| `get_doc_template` 改为 MCP resource/prompt（无参数静态文本） | -1 | 需客户端支持 resources；不支持时保留工具 |

39 → 35。

**不建议合并**：

- `delete_request` 与 `delete_requests`：前者支持 `confirmName`、后者要求每条带 `ifMatch`，删除语义不同，混批风险高于收益；
- `delete/rename_project` 与 `module`：风险语义与确认参数不同，合并会让授权弹窗说不清要改什么；
- 环境 5 个工具：对应「环境」与「变量」两类操作；
- `save_doc_image`、`import_collection`：各自能省几十次往返，删掉反而增加 token；
- `send_request`：唯一网络外发工具，必须独立授权。

### C. 不改契约也能降体积（推荐与 A 并行）

把现有 `--readonly` 扩展成**工具组开关**（集合 / 文档 / 环境 / 示例 / 网络）：用户不用的组不注册。这是唯一不破坏契约、又能真正缩小 `tools/list` 的路子，比合并工具更贴近实际使用场景。

## 5. 实施记录（2026-10-30）

| 项 | 状态 | 落点 |
|---|---|---|
| 问题 1：`rename/delete_project` 的 `project` 必填 | 已完成 | 去掉 `omitempty`；快照 `required` 已含 `project` |
| 问题 2：只读模式 `mode=write` 文案 | 已完成 | `renderToolCatalog` 增加只读分支；`TestReadOnlyListToolsWriteHint` |
| 问题 3：`import_collection` 错误分类 | 已完成 | 新增 `collection.ErrImportInvalid` 哨兵 + `errors.Is`；`app.ImportCollection` 分发同步包装 |
| 问题 4：`quotedList` 多参数 | 已完成 | 正则提取参数名；`errors_test.go` 覆盖单/多/无引号 |
| 问题 5：数量注释漂移 | 已完成 | 重写 `meta.go`，注释不再写死数量；`meta_test.go` 锁定 35 个基线 |
| 问题 6：显式 `fileName` 静默改名 | 已完成 | `validEntryName` 校验，不合法直接报错 |
| 问题 7：`create_project.root` 说明过期 | 已完成 | 参数说明改为「必须是授权且可写的工作目录」 |
| A 档：`Brief` + 描述瘦身 | 已完成 | 描述 9507 → 1713 字；`list_tools` 只回 `Brief`；预算测试上限 3200 |
| B 档：4 项合并 | 已完成 | 39 → 35；schema 快照、手册、设计文档同步 |
| C 档：工具组开关 | 已完成 | `config.ToolGroups` → `app.MCPRequest.Groups` → `meta.Group` 注册过滤；设置页 5 个组 + i18n；`TestToolGroupRegistration` |

### 第二轮：AI 易用性（同日完成）

| 项 | 状态 | 落点 |
|---|---|---|
| 详情「相关文档条目」缺 uid（断链） | 已完成 | `renderDetail` 文本补 `uid=`；docs 段补回写入口 |
| 文档按 `path` 定位 | 已完成 | `get_doc` / `update_doc` / `delete_doc` 的 `uid`/`path` 二选一；找不到列候选 |
| `search_docs` 拿不到 hash | 已完成 | 新增 `includeHash`（按项目批量取，不逐篇读盘） |
| 批量更新 | 已完成 | 新增 `update_requests`（1–10 条，单条失败不影响其它）；工具数 35 → 36 |
| 只能整表覆盖字段/参数/头 | 已完成 | `paramsUpsert` / `headersUpsert` / `bodyFieldsUpsert`（按名合并，与整表字段互斥） |
| 长文档只能整篇覆盖 | 已完成 | `update_doc.append` / `insertAfter`（anchor 必须唯一）+ `section=content|meta` 守卫 |
| AI 发现性与提示 | 已完成 | `list_tools` 常见任务速查 + 被关闭组提示；空字段表提醒 |
| 自测 | 已完成 | 8190 dev 端口起 `cmd/mcpserver`，26 项探针全部 PASS |

### 第三轮：入参与定位打磨（同日完成）

| 项 | 状态 | 落点 |
|---|---|---|
| `enabled` / `required` 被迫必填 | 已完成 | 新增 `KVInput` / `BodyFieldInput`（指针 + 默认 true/false），schema 的 required 不再含这两个字段 |
| `update_requests` schema 膨胀 | 已完成 | `items` 去掉批量场景低频的 `respDoc` / `auth` / `settings` |
| 批量写审计看不出改了哪几条 | 已完成 | `summarizeArgs` 对 `items` 输出 `{count, uids}`（不展开字段与值）；单测覆盖 |
| `insertAfter` anchor 必须唯一 | 已完成 | `InsertAfterOp.occurrence`（第 N 处）；不传仍要求唯一，防误插 |
| upsert 是整行替换 | 已完成 | 三个 upsert 改为**行内字段级合并**（只改说明不清类型/示例） |
| 请求只能按 uid 定位 | 已完成 | `get_request_detail` / `update_request` / `delete_request` 支持 `path`；`[NOT_FOUND]` 附请求/模块候选；批量失败带 `code` |

### 第四轮：批量、路径与静态内容（同日完成）

| 项 | 状态 | 落点 |
|---|---|---|
| 批量更新逐条重复 resolve | 已完成 | 共享一次 `resolveWrite` + 首尾各刷新一次索引 |
| `update_request` 顶层字段过多 | 已完成 | 三个 `*Upsert` 合并为 `patch{params,headers,bodyFields}`（字节数未降，见下） |
| `delete_requests` 缺确认与路径 | 已完成 | `confirm` 整批校验 + `items[].path` + `failed[].index/code` |
| 候选按索引顺序 | 已完成 | `rankNodes` 按前缀/包含排序 |
| `failed[]` 缺下标 | 已完成 | `index`（0 基） |
| 只能按子串锚定插入 | 已完成 | `insertAfter.anchorHeading`（按标题定位小节末尾） |
| 批量文档只认 uid | 已完成 | `get_doc.paths`（与 `uids` 二选一） |
| resource 只有模板 | 已完成 | 新增 `apidoc://error-codes`、`apidoc://collection-format` |
| 批量更新只认 uid | 已完成 | `update_requests.items[].path`（成功条目回填解析后的 uid） |
| 批量删除缺逐条确认 | 已完成 | `delete_requests.items[].confirmName`（不一致只拒绝该条） |
| `anchorHeading` 只做精确匹配 | 已完成 | `normalizeHeading`（全角空格/连续空格/闭合 `##`/尾部 `*`）+ 找不到时列文内标题 |
| `patch` 空数组被静默忽略 | 已完成 | 空数组 = 清空该表；部分行仍按名合并不丢行（`TestPatchEmptyArrayClears`） |
| 批量更新逐条全树扫描索引 | 已完成 | 写路径改用 `diskHashOf`；`get_request_detail` 保持索引 hash 口径 |
| 候选清单每次强制全树重建 | 已完成 | 去掉 `ReloadCollection`，索引由写路径与 fsnotify 维护 |
| resource 只有中文标题 | 已完成 | 三个静态 resource 标题/描述补英文副标题 |
| `tools/list` 体积无预算约束 | 已完成 | `TestToolsListBudget` 锁 70KB（当前 66.7KB）；字段说明压缩约 3,500 字 |

## 7. dev 端点 v5 测评反馈核实（2026-10-30）

| 外部反馈 | 结论 | 处理 |
|---|---|---|
| P-1 `section` 三套说法不一致 | 存在：`update_requests.items` 漏 `response`、错误提示漏 `docs` | 已统一为 `request / response / docs` |
| P-2 `section` 越界报 `[INTERNAL]` | 存在：两处 `fmt.Errorf` 未包 `withInvalid` | 已改 `[INVALID]`；`TestSectionErrorsAreInvalid` |
| P-3 3 个工具返回 `structuredContent` 却无 `outputSchema` | 部分存在：`get_doc` / `get_request_detail` 成功态确实如此（`requestDetailFullOutput` 已成死代码）；`use_workspace` 只在错误态有 sc（全局错误契约） | 前两个已挂 schema（双形态并集、required 只留 `project`）；`use_workspace` 成功态本就不返回 sc |
| P-4 `weakMatch` 恒为 null | 存在：`omitempty` 让 `false` 被省略 | 已去掉 `omitempty` |
| P-5 `list_tools.total` 不随过滤变化 | 设计如此（注释已写「当前模式注册总数」），但 schema 未说明 | 已给 `total` / `returned` 补 description |
| P-6 `method` 缺 `enum` | 存在，但 SDK 的 `jsonschema` tag 只作 description（且禁止 `WORD=` 前缀），加 enum 必须手写整个 InputSchema | 不修：服务端已校验并回 `[INVALID]`，描述已列白名单 |
| P-7 `get_doc_template` / `prune_empty_modules` 未恢复 | 设计取舍：前者迁移为 resource `apidoc://doc-template`，后者合并为 `delete_module(pruneEmpty=true)` | 不恢复（恢复会重新增大 `tools/list`） |
| `inputSchema` 增长 40% | 存在：`update_requests.items[]` 内联了完整请求字段 | 记录为后续项：用 `$defs`/`$ref` 需手写 schema，本轮不做 |

## 8. 低风险项收尾（2026-10-30）

| 项 | 状态 | 落点 |
|---|---|---|
| 错误分类漏网 15 处（此前落 `[INTERNAL]`） | 已完成 | `env.go` 4 处、`doc.go` 4 处、`service.go` 7 处；新增 `TestErrorCodesClassified`（6 个场景表驱动） |
| `update_response_example` 示例不存在误报 `[IO]` | 已完成 | 新增 `listExamples`：先 `ReadRequest`（NOT_FOUND）再列示例（IO） |
| `import_collection.text` 无上限 | 已完成 | `maxImportTextBytes = 10MB`，超限 `[LIMIT]`；`TestImportCollectionSizeLimit` |
| `Brief` / `Description` 两套文案无约束 | 已完成 | `TestToolBriefAndDescriptionBudget`（Brief ≤ 40 字且不重复、Description ≤ 120 字） |
| `send_request` 无地址限制（SSRF） | 未做 | 属行为变更，需先确认策略 |
| `method` 缺 `enum` | 未做 | SDK tag 不支持，需手写整份 InputSchema |
| `tools/list` 余量 1.9% | 观察 | 68,642 / 70,000；下次加字段前需先决定方案 |

> 实测：`update_request` schema 11,334 → 11,917 字节（`patch` 少了两层顶层字段，但 SDK 的
> schema 生成器不复用 `$defs`，`patch` 内部仍要声明三个数组）——结构更清晰，字节数没有下降。

## 6. 处理顺序

1. 修 `rename/delete_project` 的必填矛盾（唯一可能「按描述用就出错」的点）；
2. 修 `quotedList` 并补多参数单测；`ImportCollection` 改用哨兵错误分类；
3. 修只读模式的 `mode=write` 文案、`fileName` 校验口径、`create_project.root` 说明；
4. 清理数量注释（或改为不写数字）；
5. 做 A 档描述瘦身 + C 档工具组开关；
6. B 档合并留到下一次契约变更窗口。

## 6. 验收标准

- `bash scripts/check.sh` 全绿（含 `-race`）。
- `TestToolSchemaContract` 快照随契约变更显式更新，不静默漂移。
- 新增单测：`quotedList` 多参数、只读模式 `list_tools(mode=write)`、`import_collection` 错误分类。
- 工具数量与风险分类的断言集中在一个测试里，注释不再重复写数字。
