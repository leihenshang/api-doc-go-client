# 客户端 gRPC 测试功能设计（api-doc-go-client）

> **用途**：定义客户端新增 gRPC 测试能力的完整功能清单与实施路线，作为排期、开发与验收依据。
> **范围**：仅桌面客户端 `api-doc-go-client`（Go 内核 + Wails v2 + 自建 Vue3 UI）。服务端（`api-doc-go`）对 gRPC 接口的**展示 / 同步**不在本文范围（服务端数据模型已预留 `protocol_type` 字段，见 §11）。
> **快照日期**：2026-10-02。状态以代码为准，「落点」列给的是改动位置（含行号为当时证据）。
> **维护方式**：功能点编号一旦分配不再复用；完成后把「状态」改为 ✅ 并补落点；新增点在所属分组末尾追加编号。
> **相关文档**：`doc/客户端功能规划与完成情况.md`（总体功能清单）、`doc/客户端问题记录.md`（问题与修复记录）。

---

## 0. 决策与约束（已拍板）

| # | 决策 | 说明 |
|---|---|---|
| **D1** | **schema 以本地 `.proto` 文件为唯一真相源** | 不引入 protoc / 代码生成；Go 侧纯 Go 编译（`bufbuild/protocompile`），跨平台、可离线 |
| **D2** | **导入默认复制进集合** `protos/` 目录 | 集合自包含、可 git 提交、跨机一致（Postman 式）；「引用外部绝对路径」作为高级选项，UI 标注「不可共享」 |
| **D3** | **不监听 proto 变更** | 不做文件监听、不做热更新、不提示「定义已变化」；**更新只由用户手动「重新导入 / 更新定义」触发**。因此必须让「当前用的是哪份定义、什么时候导入的」在界面上可见（G2.4） |
| **D4** | **解析失败即提示失败，不允许保存** | ① 导入动作原子化：解析失败 → 不复制、不落盘、不更新定义；② 已存在的 gRPC 请求在定义解析失败期间**禁止保存与发送**（唯一例外见 §5：允许改「定义路径 / import」这条修复通道，避免把用户锁死） |
| **D5** | 服务地址与定义解耦 | 同一份 proto 可指向多套环境（`{{host}}` 变量），地址变更不触发重新解析 |
| **D6** | 首期只做 unary | 流式（server / client / bidi）、mTLS、反射、代码生成按 §7 阶段推进 |

**明确不做**（避免范围蔓延）：

| 不做 | 原因 |
|---|---|
| proto 文件监听 / 自动重新解析 | D3 |
| proto 语法编辑（内置编辑器） | 定义文件由用户仓库管理，应用只解析只读 |
| 由 proto 生成 Go/TS stub 代码 | D1；动态调用（`dynamicpb`）已足够 |
| 反射优先的「零文件」体验 | 首期以 .proto 为准；反射作为后续可选增强（P7） |
| 服务端 gRPC 接口文档展示 / 同步 | 另一议题，服务端已有 `protocol_type` 预留 |
| 服务端流式压测 / 负载测试 | 与「接口调试」定位无关 |

---

## 1. 目标用户流程

```
① 新建「gRPC 请求」（未落盘草稿，关闭时才提示保存）
      ↓
② Schema 分段：导入 .proto（文件选择器，可多选）+ import 目录（可多选）
      ↓ 解析成功 → 复制进集合 protos/ → 列出服务/方法
      ↓ 解析失败 → 直接提示（文件 + 行列），不落盘，可重选
③ 请求栏输入服务地址 host:port（支持 {{host}} 等变量）
      ↓
④ 选择服务 / 方法（下拉可搜索；自动带出 unary / server-stream / client-stream / bidi）
      ↓
⑤ Message：JSON 编辑器（可「按 proto 生成样例」）
      ↓
⑥ Metadata（KV 表，值支持变量）
      ↓
⑦ 发送 → 响应：状态码（OK / NOT_FOUND(5)…）+ 耗时 + 大小
                  + 初始元数据 + 尾元数据 + 响应消息（JSON 树）
```

**界面分区（右侧响应区为现有布局，不改）**

```
┌ 请求栏：［服务·方法 ▾］［ host:port ］［TLS］   ［ 发送 ］──────────────┐
├ 编辑器分段：Message │ Metadata │ Schema │ Options │ Docs ─────────────┤
│  Message = JSON 消息编辑（生成样例 / 校验提示）                        │
│  Schema  = 定义来源（文件 + import + 导入时间 + 服务方法列表 + 错误）    │
│  Options = 截止时间 / 压缩 / 流式模式 / TLS 细项                       │
└───────────────────────────────────────────────────────────────────────┘
```

---

## 2. 数据模型

### 2.1 磁盘形态（请求文件）

复用现有「一请求一文件」结构，新增独立 `grpc:` 块（HTTP 请求不受影响）：

```yaml
# api/SayHello.yml
info: {name: SayHello, type: grpc, seq: 4}
meta: {uid: 7b2f…, base_rev: 0}
grpc:
    target: '{{host}}'                     # 服务地址 host:port，支持变量（D5）
    service: demo.Greeter
    method: SayHello
    proto: protos/greeter.proto            # 相对集合目录（D2；集合外路径时提示不可共享）
    imports: [protos]                      # import 搜索路径，相对集合目录
    metadata:
        - {name: authorization, value: 'Bearer {{token}}', enabled: true}
    message: |
        {"name": "world"}
    stream: unary                          # 默认由方法签名推断，可覆盖
    tls: {mode: none, ca: '', cert: '', key: '', insecureSkipVerify: false}
    settings: {timeoutSec: 30}             # 请求级覆盖全局设置（沿用现有 settings 语义）
docs: ""
```

### 2.2 磁盘形态（集合内约定）

```
<集合目录>/
├─ opencollection.yml            # 集合清单（本期不改，集合级默认定义留待 §7 P8）
├─ protos/                       # 导入的 .proto 落点（新增约定，常量 protosDir）
│   └─ greeter.proto
├─ api/…                         # 请求（type: http | grpc）
└─ docs/…                        # 文档（现有约定）
```

已确认无副作用：集合扫描器只读 `.yml/.yaml`（`internal/collection/collection.go:456`），`.proto` 不会被扫进树。

### 2.3 内存 / TS 契约

| 层 | 改动 |
|---|---|
| Go 内存模型 | `internal/collection/types.go` 的 `Request` 增加 `GRPC *GRPCBlock`（或 `GRPC GRPCBlock` + `Type` 字段），新增 `GRPCBlock{Target, Service, Method, Proto, Imports, Metadata, Message, Stream, TLS}` |
| 共享 schema | `api-doc-go-share/collection/request.go`：`RequestFile` 增加 `GRPC *GRPCBlock yaml:"grpc,omitempty"`；`HTTP` **保持值类型**，标签改 `http,omitempty` 并给 `HTTPBlock` 加 `IsZero()`（yaml.v3 认 `IsZeroer`，效果是 gRPC 文件不写空 `http:` 段，且不动服务端与既有读取方 —— 实现时对原计划的调整，见 §10 第 7 条）；`knownTopLevel` 增加 `grpc` |
| TS 契约 | `frontend/src/types.ts` 的 `RequestDoc` 增加 `grpc?: GrpcDoc`；新增 `GrpcDoc` / `GrpcTLS` / `GrpcMethod`（服务方法列表）/ `GrpcParseResult` 类型 |
| 响应 | `runner.Result` 增加 `Trailers []collection.KV`（尾元数据）与 `GRPCCode int`（或复用 `Status` 放 gRPC code，二选一，实施时定）；TS `SendResult` 同步 |

