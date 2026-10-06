# MCP 服务设计（cmd/mcpserver + 客户端内嵌）

把 api-doc-go 集合（项目）暴露给外部 AI 工具（Claude Desktop / Cursor / Cline 等），
让 AI 能查接口、读详情、建请求、跑请求并留存响应。

> 使用者看《MCP服务使用手册.md》；本文只讲设计与取舍。

## 一、形态与启动

```bash
mcpserver -root D:\collections                              # stdio（本地 AI 工具挂载，默认）
mcpserver -root D:\collections -http 127.0.0.1:8189         # 追加 streamable HTTP（与 stdio 同时提供）
mcpserver -root D:\collections -readonly                    # 只读：不注册写工具，send_request 不落盘
mcpserver -root D:\collections -http 0.0.0.0:8189 -http-token <令牌>   # 跨主机（WSL2 / 局域网）
```

| 参数 | 说明 |
|---|---|
| `-root` | 必填。其下**含 `opencollection.yml` 的子目录 = 一个项目** |
| `-http` | 留空 = 只走 stdio；给了地址则同时提供 streamable HTTP（路径 `/mcp`）。只给端口（`:8189`）按**回环**处理，跨主机必须显式写 `0.0.0.0:8189` |
| `-http-token` | HTTP 的 Bearer 令牌。**监听非回环且未给时，main 直接拒绝启动**；客户端内嵌服务在启用且为空时自动生成并落盘 |
| `-http-allow-origin` | 浏览器来源白名单（逗号分隔，`*` = 任意）。带 `Origin` 的请求必须命中，否则 403 |
| `-readonly` | 只注册只读工具；`send_request` 仍可用但不保存响应示例 |

**stdout 纪律**：stdio 下 stdout 属于 JSON-RPC 协议，日志一律走 stderr。`-http` 模式下进程存活等信号，不等 stdio 返回。

**依赖**：官方 `github.com/modelcontextprotocol/go-sdk`。若 `proxy.golang.org` 不可达：
`GOPROXY=https://goproxy.cn,direct go get github.com/modelcontextprotocol/go-sdk@v1.8.0`。

Claude Desktop 配置：

```json
{ "mcpServers": { "api-doc-go": { "command": "D:\\path\\to\\mcpserver.exe", "args": ["-root", "D:\\collections"] } } }
```

### 跨主机（WSL2 / 局域网）

| 项 | 设计 | 理由 |
|---|---|---|
| 鉴权 | 校验 `Authorization: Bearer <token>`：前缀大小写不敏感 + `subtle.ConstantTimeCompare` 定长比较 | 监听全网卡 = 同网段任意进程可调工具（含删除），不设令牌等于把集合交出去 |
| 来源校验 | 带 `Origin` 的请求必须命中白名单（默认拒绝一切 Origin）；命中则回 CORS 头并处理 `OPTIONS` | MCP 规范的 DNS rebinding 防护；顺带让浏览器端客户端可用。无 `Origin` 的非浏览器客户端不受限 |
| 健康检查 | `/healthz` 恒开、不鉴权，只回 `{"ok":true,"service","version"}` | 先探通端口再谈鉴权，且不泄露项目/路径 |
| 超时 | 只设 `ReadHeaderTimeout` | SSE 是长连接，写超时会掐断正常会话 |
| WSL 连法 | mirrored（Win11 22H2+）用 `127.0.0.1`；NAT 用 `ip route show default` 的网关地址（mirrored 下拿到的是路由器） | 见手册第八节 |

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

## 三、工具清单（18 个）

| 工具 | 作用 | 关键入参 |
|---|---|---|
| `list_workspaces` | 授权的工作目录 + 其中项目 + 当前默认目标 | — |
| `use_workspace` | 选定**本会话**的默认工作目录（选定后可省略 `project`） | `workspace` |
| `list_projects` | 项目列表（模糊过滤/刷新） | `project?`、`refresh?` |
| `get_project_modules` | 模块结构 + 每模块请求数 | `project?`、`query?`、`includeRequests?` |
| `get_request_detail` | 接口详情（请求/headers/docs/示例） | `project?`、`uid`、`query?`、`includeBody?`、`maxBodyBytes?` |
| `search_requests` | 模糊搜索（可跨项目） | `query`、`project?`、`limit?` |
| `create_request` | 创建请求（手填 / cURL / 复制） | `project?`、`name`、`folder?` + (`method`+`url`+…) / `curl` / `copyFromUid` |
| `create_module` | 创建模块（支持嵌套） | `project?`、`name`、`parent?` |
| `create_project` | 新建项目（目录 + manifest） | `name`、`dirName?`、`root?` |
| `send_request` | 发送并可选保存示例 | `project?`、`uid` 或 `method`+`url`、`env?`、`saveExample?`、`includeBody?`、`maxBodyBytes?` |
| `update_request` | 局部修改 / 移动模块 | `project?`、`uid`、各可选字段、`ifMatch?` |
| `delete_request` | 移除请求（进 `.trash`） | `project?`、`uid` |
| `list_envs` | 环境与变量（敏感值掩码 + hash） | `project?`、`query?` |
| `create_env` | 新建环境（可带初始变量） | `project?`、`name`、`vars?` |
| `rename_env` | 环境改名 | `project?`、`name`、`newName`、`ifMatch?` |
| `delete_env` | 删除环境 | `project?`、`name` |
| `set_env_var` | 单条变量 upsert | `project?`、`env`、`name`、`value`、`secret?`、`enabled?`、`ifMatch?` |
| `delete_env_var` | 删一个变量 | `project?`、`env`、`name`、`ifMatch?` |

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

