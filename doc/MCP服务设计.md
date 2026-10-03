# MCP 服务设计（cmd/mcpserver）

把 api-doc-go 集合（项目）暴露给外部 AI 工具（Claude Desktop / Cursor / Cline 等），
让 AI 能查接口、读详情、建请求、跑请求并留存响应。

> 想用它的人看《MCP服务使用手册.md》（装、挂载、10 个工具、排查）；本文只讲设计与取舍。

## 一、形态与启动

```bash
# stdio（本地 AI 工具挂载，默认）
mcpserver -root D:\collections

# 追加 streamable HTTP（可远程访问；与 stdio 同时提供）
mcpserver -root D:\collections -http 127.0.0.1:8189

# 只读模式：不注册写工具，send_request 强制不落盘
mcpserver -root D:\collections -readonly

# 跨主机（WSL2 / 局域网）：绑全网卡 + Bearer 令牌（不给则自动生成并打印）
mcpserver -root D:\collections -http 0.0.0.0:8189 -http-token <令牌>
```

| 参数 | 说明 |
|---|---|
| `-root` | 必填。项目根目录，其下**含 `opencollection.yml` 的子目录 = 一个项目** |
| `-http` | 留空 = 只走 stdio；给了地址则同时提供 streamable HTTP（路径 `/mcp`）。跨主机用 `0.0.0.0:8189`；只给端口（`:8189`）按全网卡处理 |
| `-http-token` | HTTP 的 Bearer 令牌。**监听非回环且未给时自动生成 32 位随机令牌并打印**（`crypto/rand`），每个 `/mcp` 请求都校验 |
| `-http-allow-origin` | 允许的浏览器来源（Origin 白名单，逗号分隔，`*` = 任意）。带 `Origin` 的请求必须命中，否则 403 |
| `-readonly` | 只注册只读工具；`send_request` 仍可用但不保存响应示例 |

**stdout 纪律**：stdio 传输下 stdout 属于 JSON-RPC 协议，所有日志一律走 stderr。
`-http` 模式下进程存活等信号，不等 stdio 返回（只开 HTTP 时 stdin 通常是关的）。

**依赖**：`github.com/modelcontextprotocol/go-sdk`（官方 SDK）。本机 `proxy.golang.org`
不可达，需用 `GOPROXY=https://goproxy.cn,direct go get github.com/modelcontextprotocol/go-sdk@v1.8.0`。

Claude Desktop 配置示例：

```json
{
  "mcpServers": {
    "api-doc-go": {
      "command": "D:\\path\\to\\mcpserver.exe",
      "args": ["-root", "D:\\collections"]
    }
  }
}
```

### 跨主机（WSL2 / 局域网）

| 项 | 设计 | 理由 |
|---|---|---|
| 鉴权 | 非回环监听且未给令牌时**自动生成并打印**；校验 `Authorization: Bearer <token>`，前缀大小写不敏感 + `subtle.ConstantTimeCompare` 定长比较 | 监听全网卡 = 同网段任意进程可调工具（能删请求），不设令牌等于把集合交出去 |
| 来源校验 | 带 `Origin` 的请求必须命中 `-http-allow-origin`（默认拒绝一切 Origin）；命中则回 CORS 头并处理 `OPTIONS` 预检。无 `Origin` 的非浏览器客户端不受限 | MCP 规范的 DNS rebinding 防护（恶意网页可诱导浏览器对内网地址发写请求）；顺带让浏览器端 MCP 客户端可用 |
| 健康检查 | `/healthz` 恒开、不鉴权，只回 `{"ok":true,"service","version"}` | 跨主机先探通端口再谈鉴权，且不泄露项目/路径 |
| 超时 | 只设 `ReadHeaderTimeout`，不设 `Read/WriteTimeout` | SSE 是长连接，写超时会掐断正常的流式会话 |
| WSL 连法 | mirrored 模式（Win11 22H2+，WSL 与 Windows 同网段）用 `127.0.0.1` 直连；NAT 模式用 `ip route show default` 的网关地址 | mirrored 下那条命令拿到的是路由器地址（实测本机 `192.168.31.1`），不是宿主 |