> **共享模块改动成本**：本机 `GOWORK=`（无 go.work），share 以 `v0.3.0` 依赖引入。开发期在 client 的 `go.mod` 加 `replace github.com/leihenshang/api-doc-go-share => ../api-doc-go-share`；联调通过后再发新版 share。

### 2.4 索引 / 历史 / 搜索 / 同步的适配

`internal/index`（`index.go:17-30`）、`internal/history`（`history.go:18-30`）、`internal/syncengine`（`engine.go:231-284`）都依赖 `Method` + `URL`。为免大改，约定：

| 字段 | gRPC 请求的取值 | 效果 |
|---|---|---|
| `Method` | `GRPC`（新增方法色，`frontend/src/lib/method.ts:4-13` 补一项） | 列表/标签可区分协议 |
| `URL` | `grpc://{target}/{service}/{method}`（由 target/service/method 合成，写盘时派生） | 搜索、历史、同步 payload 继续可用 |

**建议**：`index.Node` 与 `history.Entry` 增加 `Type` 列（`http`/`grpc`），避免"gRPC 目标被当 URL"造成语义混乱；历史回放/搜索命中逻辑不变。

---

## 3. 功能清单（总览）

| 分组 | 条目数 | 说明 |
|---|---|---|
| G1 新建与协议分派 | 4 | 入口、草稿、请求栏分派、方法选择器 |
| G2 定义管理（导入 / 更新 / 移除） | 7 | 复制进集合、多文件、import 路径、冲突处理、引用外部、移除、来源可见 |
| G3 解析与缓存 | 6 | 编译、缓存（显式 generation）、错误展示、失败禁止保存、发送门禁 |
| G4 服务与方法选择 | 5 | 列表、搜索、注释、流式类型、无方法时的空态 |
| G5 消息编辑 | 5 | JSON 编辑、生成样例、校验、字段提示、Well-Known Types |
| G6 元数据 | 3 | KV 表复用、变量替换、敏感值 |
| G7 连接与安全 | 5 | 地址、明文/TLS、证书、超时/截止、压缩 |
| G8 发送与响应 | 8 | 状态码、耗时、大小、初始元数据、尾元数据、JSON 树、错误详情、历史 |
| G9 流式（P5） | 6 | server/client/bidi、消息列表、进度、取消、半关闭、顺序 |
| G10 变量与脚本 | 4 | 变量替换、脚本 res 视图、断言、内置变量 |
| G11 持久化与工具 | 6 | 自动保存、草稿、索引、历史、grpcurl 生成、导出文档 |
| G12 边界与错误 | 8 | 各类失败场景与提示 |
| G13 工程约束 | 5 | i18n、testid、门禁、体积、devserver 桥 |
| **合计** | **72** | 详见 §4–§6 |

---

## 4. 功能清单（明细）

> 状态图例：⬜ 未开始 · 🟡 进行中 · ✅ 已完成

### G1 新建与协议分派

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G1.1 | 新建入口 | 侧栏工具栏「＋」与 TabBar「＋」的下拉/菜单里增加「gRPC 请求」；沿用「未落盘草稿」语义（关闭时才提示保存，`tabs.openDraft`） | ✅ | `components/Sidebar.vue`（工具栏下拉 + 分组菜单）、`components/TabBar.vue`（＋下拉）、`App.vue#newGrpcDraft` |
| G1.2 | 空 gRPC 草稿 | 新建后 `grpc` 段存在（即 gRPC 请求）、`target/service/method/proto/message` 均为空；Schema 分段给出「先导入 .proto」的引导 | ✅ | `stores/tabs.ts#blankGrpcRequest`、`components/GrpcSchemaPane.vue`（空态） |
| G1.3 | 请求栏按协议分派 | gRPC 下把「HTTP 方法选择器」换成「GRPC 徽标 + 服务 · 方法选择器」；地址输入仍是带变量高亮的输入框（`VarInput`），placeholder 为 `localhost:50051` | ✅ | `components/RequestBar.vue`（`isGrpcReq` 分支） |
| G1.4 | 草稿保存弹窗复用 | 关闭草稿时仍走「保存请求（名称 + 分组）/ 不保存 / 取消」对话框，名称按「方法 / 服务」预填；**解析失败时「保存」置灰尚未做**（D4 的 Go 侧校验见 G3.5） | 🟡 | `App.vue#requestClose/draftName` |

### G2 定义管理（导入 / 更新 / 移除）

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G2.1 | 导入 .proto | 文件选择器（可多选，`.proto` 过滤）+ import 目录（逐行「浏览」选目录）；**默认复制进集合 `protos/`**（D2） | ✅ | `App.PickGrpcProtoFiles`/`ImportGrpcProtos`、`components/GrpcSchemaPane.vue` |
| G2.2 | 保留目录结构 | 多文件导入时按所选文件的公共父目录保留相对结构（避免扁平化破坏 import 关系）；导入路径写入请求的 `proto`/`imports` | ✅ | `internal/collection/proto.go#ImportProtos` |
| G2.3 | 更新定义 | 「更新定义」按钮重新选择文件 → 覆盖 `protos/` 同名文件 → 重新解析（D3，唯一更新入口） | ✅ | 同上 + `components/GrpcSchemaPane.vue#importProtos` |
| G2.4 | 定义来源与时间可见 | Schema 分段显示 `protos/greeter.proto · 导入于 2026-10-02 09:31 · 4 个服务`；不提供「已变化」提示（D3） | ✅ | 同上（时间取 `ProtoFileInfo.mod`，**Unix 秒**） |
| G2.5 | 引用外部路径（高级） | 允许只引用集合外路径不复制；UI 明确标注「不可共享 / 不随集合提交」 | ⬜ | 读取侧已允许绝对路径（`collectionProtoFiles`），界面入口未做 |
| G2.6 | 移除定义 | 从集合 `protos/` 删除；**当前只做二次确认文案，未统计被引用数** | ✅ | `App.RemoveGrpcProto`、`components/GrpcSchemaPane.vue#removeProto` |
| G2.7 | 同名冲突 | 同一相对路径直接覆盖（=「更新定义」，界面 toast 提示「已更新定义 …」）；**同一次导入**里选中多个同名文件时第二个起追加序号（`greeter-2.proto`，沿用 `docs` 命名策略） | ✅ | `internal/collection/proto.go#ImportProtos/importOneProto` |