## 七、安全与正确性

| 边界 | 行为 |
|---|---|
| 路径/uid | 全部走集合层（`..`/绝对路径拒绝、uid 校验、定义文件必须在集合内），不接受任意磁盘路径 |
| 删除 | 移入集合 `.trash/`，不真正删除 |
| 并发写 | 同项目写操作由 registry 锁串行化 |
| 冲突保护 | 详情/搜索返回 `hash`；`update_request` 传 `ifMatch` 时磁盘被外部改动就拒写（不传则最后写者赢，与桌面端一致） |
| 只读模式 | 不注册 `create_*`/`update_*`/`delete_*`；`send_request` 强制 `saveExample=false` |
| 工作目录边界 | 只扫 / 只操作**白名单**目录（路径比较按绝对路径 + 软链复核，拒绝 `..` 逃逸）；`create_project` 只能在白名单内的可写目录里建（以前 `root` 参数可以指向任意路径 = 越权） |
| 授权粒度 | 每条白名单独立只读/可写：只读授权下写工具直接拒绝，`send_request` 的示例也不落盘 |

## 八、客户端内嵌（设置 → MCP 服务）

配置项 `config.MCPConfig`：`enabled` / `addr` / `port` / `token` / `readOnly` / `allowOrigins`，随设置一起落盘。

| 设计点 | 做法 | 理由 |
|---|---|---|
| 依赖方向 | `internal/mcp` 提供实现（`NewBackend`），`internal/app` 只持有 `MCPBackend` 接口，由 `main.go`/`devserver` 注入 | `internal/mcp` 需要 App 的集合运行时，反向 import 会成环；故抽象 `ProjectApp` 接口 + `NewAppFunc` 工厂 |
| 生效时机 | `Startup` / `OpenCollection` / `SaveSettings` 都调 `applyMCP` | 换集合要换项目根；保存设置要重启服务 |
| 端口占用 | 先 `net.Listen` 探一下再 `ServeHTTP` | 把「端口被占用」变成同步错误返回设置页 |
| 令牌 | 启用且为空时自动生成 32 位随机令牌并**落盘**；「重新生成」先落盘再重启 | 开了就有鉴权；不落盘会导致切主题/重启后令牌静默轮换、已连上的客户端 401 |
| 默认值 | 不启用、回环、8189、**只读** | 内嵌服务最容易被随手暴露 |
| 项目根 | **可访问的工作目录白名单**（设置里逐条添加并勾选只读/可写） | 以前取「当前集合父目录」等于隐式授权一整棵目录树；现在由用户显式授权。白名单为空则服务不监听（安全默认），设置页有「加入当前打开目录」一键添加 |

## 九、分层

```
cmd/mcpserver/main.go     flag + 传输选择（stdio / HTTP）
internal/mcp/server.go    装配：NewServer / ServeStdio / ServeHTTP（唯一 import SDK 的入口层）
internal/mcp/tools.go     16 个工具的注册与入参 schema（jsonschema tag）
internal/mcp/service.go   语义实现（不 import SDK，可单测/复用）
internal/mcp/registry.go  多项目 registry（只读扫描、懒打开、三种定位、创建项目）
internal/mcp/env.go       环境变量工具（掩码、hash、写保护）
internal/mcp/fuzzy.go     归一化 + 打分
internal/mcp/render.go    裁剪与文本渲染
internal/mcp/embed.go     客户端内嵌服务的启停实现
```

## 十、验证

- 单测：`go test ./internal/mcp/`（打分、registry 扫描/定位/创建、环境变量读写与掩码回传、冲突分支、发送并保存示例、裁剪、工具注册与 schema、只读模式差异）。
- stdio 冒烟：管道喂 `initialize` / `tools/list` / `tools/call`，验证「建模块 → 建请求 → 查目录 → 模糊搜索」全链路。
- HTTP 冒烟：`initialize` 拿 `Mcp-Session-Id` → 会话内 `tools/call`；只读模式下调 `delete_request` 返回 `unknown tool`。