实测（`0.0.0.0:8194` + 自动令牌，WSL2 Debian）：无令牌 `401` → 带令牌握手拿到会话 → `tools/call list_projects`
返回项目清单；同机 `127.0.0.1` 可通、路由器地址不可通。详见手册第八节。

## 二、项目模型

- 一个项目 = 一个集合目录（含 `opencollection.yml`）。
- 三种定位方式（优先级从高到低）：**uid → 目录基名 → 路径**（绝对或相对 `-root`）；歧义时
  报错并列出候选。工具入参统一叫 `project`。
- **懒加载**：启动只扫描目录；首次访问某项目才 `OpenCollection`（避免为所有项目各起一个
  fsnotify 监听）。`list_projects` 里的请求/模块/环境统计来自缓存，`refresh=true` 重新扫描。
- 读操作前会 `ReloadCollection`（重扫 + 重建索引），因为集合的扫描与索引是「打开时快照」
  —— 与桌面端「写完就 reload」的做法一致。

## 三、工具清单（10 个）

| 工具 | 作用 | 关键入参 | 模糊匹配 |
|---|---|---|---|
| `list_projects` | 项目列表 | `project?`(按名/路径过滤)、`refresh?` | ✅ |
| `get_project_modules` | 项目目录（模块）+ 每模块请求数 | `project`、`query?`、`includeRequests?` | ✅ 模块名 + 模块下请求名/方法/路径 |
| `get_request_detail` | 接口详情：请求(method/url/params/body/auth/settings/grpc)、headers、说明文档、已保存响应示例 | `project`、`uid`、`query?`、`includeBody?`、`maxBodyBytes?` | ✅ 在示例名/文档名里再筛 |
| `search_requests` | 模糊搜索接口（可跨项目） | `query`、`project?`、`limit?` | ✅ 打分排序 |
| `create_request` | 创建请求 | `project`、`name`、`folder?` + (`method`+`url`+`headers`+`body` | `curl` | `copyFromUid`) | — |
| `create_module` | 创建模块（支持嵌套） | `project`、`name`、`parent?` | — |
| `create_project` | 新建项目（目录 + manifest） | `name`、`dirName?`、`root?` | — |
| `send_request` | 发送并可选保存响应示例 | `project`、`uid` 或 `method`+`url`、`env?`、`saveExample?`(默认 true)、`exampleName?`、`includeBody?`(默认 true)、`maxBodyBytes?` | — |
| `update_request` | 局部修改 / 移动模块 | `project`、`uid`、`name?`/`url?`/`method?`/`headers?`/`body?`/`docs?`/`folder?`、`ifMatch?` | — |
| `delete_request` | 移除请求（进 `.trash`，可找回） | `project`、`uid` | — |

## 四、模糊匹配规则

纯标准库实现（`internal/mcp/fuzzy.go`），两阶段：

1. **索引阶段**（不读文件）：对 `name` / `url` / `method` / `path` 打分。
   归一化（全角→半角、大小写折叠、去空白与 `/ . _ - ? & =`）；命中方式：全串子串 100 分 >
   分词命中（按词命中比例）30 分 > 有序子序列 8 分；再乘字段权重（name 10 / url 6 / method 4 /
   path 3 …）。中文按普通子串处理，不引分词/拼音依赖。
2. **深读阶段**：索引一个都没命中（query 可能命中 header / docs / body）时，读前 60 个候选
   文件补齐这三个字段后重打分（分数只升不降）。命中很多时也补一轮，让 header/docs 参与排序。

结果带 `score` 与 `matched`（命中字段列表），AI 可据此判断相关性再决定是否取详情。
同分按名称升序，保证结果稳定可复现。

## 五、输出裁剪（保护 AI 上下文）

- 响应体默认 **8 KiB** 上限（`maxBodyBytes`），超限明确写「已截断：共 N 字节，本次仅返回 M 字节」，
  **不静默丢弃**。