### G3 解析与缓存

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G3.1 | 纯 Go 编译 | `bufbuild/protocompile`（`SourceResolver{ImportPaths}` + `WithStandardImports`），无需用户装 protoc；产出 `protoreflect.FileDescriptor` | ✅ | `internal/proto/compile.go` |
| G3.2 | 显式 generation 缓存 | 内存缓存，键 = `(集合 uid, 相对路径集合, imports, generation)`；**不读 mtime、不做 hash 比对**；只有「导入 / 更新定义」才 `generation++`（D3） | ✅ | `internal/proto/cache.go`、`internal/app/app.go`（`ImportGrpcProtos`/`RemoveGrpcProto` 后 `Invalidate`） |
| G3.3 | 解析错误也缓存 | 失败结果（错误 + 文件 + 行列）一并缓存并常驻展示，直到用户重新导入；避免每次发送重试编译 | ✅ | `internal/proto/cache.go` + `components/GrpcSchemaPane.vue`（常驻错误卡片） |
| G3.4 | 解析失败即时提示 | 导入动作原子化：解析失败 → 不复制、不落盘、不更新缓存，UI 立即报错（文件 + 行列 + 提示语） | ✅ | `internal/app/app.go#ImportGrpcProtos`、`components/GrpcSchemaPane.vue#importProtos` |
| G3.5 | 失败禁止保存（D4） | `SaveRequest` 前置校验：gRPC 请求且定义解析失败 → 返回结构化错误（如 `code: proto_invalid`），前端提示「定义解析失败，无法保存：请先在 Schema 分段重新导入」 | ⬜ | **下一步**：`internal/app/app.go#SaveRequest`、`stores/tabs.ts#flush` |
| G3.6 | 发送门禁 | 定义未解析成功 / `service`+`method` 为空 / `target` 为空 / 消息为空 → 发送按钮禁用并给原因（`title` 提示） | ✅ | `components/RequestBar.vue#sendBlocker`、`stores/tabs.ts#send`（兜底）、`lib/grpc.ts#grpcSendBlocker` |

### G4 服务与方法选择

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G4.1 | 服务/方法列表 | 从编译结果枚举 `[]Service{FullName, Methods[]}`，请求栏下拉按服务分组、可搜索（标签含服务短名，注释作为 tooltip） | ✅ | `app.GrpcSchemaInfo.Services`、`components/RequestBar.vue`（分组选项） |
| G4.2 | 方法签名与注释 | 显示 `rpc Method (Req) returns (stream Resp)` 形态与 proto 注释（`SourceInfo`） | ✅ | `internal/proto/compile.go`（`Comment`/`Input`/`Output`）、`components/GrpcSchemaPane.vue` |
| G4.3 | 流式类型标注 | 列表与 RequestBar 上标注 unary / server-stream / client-stream / bidi，并写入请求 `stream` 字段 | ✅ | 同上（选中方法即写 `grpc.stream`） |
| G4.4 | 输入输出类型提示 | 选择方法后展示 `Input`/`Output` 完整类型名，作为「生成样例」的依据 | ✅ | `components/GrpcSchemaPane.vue`（`demo.HelloRequest → demo.HelloReply`） |
| G4.5 | 空态 | 未导入定义 / 定义里没有 service 时给出明确空态与下一步（导入 / 换个 proto） | ✅ | `components/GrpcSchemaPane.vue`（`grpc.schema.empty` / `noService`） |

### G5 消息编辑

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G5.1 | JSON 消息编辑 | 沿用现有 JSON 编辑体验（等宽、格式化），存盘为字符串 | ✅ | `components/GrpcMessagePane.vue`、`RequestEditor.vue`（分段分派）。与 HTTP 请求体一致：等宽 textarea + 一键格式化（无独立高亮层） |
| G5.2 | 生成样例 | 「按 proto 生成样例」：按 `MessageDescriptor` 递归填充（标量默认值、repeated 一条、map 一条、oneof 首项、枚举首值），产出可编辑 JSON | ✅ | `internal/proto/template.go`、`App.GrpcSampleMessage`、`GrpcMessagePane`（已有内容时先确认再覆盖） |
| G5.3 | 消息校验 | 发送前用 `protojson.Unmarshal` 校验，错误带字段路径（`DiscardUnknown=false`），在 Message 分段内联提示 | ✅ | `internal/proto/fields.go#ValidateMessage`、`App.GrpcValidateMessage`（防抖 350ms 内联展示）、执行器发送前仍会再校验一次 |
| G5.4 | 字段提示 | Message 分段侧栏列出当前方法的字段（名称/类型/repeated/enum），点击插入 | ✅ | `internal/proto/fields.go#Fields/FieldInfo`、`App.GrpcMessageFields`、`GrpcMessagePane`（在**光标处**插入「键 + 类型默认值」，消息为空时给完整骨架）。当前只列顶层字段 |
| G5.5 | Well-Known Types | `Timestamp/Duration/Any/Struct/…` 按 protojson 语义（RFC3339 字符串、`@type` 等）支持与提示 | 🟡 | 样例与执行器都走 protojson（实测 `Timestamp` 生成 `1970-01-01T00:00:00Z`）；**字段级格式提示未做**（字段表只给类型全名，`Any` 仍需手写 `@type`） |

### G6 元数据

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G6.1 | Metadata KV 表 | 直接复用 `KeyValueTable`（名称/值/启用/说明/批量编辑） | ✅ | `components/GrpcMetadataPane.vue`（复用现有组件，零改动） |
| G6.2 | 变量替换 | 名称与值都支持 `{{var}}`，复用 `varx.Resolve` 与 `runner.resolveKV`（`runner.go:243-254`） | ✅ | `internal/runner/grpc.go`（实测 `x-token` 被服务端回显成 `x-echo-token`）。表格本身是纯 input，无 `{{}}` 高亮（与 HTTP 请求头一致） |
| G6.3 | 敏感值 | 沿用环境变量的 `secret` 标记，悬停只显示掩码（与 `VarInput` 一致） | 🟡 | 运行时已按变量解析（敏感值在提示里仍只出掩码）；**metadata 表格未接 `VarInput`**，界面不做掩码展示 |

### G7 连接与安全

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G7.1 | 服务地址 | `host:port`，支持变量与无 scheme 输入；非法格式即时提示 | ✅ | 执行器 `renderTarget/normalizeTarget`（容忍 `grpc://`、`https://`、`dns:///` 与多余路径，缺端口 / 为空即时报错）；`components/RequestBar.vue` 占位 `localhost:50051` |
| G7.2 | 明文 / TLS | Options 分段：明文（默认）/ TLS；TLS 下可选 CA、客户端证书（文件选择器 `PickFile`） | ✅ | `components/GrpcOptionsPane.vue`、`runner.transportCreds`（给全 cert+key 即 mTLS） |
| G7.3 | 跳过校验 | 复用全局「忽略 SSL 校验」与请求级 `settings.insecureSsl` | ✅ | `runner.MergeOptions`（HTTP 与 gRPC 同一套）+ `transportCreds` 合并；单测覆盖（全局关、请求级开 → 自签握手成功） |
| G7.4 | 超时 / 截止时间 | 默认取全局超时，可按请求覆盖（`settings.timeoutSec`），UI 显示剩余 | ✅ | 同上；Options 分段可填秒数（留空 = 全局），实测 1s + 服务端睡 3s → `DEADLINE_EXCEEDED(4)` |
| G7.5 | 压缩 | 可选 `gzip`（`grpc.UseCompressor`），默认关闭 | ✅ | `grpc.compress` + `runner`（`grpc.UseCompressor(gzip.Name)`）；实测 gzip 请求 → OK（服务端需注册 gzip 解压器，`cmd/grpcfixture` 已注册） |

