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