- 列表默认 50 条（`limit`，上限 200）。
- 工具结果统一是**可读文本**（不进结构化 JSON）：AI 工具链里文本更省 token、也更好读。
- 业务错误用 `isError: true` 返回（AI 能读到并纠正），不是协议级 error。

## 六、安全与正确性

| 边界 | 行为 |
|---|---|
| 路径/uid | 全部走集合层（已有 `..`/绝对路径拒绝与 uid 校验），不接受任意磁盘路径 |
| 删除 | 移入集合 `.trash/`，不真正删除 |
| 并发写 | 同项目的写操作由 registry 锁串行化，避免同时写同一文件 |
| 冲突保护 | `get_request_detail`/`search_requests` 返回 `hash`；`update_request` 传 `ifMatch` 时，磁盘被外部改动就拒写并提示「重新读取后合并，或另存为新请求」（不传则最后写者赢，与桌面端一致） |
| 只读模式 | 不注册 `create_*` / `update_*` / `delete_*`；`send_request` 强制 `saveExample=false` |

## 六之二、客户端内嵌（设置里配置）

除独立二进制外，客户端内嵌同一套服务，配置落在「全局设置 → MCP 服务」分区（`config.MCPConfig`：
`enabled` / `addr` / `port` / `token` / `readOnly` / `allowOrigins`），随其它设置一起落盘。

| 设计点 | 做法 | 理由 |
|---|---|---|
| 依赖方向 | `internal/mcp` 提供实现（`NewBackend`），`internal/app` 只持有 `MCPBackend` 接口，由 `main.go` / `cmd/devserver` 注入 | `internal/mcp` 需要 App 的集合运行时（打开/发送/保存），反向 import 会成环；改成接口后 `internal/mcp` 不再 import `internal/app`（`ProjectApp` 接口 + 工厂 `NewAppFunc`） |
| 生效时机 | `Startup` / `OpenCollection` / `SaveSettings` 三个钩子都调 `applyMCP` | 换集合要换项目根；保存设置要重启服务 |
| 端口占用 | 先 `net.Listen` 探一下再 `ServeHTTP` | 把「端口被占用」变成同步错误返回给设置页，否则监听错误发生在内部 goroutine 里，界面只能显示「未知错误」 |
| 令牌 | 启用且为空时自动生成 32 位随机令牌并落盘；「重新生成」先落盘再重启 | 开了就有鉴权，不裸奔；旧令牌立即失效 |
| 默认值 | 不启用、回环、8189、**只读** | 内嵌服务最容易被随手暴露到网络，默认只读更安全 |
| 项目根 | 当前打开集合的**父目录** | 与独立 mcpserver 的「项目 = 含 manifest 的目录」语义一致；同级集合一并对 AI 可见 |

## 七、分层

```
cmd/mcpserver/main.go     flag + 传输选择（stdio / HTTP）
internal/mcp/server.go    装配：NewServer / ServeStdio / ServeHTTP（唯一 import SDK 的入口层）
internal/mcp/tools.go     10 个 MCP 工具的注册与入参 schema（jsonschema tag）
internal/mcp/service.go   语义实现（不 import SDK，可单测/复用）
internal/mcp/registry.go  多项目 registry（懒打开、三种定位、统计缓存、创建项目）
internal/mcp/fuzzy.go     归一化 + 打分（子串/分词/子序列）
internal/mcp/render.go    裁剪与文本渲染
```

## 八、验证

- 单测：`go test ./internal/mcp/`（模糊打分、registry 扫描/定位/创建项目、只读三工具的
  端到端、冲突分支、发送并保存示例、裁剪、10 个工具注册与 schema 生成、只读模式差异）。
- stdio 冒烟：管道喂 `initialize` / `tools/list` / `tools/call`，验证建模块 → 建请求 →
  查目录 → 模糊搜索全链路。
- HTTP 冒烟：`initialize` 拿 `Mcp-Session-Id` → 会话内 `tools/call`；只读模式下调用
  `delete_request` 返回 `unknown tool`。