### G8 发送与响应

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G8.1 | 动态调用 | `dynamicpb` + `conn.Invoke`（unary）/ `conn.NewStream`（流式），无代码生成 | ✅ | `internal/runner/grpc.go`（unary 已打通；流式见 G9/P5） |
| G8.2 | 状态码 | `status.FromError` → `OK` / `NOT_FOUND(5)` 等；徽章配色复用响应面板语义 | ✅ | `components/lib/grpc.ts`（`grpcCodeLabel`/`grpcStatusType`，与 Go 侧 `GrpcCodeName` 同一套；OK 绿、取消/超时黄、其余红）、`ResponsePanel.vue`（按 `proto === 'gRPC'` 分派） |
| G8.3 | 耗时与大小 | 耗时（首包/总）、收发字节数展示 | 🟡 | 总耗时、响应字节数与**发送字节数**（`Result.SentSize`）已展示；流式的「首包耗时」要等 P5 |
| G8.4 | 初始元数据 | 响应头页签展示 initial metadata | ✅ | `ResponsePanel.vue`（gRPC 下页签改名「初始元数据」，内容即 `Result.Headers`） |
| G8.5 | 尾元数据 | **新增「响应尾」页签**展示 trailing metadata | ✅ | 同上（`Result.Trailers`；实测 `x-trailer=t1`、`x-echo-name=alice`） |
| G8.6 | 响应消息 | `protojson.Marshal`（缩进 2、`EmitUnpopulated=false`）→ **现有 `JsonTree.vue` 零改动渲染** | ✅ | `internal/runner/grpc.go`、`components/JsonTree.vue`（零改动，实测渲染 `{"message":"hello alice","code":1}`） |
| G8.7 | 错误详情 | `google.rpc.Status` 的 `details`（`Any`）按 protojson 展开展示 | 🟡 | 错误体已是 JSON（`{code, codeName, message}` 直接进 JSON 树）；`details` 目前只带各 `Any` 的**类型 URL 列表**，未做完整展开 |
| G8.8 | 历史记录 | 发送写入历史（`internal/history`），可回看/重放 | ✅ | `internal/app/app.go#recordHistory`（与 HTTP 共用；实测历史里出现 `GRPC grpc://127.0.0.1:50051/demo.Greeter/SayHello 0`） |

### G9 流式（阶段 P5，非 MVP）

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G9.1 | server-stream | 逐条追加消息列表（序号/时间/大小），可展开每条 JSON | ⬜ | `internal/runner/grpc.go`、`components/ResponsePanel.vue` |
| G9.2 | client-stream | 编辑器可维护「多条消息」，支持逐条发送与「一次发送全部」 | ⬜ | `components/GrpcMessagePane.vue` |
| G9.3 | bidi | 双向交互：发送区 + 接收区并行，支持半关闭（`CloseSend`） | ⬜ | 同上 |
| G9.4 | 增量推送 | 结果不能只靠「一次返回」，需 `EventsEmit` 增量事件（`grpc:message` / `grpc:end`），前端订阅后增量渲染 | ⬜ | `internal/app/app.go`、`lib/ipc.ts#onAppEvent` |
| G9.5 | 取消 | 「取消」按钮 → `CancelSend` 语义扩展到流（关闭 stream + 取消 ctx） | ⬜ | `stores/tabs.ts#cancelSend` |
| G9.6 | 背压与上限 | 单请求消息数/总字节上限（对齐 HTTP 侧 `maxBodySize = 10<<20`，`runner.go:29`），超限提示并中止 | ⬜ | `internal/runner/grpc.go` |

### G10 变量与脚本

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G10.1 | 地址/元数据/消息变量替换 | `target`、metadata、`message` 中的 `{{var}}` 发送前替换；未定义变量沿用现有语义（`resolve.missing` 高亮） | ✅ | 三处都在执行器里用 `varx.Resolve` 渲染（消息在 `SendGRPC` 里先渲染再 protojson 解析）；地址栏高亮走 `tabs.doResolve`（gRPC 取 `target`）；单测覆盖消息变量 |
| G10.2 | 脚本 pre / post | pre 注入 `bru` 不变；post 的 `res` 视图扩展 gRPC 语义（`res.status` = gRPC code、`res.grpcCode`、`res.trailers`、`res.body` = 响应 JSON 文本） | ✅ | `internal/script/script.go`（`Response.IsGRPC/GRPCCode/Trailers` + VM 注入）、`internal/app/app.go`（按 `res.Proto == "gRPC"` 填充） |
| G10.3 | 断言 | 沿用 `asserts`，`res.status` 与 `res.body` 判定规则文档化（避免与 HTTP 混淆） | ✅ | 规则写在 `internal/script/script.go` 的类型注释里：gRPC 响应的 `res.status` **是 gRPC code**（0 = OK），用 `res.isGrpc` 区分协议、`res.grpcCode` 取码值；`res.body` 为 protojson 解析后的对象 |
| G10.4 | 内置变量 | `$uuid`/`$timestamp` 等内置变量在 gRPC 下同样可用 | ✅ | 与 HTTP 同一套 `varx.Builtins()`（`app.envVars` 注入），地址 / 元数据 / 消息里都能用 |

### G11 持久化与工具

| # | 功能 | 说明 | 状态 | 落点 |
|---|---|---|---|---|
| G11.1 | 自动保存 | 沿用 800ms 防抖 + 关闭前 flush；解析失败期间拒绝写入（D4）并提示 | ⬜ | `stores/tabs.ts#touch/flush` |
| G11.2 | 草稿 | gRPC 草稿同样支持 localStorage 持久化与重启恢复 | ⬜ | `stores/tabs.ts`（drafts） |
| G11.3 | 索引与搜索 | 命中 `title/url/method/path`（URL 为 `grpc://…`），命令面板可搜到 gRPC 请求 | ✅ | 沿用 `Method = GRPC` + 派生 URL（`internal/index` 零改动）；实测 `SearchIndex("SayHello")` 命中 `{method: GRPC, url: grpc://{{host}}/demo.Greeter/SayHello}`。按建议加 `Type` 列**未做**（当前用 Method 已能区分） |
| G11.4 | 历史 | 与 HTTP 共用历史面板，列表标注协议 | ✅ | `internal/app#recordHistory` 与 HTTP 共用；`components/HistoryDialog.vue` 用 `MethodTag` 渲染（GRPC 走协议色）；实测历史里有 `GRPC grpc://…/demo.Greeter/SayHello 0` |
| G11.5 | grpcurl 生成 | 代码生成新增 `grpcurl`（含 `-plaintext`、`-H`、`-d`、`-import-path`/`-proto`） | ✅ | 共享包 `codegen.GrpcurlSnippet`（v0.5.0）+ `App.generateGrpcCode`（变量渲染、明文/TLS → `-plaintext`/`-insecure`）+ `CodeGenDialog`（gRPC 只列 grpcurl）；实测片段含 `-import-path 'protos'`、`-proto 'greeter.proto'`、`-H`、`-d` 与 `'host' 'demo.Greeter/SayHello'` |
| G11.6 | 导出文档 | 导出 Markdown 时按协议分节（gRPC 段展示 target/service/method/message 示例） | ✅ | `internal/collection/export.go`：gRPC 请求输出服务方法（含流式中文标注）、定义、连接、Metadata 与 JSON 消息块，不再走 HTTP 的 query/请求头/请求体分支 |

### G12 边界与错误

| # | 功能 | 说明 | 状态 |
|---|---|---|---|
| G12.1 | 找不到 proto 文件 | 「找不到 protos/xxx.proto（可能未导入或已被移除）」+ 一键跳转导入 | ✅（`collectionProtoFiles` 的报错 + Schema 分段常驻错误卡片 + 卡片里的「重新导入」） |
| G12.2 | import 缺失 | 「找不到 import 依赖 google/api/annotations.proto：请把其所在目录加入 import 路径」 | ✅（`protocompile` 原始报错 + import 路径行内编辑，同一张错误卡片） |
| G12.3 | 语法/语义错误 | 展示文件 + 行列 + 原始错误信息（`protocompile` reporter） | ✅（实测 `broken.proto:8:1: syntax error: unexpected '}', expecting int literal`，且导入原子化不落盘） |
| G12.4 | 目标不可达 | DNS/连接失败/TLS 握手失败分别给可读提示（复用 `sendError` 风格，`runner.go:107-116`） | ✅（作为 `UNAVAILABLE(14)` 结果返回，详情进响应体；实测 `dial tcp 127.0.0.1:1: connectex: …` 原文可见） |
| G12.5 | 截止时间到 | 「请求超时（deadline 30s）」并区分是客户端取消还是服务端未响应 | 🟡（`DEADLINE_EXCEEDED(4)` + `context deadline exceeded` 已可见，实测请求级 1s 超时；「区分取消 / 服务端未响应」的细分文案未做） |
| G12.6 | 消息不合法 | 字段路径级错误提示，定位到具体字段 | ✅（Message 分段内联校验 + 执行器发送前校验；错误形如 `proto: (line 1:2): unknown field "nope"`） |
| G12.7 | 流式超上限 | 超过消息数/字节上限时中止并保留已收内容 | ⬜（随 P5 流式一起做；当前单条消息上限 10MB 已生效） |
| G12.8 | 二进制/大消息 | 非 UTF-8 或超大响应按现有「二进制提示/base64」策略展示 | ✅（gRPC 响应恒为 protojson 文本，走 JSON 树；`bytes` 字段按 protojson 的 base64 语义展示；超大消息由 10MB 上限挡住） |

### G13 工程约束

| # | 约束 | 说明 | 状态 |
|---|---|---|---|
| G13.1 | i18n | 所有新增 UI 文案进 `zh-CN.ts`/`en-US.ts`，过 `npm run i18n:check`；**注意示例里的 JSON 花括号会让 vue-i18n 抛错**（用单引号/转义或改写示例） | ✅（新增 `grpc.*` 文案含 `stream`/`block` 子段，`KEYS_MATCH 389 → 419`） |
| G13.2 | testid | 可交互元素加 `data-testid`（命名 `模块.子域.元素`，如 `grpc.schema.import`、`grpc.method.pick`） | ✅（`tree.new`/`tab.new` 下拉项、`req.grpcBadge`、`req.grpcMethod`、`grpc.schema.*`、`grpc.method.<Name>`） |
| G13.3 | 门禁 | `bash scripts/check.sh` 全过（gofmt / build / vet / go test / vue-tsc / i18n） | ✅（`go test` 仅剩本文档已记录的 Windows XDG 两包） |
| G13.4 | 依赖与体积 | 引入 `google.golang.org/grpc`、`google.golang.org/protobuf`、`github.com/bufbuild/protocompile`；预估 +8~12MB（26.99MB → 约 35~39MB）；本机 Go 代理可达 | ✅（实测 `28.30 MB → 34.34 MB`，+6.0 MB） |
| G13.5 | devserver 桥 | 新增 App 方法必须 JSON 可序列化（devserver 反射桥按形参类型 `json.Unmarshal`，`cmd/devserver/main.go:87-136`）；同时更新 `frontend/wailsjs` 绑定 | ✅（`PickGrpcProtoFiles` 返回 `[]string` 可序列化；`wails build` 已重新生成绑定） |

---

## 5. Schema 状态机（D3 / D4 的落地形态）

```
空定义 ──导入成功──▶ 已解析(gen=n) ──更新定义成功──▶ 已解析(gen=n+1)
   │                     │                                  │
   │                 解析失败                            解析失败
   │                     ▼                                  ▼
   └──导入失败────▶ 解析失败(常驻错误) ◀──────────── 仍用 gen=n 定义 + 新错误提示
```

| 状态 | 发送 | 保存 | 界面 |
|---|---|---|---|
| 空定义 | 禁用 | 允许（草稿也能存） | Schema 分段引导「导入 .proto」 |
| 已解析 | 允许 | 允许 | 显示来源 / 导入时间 / 服务方法列表 |
| 解析失败 | 禁用 | **拒绝**（D4） | 常驻错误卡片 + 「重新导入」按钮 |

**唯一例外（防锁死）**：定义解析失败时，**只允许保存「定义路径 / import 路径」这两个字段的修改**（走专用方法 `UpdateRequestProto`），因为这是用户修复问题的通道；其余字段（消息/元数据/地址）仍不允许保存。若实现成本高，退化为「允许保存但弹出强提示」也可接受，需在实施时确认。

---

## 6. 兼容性与影响面

### 6.1 向后兼容（已验证）

| 场景 | 现状 | 结论 |
|---|---|---|
| 旧版客户端打开含 `type: grpc` 的集合 | `internal/collection/collection.go:500` 的 `default` 分支只处理 `docs/*.md` → 该文件被**静默跳过** | 不报错、不干扰 ✅ |
| 服务端（api-doc-go）解析此类文件 | `server/internal/service/bruno.go:68` 跳过 `type != "http"` | 不报错 ✅（但也不展示） |
| `.proto` 放进集合 | 扫描器只读 `.yml/.yaml`（`collection.go:456`） | 不进树、不报错 ✅ |

### 6.2 必须顺手改的一处（与 D3 直接相关）

`internal/watch/watch.go:125-160` 的外置改动监听**与扩展名无关**：任何 Write/Create/Rename/Remove 都会推到 `app.go:177` → 前端 `collection:changed` → 重载/提示。若 `.proto` 放在集合内，用户一改 proto 就会触发「集合外部改动」，与 D3 冲突。

**改法**：给 `watch` 加扩展名白名单（与扫描器一致：`.yml/.yaml/.md`），并补单测；导入 proto 写盘时调用一次 `Collection.Ignore`（自写回环）避免导入动作自触发。

### 6.3 共享模块与发布

- `api-doc-go-share` 需加 `GRPC` 块、把 `HTTP` 改成可省略指针、`knownTopLevel` 加 `grpc`；
- 本机无 `go.work`，开发期用 `replace` 指向 `../api-doc-go-share`，联调完成后发新版 share 并回填版本号。

---

## 7. 分阶段实施计划

| 阶段 | 内容 | 估时(人日) | 验收标准 |
|---|---|---|---|
| **P0** | 依赖引入；`internal/proto`：编译 + 显式 generation 缓存 + 服务方法枚举 + 生成样例；单测（fixture `.proto`） | 1 | `go test ./internal/proto` 绿；同一 `.proto` 编译结果被复用（generation 不变时不重编） |
| **P0.5** | `watch` 扩展名白名单 + 单测 + 导入时 `Ignore` | 0.5 | 改 `.proto` 不再触发「集合已变更」；改 `.yml` 仍触发 |
| **P1** | `internal/runner/grpc.go`：unary 打通（`dynamicpb` / `protojson` / metadata / status / trailers / 明文 + TLS 基础） | 1.5–2 | `cmd/grpcfixture` 上 unary 成功、错误码正确、尾元数据可见；单测覆盖 |
| **P2** | UI：新建入口 + RequestBar 服务·方法选择器 + **Schema 分段**（导入/更新/错误/来源/方法列表） | 2–2.5 | 导入失败原子化（不落盘）；来源与时间可见；服务方法可搜可选 |
| **P3** | UI：**Message 分段**（生成样例 + 校验反馈）+ Metadata 分段 + 响应面板（状态/初始元数据/**尾元数据**/JSON） | 2–2.5 | 端到端：导入 → 输地址 → 选方法 → 发送 → 看到响应与尾元数据 |
| **P4** | 落盘/索引/历史/搜索适配 + grpcurl 生成 + 导出 | 1–1.5 | gRPC 请求可保存/重启恢复/可搜到/历史可回看；`grpcurl` 命令可直接复制执行 |
| **P5** | 流式（server/client/bidi）+ 增量事件推送 + 取消 + 上限 | 2–3 | 4 种方法（unary/server/client/bidi）在 fixture 上跑通；流式消息增量出现；取消可中断 |
| **P6** | TLS/mTLS + 证书选择 + 压缩 + `protos/` 目录下拉（免选文件） | 1–1.5 | mTLS 双向认证对 fixture 成功；无需文件选择器即可从集合内挑 proto |
| **P7** | 反射（可选增强：「从服务地址拉取定义」） | 1–1.5 | 对开启反射的服务可零文件调用；关闭反射时给出明确提示 |
| **P8** | 集合级默认定义（`opencollection.yml` 配一次，请求只写 service/method）+ 集合内多 proto 管理界面 | 1–1.5 | 多请求共享一份定义时无需重复填路径 |
| **P9** | 测试与回归：`cmd/grpcfixture`（4 种方法 + 错误码 + metadata 校验 + 可选反射/双向 TLS） | 1.5–2 | 门禁绿；关键路径有单测；真窗口手工核对一轮 |

- **MVP（P0–P3，端到端可用）≈ 6–7.5 人日**
- **全量 ≈ 13–16 人日**（不含服务端展示）
- 进度：**P0 / P0.5 / P1 / P2 / P3 / P4 / P6 / P8 / P9 已完成** —— 除流式与反射外的功能全部落地，落点与实施细节见 §10。
- **剩余**：**P5 流式**（server / client / bidi + 增量事件 + 取消 + 上限，6 项）与 **P7 反射**（计划里标注为可选增强）。

---

## 8. 风险与待定项

| # | 风险 / 待定 | 影响 | 应对 |
|---|---|---|---|
| R1 | import 路径在真实工程里常需要多个 vendor 目录 | 解析失败率高 | UI 支持多 import 路径 + 明确报错；P6 提供「集合内 `protos/` 自动加入 import」 |
| R2 | 大 proto（googleapis / k8s）编译耗时 | 首次解析卡顿 | 缓存 + 异步解析 + loading；解析在 Go 侧不阻塞 UI |
| R3 | `Method`/`URL` 复用造成语义混淆 | 搜索/同步列表可读性 | 建议 index/history 加 `Type` 列；UI 用协议标签区分 |
| R4 | 流式与「一次返回 Result」IPC 模型冲突 | 需要早期定型 | P1 即确定事件名与载荷格式，避免 P5 返工 |
| R5 | 体积 +8~12MB | 分发/更新变大 | 可接受；如需瘦身可后续用 build tag 拆分 |
| R6 | 共享模块 schema 变更影响服务端 | 需协同发版 | 先 `replace` 联调；服务端只需「不认识也不崩」（现状已满足） |
| R7 | 「不允许保存」把用户锁死 | 体验风险 | §5 的唯一例外：允许改定义路径/import |
| **T1** | 是否要「内联 proto 文本」（把定义内容直接存进请求文件） | 影响 schema 设计 | 待定，倾向不做（与 D2 冲突，且文件体积大） |
| **T2** | gRPC code 放 `Status` 还是新增 `GRPCCode` | 影响历史/断言语义 | 待定，实施 P1 时定（倾向新增字段并让 `Status` 与之同步，兼容现有 UI） |
| **T3** | Compile 缓存进程内是否跨集合共享 | 影响内存占用 | 待定，倾向按集合隔离（切换集合即清空） |

---

## 9. 复用清单（不改动即可用的既有能力）

| 能力 | 现状 | 复用于 gRPC |
|---|---|---|
| 变量替换 | `internal/varx`（`Resolve`/`Builtins`） | 地址、metadata、message |
| KV 行渲染 | `runner.resolveKV`（`runner.go:243-254`） | metadata 逐行渲染 |
| 全局/请求级设置 | `internal/config` + `Settings` 覆盖（`runner.go:33-40`） | 超时、忽略证书 |
| 文件/目录选择 | `App.PickFile` / `App.PickDirectory` | 选 `.proto`、import 目录、证书 |
| 脚本引擎 | `internal/script`（goja） | pre/post 脚本（需扩展 `res` 视图） |
| JSON 响应渲染 | `components/JsonTree.vue` | protojson 结果直接渲染 |
| 草稿/会话/撤销 | `stores/tabs.ts` | gRPC 请求同一套 |
| 索引/历史/搜索 | `internal/index`、`internal/history` | URL 合成后直接可用 |
| 门禁 | `scripts/check.sh` | 新增单测纳入 |

---

## 10. 实施进度

| 阶段 | 状态 | 落点 / 说明 |
|---|---|---|
| **P0** | ✅ 2026-10-02 | 依赖：`google.golang.org/grpc v1.84.0`、`google.golang.org/protobuf v1.36.12`、`github.com/bufbuild/protocompile v0.14.1`；新增 `internal/proto`（`compile.go` 编译+服务方法枚举+描述符查询、`cache.go` 显式失效缓存、`template.go` 样例消息、`testdata/*.proto`）+ 13 个单测 |
| **P0.5** | ✅ 2026-10-02 | `internal/watch`：`DefaultExtensions`（`.yml/.yaml/.md`）白名单，`.proto`/证书等变更不再触发「集合外部改动」；目录与无扩展名路径维持原行为（兼顾「先建目录再写文件」的兜底）+ 3 个单测 |
| **P1** | ✅ 2026-10-02 | `internal/runner/grpc.go`：unary 动态调用（`dynamicpb` + `protojson`）、metadata（含变量）、gRPC 状态、尾元数据、明文/TLS/mTLS 凭据、地址归一化；`runner.Result` 新增 `Trailers`；12 个单测（真实 TCP + `UnknownServiceHandler` 动态服务端 + 自签证书 TLS） |
| **P2（后端部分）** | ✅ 2026-10-02 | ① 共享包 `api-doc-go-share/collection/request.go`：新增 `GRPCBlock`/`GRPCTLS`、`RequestFile.GRPC`、`knownTopLevel+grpc`；② 集合层 `internal/collection/proto.go`（导入/列举/删除，落点 `protos/`）+ `collection.go`/`types.go` 按 `info.type` 读写分派（gRPC 请求的 `Method` 固定 `GRPC`、`URL` 派生 `grpc://目标/服务/方法`，供索引/历史/搜索继续可用）；③ App 门面 `internal/app/grpc.go`：`ImportGrpcProtos` / `ListGrpcProtos` / `RemoveGrpcProto` / `LoadGrpcSchema` / `GrpcSampleMessage` + `SendRequest` 按协议分派（`executeRequest`），并接入 `proto.Cache`（换集合即失效）；④ 绑定已由 `wails build` 重新生成 |
| **P2（前端部分）** | ✅ 2026-10-02 | ① 新建入口：侧栏工具栏「＋」下拉、分组行「＋」菜单、TabBar「＋」下拉都增加「gRPC 请求」（`Sidebar.vue`/`TabBar.vue`/`App.vue#newGrpcDraft`），沿用未落盘草稿语义；② 空草稿 `stores/tabs.ts#blankGrpcRequest`（`grpc` 段存在即 gRPC，method 固定 `GRPC`，URL 留空由集合层派生）；③ 请求栏按协议分派 `RequestBar.vue`：GRPC 徽标 + 服务/方法分组下拉（`filterable`、选项值用 `服务.方法` 全名）+ 服务地址 `VarInput`（placeholder `localhost:50051`），gRPC 下隐藏「生成代码」（grpcurl 属 P4）、发送按钮按门禁置灰并给原因；④ Schema 分段 `components/GrpcSchemaPane.vue`：导入/更新（多选 `.proto`，新 App 方法 `PickGrpcProtoFiles`）、集合内定义下拉切换、移除（二次确认真）+ 来源与时间 + 服务方法列表（注释/流式标注/入参→出参，点击即选中）+ 常驻错误卡片 + 空态引导，`RequestEditor` 按协议切换分段（gRPC = Schema/Vars/Script/Tests/Docs）；⑤ 打开/恢复请求时自动 `loadGrpcSchema`（tab 上缓存 `grpcSchema`/`grpcError`）；⑥ 绑定由 `wails build` 重新生成 |
| **P3** | ✅ 2026-10-02 | ① Message 分段 `components/GrpcMessagePane.vue`：等宽 JSON 编辑 + 「生成样例」（已有内容先确认）+ 一键格式化 + **按定义实时校验**（防抖 350ms，错误带字段路径，如 `proto: (line 1:2): unknown field "nope"`）+ 字段提示（`internal/proto/fields.go#Fields` → `App.GrpcMessageFields`，11 个字段带类型，点击在光标处插入「键 + 类型默认值」；校验走 `App.GrpcValidateMessage`）；② Metadata 分段 `components/GrpcMetadataPane.vue`（复用 `KeyValueTable`，零改动）；③ 响应面板按 `proto === 'gRPC'` 分派：状态徽章 `OK(0)`/`NOT_FOUND(5)` + 语义色（`lib/grpc.ts` 的 gRPC code 表）、「响应头」页签改名「初始元数据」、新增「响应尾」页签（G8.4/G8.5/G8.2）；④ 发送门禁去掉「消息非空」条件（空消息合法）；⑤ 绑定由 `wails build` 重新生成 |
| **P4** | ✅ 2026-10-02 | ① 落盘/索引/历史/搜索沿用「Method=GRPC + 派生 URL」，实测索引命中与历史记录（G11.3/G11.4）；② `grpcurl` 生成：共享包 `codegen.GrpcurlSnippet`（v0.5.0）+ `App.generateGrpcCode` + `CodeGenDialog` 按协议切语言（G11.5）；③ 导出 Markdown 按协议分节（G11.6）；④ G3.5：`App.checkGrpcSavable` 在 `SaveRequest` / `CreateRequestFromDraft` 前置校验定义，`tabs.flush` 捕获拒绝后保留脏标记 + 提示 + 常驻错误卡片；⑤ G10.1 消息内变量替换、G10.2/G10.3 脚本 `res` 视图的 gRPC 语义 |
| **P6** | ✅ 2026-10-02 | TLS / mTLS 由 Options 分段（明文 / TLS + CA / 客户端证书 / 私钥 + 跳过校验）承接，`runner.transportCreds` 给全 cert+key 即 mTLS；压缩 `gzip` 落 `grpc.compress` 并走 `grpc.UseCompressor`；「集合内定义下拉」在 P2 已提供（免点文件选择器） |
| **P8** | ✅ 2026-10-02 | 集合级默认定义：共享包清单 `Manifest.GRPC`（`GRPCDefault{Proto, Imports}`，v0.5.0）+ `Collection.GrpcDefault/SetGrpcDefault`（内存副本 + 回写清单）+ 读时回落（`applyGrpcDefault`）/ 写时省略（`stripGrpcDefault`）+ Schema 分段的「设为集合默认 / 取消 / 空态一键使用」+ `App.GetGrpcDefault/SetGrpcDefault`（小驼峰 DTO） |
| **P9** | ✅ 2026-10-02 | 新增 `cmd/grpcfixture`：按 `.proto` 动态收发（`UnknownServiceHandler`），覆盖 unary / server / client / bidi 四种形态、`boom` → NOT_FOUND、`slow` → 睡 3s、metadata 回显 + 尾元数据、`-tls-cert/-tls-key` 可选 TLS、内置 gzip 解压器；配套 `fixture_test.go`（一元 / 错误码 / gzip / 流式门禁 / 描述符缺失），纳入门禁 |
| P5 | ⬜ | 流式（server / client / bidi）+ 增量事件推送 + 取消 + 上限 —— 见 §7 与 G9 |
| P7 | ⬜ | 反射拉取定义（可选增强） |

### 实施中确认的关键细节（后续实现勿踩）

1. **调用路径是 `/包.服务/方法`**（服务与方法之间是**斜杠**）：写成 `/包.服务.方法` 会被服务端判为 `malformed method name` 并返回 `UNIMPLEMENTED`。已封装为 `runner.grpcCallPath()`，`Result.URL` 也用同一形态（`grpc://host:port/包.服务/方法`）。
2. **非 OK 状态作为「结果」而非 error 返回**：`Result.Status` = gRPC code、`Result.Trailers` = 尾元数据、`Body` = `{code, codeName, message, details[]}` JSON。这样响应面板的状态徽章、尾元数据页签、JSON 树都能直接渲染；只有「没到服务端」的情况（地址非法 / 消息不合法 / 证书读不出来 / 流式方法暂不支持）才返回 error。
3. **状态码名统一 SCREAMING_SNAKE**：`runner.GrpcCodeName()` 维护 `OK / NOT_FOUND / DEADLINE_EXCEEDED …` 对照表（`codes.Code.String()` 是 CamelCase，界面与文档按官方大写名展示；前端 P3 需要一份同样的 TS 表）。
4. **样例消息的坑**：map 字段的样例值要按 `MapValue()` 取（map 字段自身是「合成 entry 消息」，直接当消息处理会类型不匹配）；`google.protobuf.Any` 无法自动编造 `@type`，样例里留空；自引用消息必须限深（`sampleMaxDepth = 3`）。
5. **缓存 key 要排序 + 去重**：同一份定义「顺序不同 / import 路径重复」必须命同一个 key，否则每次打开请求都会重新编译。
6. **连接失败也是状态**：目标不可达 / 证书不受信 → `UNAVAILABLE(14)` 结果（含原因），不是 error —— 界面上表现为「状态徽章 + 详情」，与 HTTP 的 4xx 观感一致。
7. **`http:` 段不改成指针**（§2.3 原计划的调整）：yaml.v3 认 `IsZeroer`，给 `HTTPBlock` 加 `IsZero()` + 标签改 `http,omitempty` 即可让 gRPC 文件不写空 `http: {}`，而 HTTP 文件照旧 —— 比改成 `*HTTPBlock` 的迁移风险小得多（服务端与既有读取方都不用动）。已用单测双向锁定（gRPC 文件无 `http:`、HTTP 文件有 `url:`）。
8. **导入两步走**：先用「源路径」编译校验，成功后再复制进集合 `protos/`，最后按集合内相对路径重新解析并返回 —— 这样解析失败时**不落盘**（D4）。
9. **多文件导入的入口定义**取「自身定义了服务的第一个文件」（`proto.Compile` 返回的 `Files` 与入参文件顺序一一对应，可按下标回推），其余文件在 `Protos` 里供切换。
10. **共享包已发版**：`api-doc-go-share` 已发布 **v0.4.0**（2026-10-02，含 `grpc` 段、`HTTPBlock.IsZero` + `http,omitempty`；CHANGELOG/README 同步更新，tag 与 master 均已推送 GitHub）。客户端 `go.mod` 直接 `require ... v0.4.0`，联调期的临时 `replace` 已删除 —— 其他机器与 CI 都能正常构建。服务端仍停在 v0.3.0 不受影响（本次只有新增与「写盘省略空 http 段」，无破坏性变更）。
11. **体积实测**：引入 grpc + protobuf + protocompile 后桌面端 `28.30 MB → 34.34 MB`（**+6.0 MB**），比 §7 预估的 +8~12MB 略好。
12. **naive 选择器的空值**：`:value` 传**空串**会被当成「已选中」，placeholder 被吃掉（服务/方法选择器一开始就不显示「先导入 .proto 定义」）；未选中必须传 `null`。同理 `@update:value` 的清空回调给的是 `null`，写回前要归一成 `''`。
13. **`collection.ProtoFileInfo.mod` 是 Unix 秒**（Go `time.Unix()`），前端要 `new Date(mod * 1000)`；当毫秒用会显示成 1970。`GrpcProtoFile.mod` 的类型注释已标注单位。
14. **原生文件对话框在浏览器态点不了**：`PickGrpcProtoFiles` 走 `runtime.OpenMultipleFilesDialog`，devserver/Playwright 里用 `window.go` 代理桩掉这一个方法（其余方法照旧走 HTTP 反射桥），导入 → 落盘 → 解析 → 选方法 → 保存草稿 → 重开还原整条链路仍是真实代码在跑。
15. **gRPC 草稿的页签名**：gRPC 请求没有 query/请求头/请求体，`RequestEditor` 的分段按协议切换（gRPC = Schema/Message/Metadata/Vars/Script/Tests/Docs），切 tab 时要把分段复位到该协议的首个页签，否则会停在 HTTP 的 Params 上空转。
16. **请求消息可以为空**：空消息 = 全默认值消息，执行器与 `ValidateMessage` 都按「跳过解析」处理，所以发送门禁**不含**「消息非空」（此前加上它，只是因为当时还没有消息编辑器）；内容是否合规由 Message 分段实时校验 + 执行器发送前再校验一次兜底。
17. **字段默认值的合法写法**：枚举给 `0`（protojson 接受枚举数字，且 proto3 首个枚举值恒为 0），message / map / repeated 给 `{}` / `{}` / `[]`，`null` 对任意字段都合法（= 默认值）——样例生成仍按名字给（`"COLOR_UNSPECIFIED"`）。
18. **取原生 textarea 要认对属性**：naive 的 `InputInst` 暴露的是 `textareaElRef`（不是 `textareaEl`）；「点击字段插入到光标处」要靠它 + `setSelectionRange`。
19. **popconfirm 的确认回调**：`@positive-click` 才执行动作，光点触发按钮只会展开气泡（「生成样例」的覆盖确认、P8 的「设为默认」都是这样，写 e2e 要先点确认按钮）。
20. **清单改动必须回写**：`Collection.writeManifest` 是「全量重写」，集合级默认定义要在 `Collection` 里留一份内存副本（`grpcDefault`）再写回，否则每次打开集合都会把它写丢。
21. **共享包 `GRPCDefault` 只有 yaml 标签**：直接经 App 方法返回会序列化成 `{"Proto":…}`（前端按 `proto` 取不到，P8 联调时踩到）；App 层用 `GrpcDefaultInfo` 显式小驼峰 DTO 提供给前端。
22. **请求级 settings 的 YAML 键是 `timeout`**（JSON 才是 `timeoutSec`）：核对落盘别按 JSON 名找。
23. **gzip 需要服务端配解压器**：客户端 `grpc.UseCompressor(gzip.Name)` 只带压缩标记，服务端没 import `encoding/gzip` 会回 `UNIMPLEMENTED: Decompressor is not installed for grpc-encoding "gzip"`（`cmd/grpcfixture` 已注册；真实服务端要自备，Options 分段的提示里也写了）。

## 11. 关键落点证据（现状）

| 主题 | 位置 |
|---|---|
| 请求内存模型 | `internal/collection/types.go:23-41` |
| 请求磁盘形态 + `Extra` 兼容 | `api-doc-go-share/collection/request.go:34-41,53` |
| `type` 分派（读） | `internal/collection/collection.go:484-495`（未知类型跳过：`:500`） |
| 集合扫描扩展名过滤 | `internal/collection/collection.go:456` |
| 集合外部改动监听 | `internal/collection/collection.go:114`、`internal/watch/watch.go:125-160`、`internal/app/app.go:177` |
| 执行器入口 / 结果 | `internal/runner/runner.go:81` / `:66-77` |
| 发送编排（含脚本与历史） | `internal/app/app.go:907,937-973,1038-1058` |
| 前端 IPC | `frontend/src/lib/ipc.ts:129-130,151` |
| 编辑器分段 | `frontend/src/components/RequestEditor.vue`（HTTP / gRPC 两套 `segments`） |
| 请求栏（HTTP 方法枚举 + gRPC 分派） | `frontend/src/components/RequestBar.vue`（`isGrpcReq` 分支） |
| gRPC 前端入口 | `frontend/src/components/Sidebar.vue`（工具栏「＋」下拉 / 分组菜单）、`frontend/src/components/TabBar.vue`（「＋」下拉）、`frontend/src/App.vue#newGrpcDraft` |
| gRPC 前端通用逻辑 | `frontend/src/lib/grpc.ts`（协议判定 / 发送门禁 / 服务·方法换算、`isGrpc`/`grpcOf`/`grpcSendBlocker`） |
| gRPC Schema 分段 | `frontend/src/components/GrpcSchemaPane.vue`、`frontend/src/stores/tabs.ts#loadGrpcSchema`（tab.grpcSchema / tab.grpcError） |
| gRPC Message 分段 | `frontend/src/components/GrpcMessagePane.vue`、`internal/proto/fields.go`（`Fields`/`ValidateMessage`）、`App.GrpcMessageFields`/`GrpcValidateMessage`/`GrpcSampleMessage` |
| gRPC Metadata 分段 | `frontend/src/components/GrpcMetadataPane.vue`（复用 `components/KeyValueTable.vue`） |
| gRPC 响应渲染 | `frontend/src/components/ResponsePanel.vue`（`isGrpcRes` 分派 +「响应尾」页签）、`frontend/src/lib/grpc.ts`（gRPC code 表） |
| gRPC 协议色 | `frontend/src/lib/method.ts`（`GRPC` → `--app-method-grpc`）、`frontend/src/styles/base.css` |
| 响应面板分段/状态语义 | `frontend/src/components/ResponsePanel.vue:25,100-110` |
| 状态码短语表 | `frontend/src/lib/format.ts:37-40` |
| 方法色 | `frontend/src/lib/method.ts:4-13` |
| 索引 / 历史 / 同步字段 | `internal/index/index.go:17-30`、`internal/history/history.go:18-30`、`internal/syncengine/engine.go:231-284` |
| 代码生成（共享包） | `api-doc-go-share/codegen/codegen.go:15-21,46-65` |
| 导出 Markdown | `internal/collection/export.go:182` |
| 服务端协议字段预留 | `api-doc-go/server/internal/model/api.go:14`、`server/internal/dto/dto.go:122` |
| 服务端跳过非 http | `api-doc-go/server/internal/service/bruno.go:68` |
| devserver 反射桥 | `cmd/devserver/main.go:87-136` |
