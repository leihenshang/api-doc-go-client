# MCP 服务使用手册

面向**用 AI 工具操作接口集合**的人：怎么装、怎么挂、每个工具能干什么、遇到报错怎么办。
设计原理与分层见《MCP服务设计.md》；客户端内嵌服务见文末《附》。

## 一、它能做什么

把 api-doc-go 的**集合目录**（项目）接给 AI（Claude Desktop / Cursor / Cline 等），
让 AI 查接口、读详情、建请求、跑请求、管理环境变量，并把响应留存成示例。共 **16 个工具**：

| 类别 | 工具 | 一句话 |
|---|---|---|
| 查询 | `list_projects` | 有哪些项目（集合） |
| 查询 | `get_project_modules` | 某项目有哪些模块，每个模块下有哪些请求 |
| 查询 | `get_request_detail` | 某接口的完整详情 + 已保存的响应示例（返回 `hash` 供冲突保护） |
| 查询 | `search_requests` | 模糊搜索（「记得内容、记不清名字」时最该用） |
| 查询 | `list_envs` | 有哪些环境与变量（**敏感值只回掩码**，每个环境带 `hash`） |
| 写 | `create_request` | 新建请求（手填 / cURL 导入 / 复制现有） |
| 写 | `create_module` | 新建模块（目录，支持嵌套） |
| 写 | `create_project` | 新建项目（集合目录 + manifest） |
| 写 | `update_request` | 改请求 / 移动模块（支持 `ifMatch` 冲突保护） |
| 写 | `delete_request` | 移除请求（文件进 `.trash`，可找回） |
| 执行 | `send_request` | 发请求；默认把响应存成响应示例 |
| 写 | `create_env` / `rename_env` / `delete_env` | 环境的新建 / 改名 / 删除（旧文件进 `.trash`） |
| 写 | `set_env_var` / `delete_env_var` | 单个变量的 upsert / 删除（敏感值回填掩码 = 不改） |

各工具的完整入参与示例见「五、工具详解」；参数速查见「十一、命令与参数速查」。

> **敏感值（`secret: true`）的值永远只回掩码 `••••••`**，这是有意的。轮换就把新值传给 `set_env_var`；
> 不想动它就把掩码原样传回（见 5.10）。
>
> AI 侧看到的返回是一段**可读文本**（不是结构化 JSON），超大内容会被裁剪并明确标注（见「六、输出裁剪」）。

## 二、快速开始

### 1. 准备项目根目录

先把要交给 AI 的目录**授权**给它（白名单）：`-allow-writable D:\collections`（可写）或
`-allow D:\collections`（只读）；客户端内嵌服务在「设置 → MCP 服务 → 可访问的工作目录」里添加。
**白名单之外一律拒绝**（读不到、也写不了）；没授权任何目录时客户端内嵌服务不会启动。

「项目」= 一个集合目录 = **含 `opencollection.yml` 的目录**。把若干集合放进同一个父目录：

```
D:\collections\                 ← 这个就是 -root
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
{ "mcpServers": { "api-doc-go": { "command": "D:\\tools\\mcpserver.exe", "args": ["-root", "D:\\collections"] } } }
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
| `-http-token` | ❌ | HTTP 的 Bearer 令牌。**监听非回环地址时必须给**（不给直接拒绝启动，避免把集合暴露给同网段任意进程） |
| `-http-allow-origin` | ❌ | 允许的浏览器来源（Origin），逗号分隔，`*` = 任意。仅浏览器端客户端需要，同时是 DNS rebinding 防护 |
| `-readonly` | ❌ | 只读模式：不注册写工具；`send_request` 仍可用但**不保存**响应示例 |

两个约定：**stdio 下 stdout 属于 JSON-RPC 协议**（日志一律走 stderr）；`-http` 模式下进程**等信号退出**，
不会因为 stdin 关闭而退出。

## 三、项目模型（搞懂三条就够用）

0. **只有被授权的工作目录（白名单）可见**：`-allow` / `-allow-writable`，或客户端设置里的白名单。
   白名单为空 = 没有任何项目 —— 这是安全默认，不是故障。先用 `list_workspaces` 看授权了什么，
   再用 `use_workspace` 选定默认目标（之后各工具的 `project` 都可以省略）。这个默认目标是**按会话**的：
   同一次连接里选定即可，多个客户端（HTTP 下多个会话）互不影响。
1. **一个项目 = 一个集合目录**，由授权目录扫描得到；目录变化后用 `list_projects` 的 `refresh: true` 重扫。
2. **三种方式指定项目**（入参都叫 `project`）：**uid** → **目录名/集合名** → **路径**（绝对或相对 `-root`）。写错会报错并列出候选。
3. **懒加载**：启动只扫目录（只读 manifest，不改动项目），第一次访问某项目才真正打开。因此
   `search_requests` 不带 `project` 时**只搜「已加载」的项目** —— 新项目先 `get_project_modules` 或
   `list_projects`（不带 query）打开一次。

读操作前会自动重扫该项目的集合与索引，所以刚创建的请求立刻能被搜到。

## 四、工具详解

> 下表「参数」列均为 JSON 字段名；标 ✅ 的是必填。`project?` 表示**可省略**：
> 省略时用 `use_workspace` 选定的**本会话**默认工作目录（没选过则报错并提示）。

### 4.0 `list_workspaces` / `use_workspace`

- `list_workspaces`：列出**授权的工作目录**（含只读/可写）、其中项目与当前默认目标；AI 拿不到项目时先看它。
- `use_workspace`：选定默认工作目录（传工作目录 path，或项目标识：名称/路径/uid）。选定后其余工具的
  `project` 都可省略（**按会话生效**：同一次连接内有效，不会改到别的客户端）；不在白名单内会直接拒绝。
  未选定且未传 `project` 时，工具会报错并提示用 `use_workspace` 或显式传 `project`。

### 4.1 `list_projects`
`project?`（按名/路径模糊过滤）、`refresh?`（重新扫描 `-root`）。
返回每个项目的 `路径/名称/uid/请求数/模块数/环境列表`；不可用目录带跳过原因。统计在**首次访问该项目后**才有值（懒加载）。

### 4.2 `get_project_modules`
`project?`、`query?`（过滤模块名与模块下请求名/方法/路径）、`includeRequests?`（附带请求清单含 uid）。
返回模块树 + 每模块请求数/子模块数；根目录下的请求归到 `(根目录)`。

### 4.3 `get_request_detail`
`project?`、`uid` ✅、`query?`（在示例名/文档名里再筛）、`includeBody?`、`maxBodyBytes?`（默认 8192）。
返回 method/url/params/headers/body/auth/settings/grpc + 请求内 `docs` + 关联的 `docs/*.md` + **已保存响应示例列表**
+ **`hash`**（演示为前 12 位）——改这个接口时把它作为 `update_request.ifMatch` 传入即可获得冲突保护。

### 4.4 `search_requests`
`query` ✅（名称/URL/方法/路径/header/说明/请求体片段都能命中，中文按子串）、`project?`（省略 = 所有已加载项目）、
`limit?`（默认 50，上限 200）。
返回按相关度降序，每条带 `score` 与 `matched`（命中的字段）、`uid`/`path`/`hash`。

### 4.5 `create_request`
三种来源（互斥，按顺序判断）：

| 方式 | 参数 | 说明 |
|---|---|---|
| 手填 | `name` ✅ + `method`/`url`/`headers`/`params`/`bodyType`+`bodyRaw`/`auth`/`docs`/`folder` | `{"project":"shop","folder":"api/user","name":"login","method":"POST","url":"{{host}}/api/login"}` |
| cURL 导入 | `name` ✅ + `curl` | `{"project":"shop","name":"from-curl","curl":"curl -X POST https://x/api/login -H 'Content-Type: application/json' -d '{\"u\":\"a\"}'"}` |
| 复制现有 | `name` ✅ + `copyFromUid` | `{"project":"shop","name":"login-copy","copyFromUid":"<uid>"}` |

同名在分组内**自动加序号**（不覆盖）；`headers`/`params` 元素为 `{"name":..,"value":..,"enabled":true}`；
`bodyType` 取 `none`/`raw`/`form`；返回落盘后的 `uid` 与 `path`。

### 4.6 `create_module` / `create_project`
- `create_module`：`project?` + `name` ✅（+ `parent` 父模块路径，留空 = 根），支持嵌套（`parent=api` 建 `api/user`）。
- `create_project`：`name` ✅（manifest 显示名）+ `dirName`（目录名，留空 = 与 name 相同）+ `root`（留空 = `-root`）。建完 `list_projects` 立刻可见。

### 4.7 `send_request`
`project?` +（`uid` 或 `method`+`url`）、`env?`（留空 = 第一个环境）、`saveExample?`（默认 true）、
`exampleName?`（默认 `"{状态码} 响应"`）、`includeBody?`（不传 = 返回体）、`maxBodyBytes?`。
临时请求（给 `method`+`url`）**不落盘也不存示例**；存下的示例客户端响应面板「本次响应 ▾」能直接回看。

### 4.8 `update_request`
`project?`、`uid` ✅，以及**只改传入的** `name`/`url`/`method`/`headers`/`body`/`auth`/`docs`，`folder` 移动到别的模块。
`ifMatch` 传详情里的 `hash`（12 位前缀即可，服务端按 git 风格前缀比对，≥8 位有效）→ 磁盘文件在此期间被外部改动就
**拒绝写入**；**不传 = 最后写者赢**（与桌面端一致）。改名会自动同步文件名。

### 4.9 `delete_request`
`project?` + `uid` ✅。文件**移入集合的 `.trash/`**（带时间戳），不会真正删除。
误删恢复：把文件从 `.trash/` 挪回原位并 `list_projects(refresh: true)`。

### 4.10 环境变量工具（6 个）

环境 = `environments/<env>.yml`，请求里用 `{{变量名}}` 引用。

| 工具 | 参数 | 说明 |
|---|---|---|
| `list_envs` | `project?`、`query?` | 列出环境与变量；`query` 对环境名与变量名都过滤；每个环境带 `hash` |
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

只读模式（`-readonly`）下 AI 只能查 + 试发（不落盘），适合放行给「只答疑/查文档」的机器人。

## 六、输出裁剪（保护 AI 上下文）

| 内容 | 默认 | 怎么调 |
|---|---|---|
| 发送响应体 | 裁到 **8192 字节** | `maxBodyBytes`；`includeBody=false` 则完全不返回体 |
| 列表条数 | **50** | `limit`（上限 200） |
| 详情里的示例响应体 | 不返回 | `get_request_detail(includeBody=true, maxBodyBytes=…)` |

被裁剪时返回文本会**明确写**「已截断：共 N 字节，本次仅返回 M 字节」，不会静默丢弃。

## 七、跨主机与 WSL

AI 工具跑在 WSL2、集合放在 Windows（或反过来）时，**不要**在 WSL 里直接跑 mcpserver 读 `/mnt/c/...`
（原因见 7.4），而是 Windows 侧跑服务、WSL 侧走 HTTP。

### 7.1 两步启动

```bash
# Windows 侧：绑 0.0.0.0 并显式给令牌
mcpserver.exe -root D:\collections -http 0.0.0.0:8189 -http-token my-secret-token
```

### 7.2 WSL 侧连哪个地址

| WSL 网络模式 | 怎么判断 | WSL 里填的地址 |
|---|---|---|
| **mirrored**（推荐，Win11 22H2+ / WSL 2.0+ 默认） | WSL 与 Windows 同一网段 | `http://127.0.0.1:8189/mcp` ✅ 直接通 |
| **NAT**（老模式） | WSL 在 `172.x` 虚拟网段 | `http://<宿主IP>:8189/mcp`，宿主 IP 用 `ip route show default` 的网关地址 |

> ⚠️ mirrored 模式下**别用 `ip route show default` 的结果** —— 那拿到的是路由器地址（实测本机是
> `192.168.31.1`），不是 Windows 宿主，连不上。

先探通再谈业务：`curl http://127.0.0.1:8189/healthz` → `{"ok":true,...}`（不需要令牌）。

### 7.3 客户端配置与自检

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

### 7.4 为什么不在 WSL 里直接跑服务读 `/mnt/c/...`

`/mnt/c`（drvfs/9p）**不产生可靠的 inotify 事件** → 外部改动监听与索引刷新基本失效，冲突保护要靠手动重读兜底，
跨文件系统的重命名/锁语义也更容易出怪问题（尤其 `.trash` 移动）。要本地跑就把集合放在 WSL 原生文件系统
（`~/collections`）并保证只有一个写者；否则用上面的 HTTP 方式。

## 八、排查

| 现象 | 原因 / 处理 |
|---|---|
| AI 说「找不到工具 / 服务器未连接」 | `command` 路径错、没重启 AI 工具、`-root` 没传（必填，会直接退出） |
| `list_projects` 空但目录明明在 | 该子目录**缺 `opencollection.yml`**；补好后 `refresh: true` 重扫 |
| 提示「这是 Bruno 集合目录」 | Bruno 格式需先用客户端「导入」转换 |
| 提示「找不到项目」/「匹配到多个项目」 | `project` 支持 uid/名称/路径；歧义时用返回的候选（推荐 `path`） |
| `search_requests` 少了某些项目 | 不带 `project` 只搜**已加载**项目；先 `get_project_modules` 打开它 |
| 保存被拒「文件已被外部修改」 | 读取之后文件被改过。重新 `get_request_detail` 合并后重试或另存新请求。**偶发误报**：文件监听重建索引有几百毫秒延迟，重新读一次即可 |
| 发送失败 | 地址不通/超时（默认 30s，受全局 `timeoutSec` 约束）/证书问题（`insecureSsl`） |
| 写操作提示 `unknown tool` | 用了 `-readonly`，写工具没注册 |
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

## 九、安全清单

| 项 | 说明 |
|---|---|
| 能做什么 | 在 `-root` 下的集合里读写请求、建模块/项目、发请求、删请求（进 `.trash`） |
| 不会做什么 | 不碰 `-root` 之外的路径（集合层拒绝绝对路径与 `..`）；不执行任意命令；不自动提交 git |
| 写保护 | `update_request` 支持 `ifMatch`；删除只进 `.trash`；环境写操作支持 `ifMatch` |
| 远程暴露 | 监听非回环**必须**带令牌（独立二进制不给就拒绝启动；内嵌服务自动生成并落盘），每个请求都校验；`/healthz` 免令牌且只回版本号 |
| 防火墙 | 首次启动弹窗选「专用网络」；或管理员 PowerShell：`New-NetFirewallRule -DisplayName mcpserver -Direction Inbound -LocalPort 8189 -Protocol TCP -Action Allow -Profile Private` |
| 暴露面 | 只在受信网络这么做；不要直接映射公网，需要就 `ssh -L 8189:127.0.0.1:8189 user@host` |
| 建议 | ① 对外机器人用 `-readonly`；② 让 AI 指向 **git 工作副本**，改动可 review/回滚；③ 响应示例会进版本库，注意敏感数据 |

## 十、与桌面客户端的关系

- 两者操作**同一份集合文件**：AI 建的请求/示例，客户端里立刻能看到（外部改动检测会提示冲突）。
- 客户端独有：UI 编辑、变量书签、cURL 导入面板、gRPC 定义导入与 Mock 面板、同步绑定面板。
- MCP 独有：跨项目模糊搜索、脚本化批量操作、给其它 AI 复用。
- 推荐分工：AI 负责查、批量整理、跑回归并存示例；人工在客户端精修与提交。

## 附：用客户端内置的 MCP 服务（不用单独跑 mcpserver.exe）

客户端内置同一套服务，在「全局设置 → MCP 服务」分区配置（适合「AI 与客户端同机、想一键开关」）。

| 字段 | 说明 |
|---|---|
| 启用内嵌 MCP 服务 | 总开关；关闭时不监听任何端口 |
| 监听地址 | `127.0.0.1（仅本机）` / `0.0.0.0（跨主机）` |
| 端口 | 默认 8189，可改（1024–65535） |
| 只读模式 | 默认开：AI 只能查询与试发 |
| 浏览器来源 | Origin 白名单，逗号分隔；留空 = 拒绝一切浏览器来源 |
| 访问令牌 | 启用且为空时**自动生成并落盘**（重启/改设置不会变）；「重新生成」立即换新（旧令牌失效） |
| 状态行 | 运行中显示端点 URL 与可见项目数；失败显示原因（如端口被占用） |

保存后立即生效；「复制连接配置」把地址与令牌一起复制（跨主机做法与第七节相同）。

| | 客户端内嵌服务 | 独立 `mcpserver.exe` |
|---|---|---|
| 配置位置 | 设置界面 | 命令行参数 |
| 项目根 | **当前打开集合的父目录**（同级集合一并对 AI 可见） | `-root` |
| 生命周期 | 随客户端启停 | 独立进程 |
| 默认只读 | 开 | 关 |
| 多开 | 一个客户端一个实例 | 可多实例（注意端口冲突） |

配置存在 `<用户配置目录>/api-doc-client/config.json` 的 `mcp` 段。跨主机时记得把**只读模式关掉**才能让 AI 建/改接口。

## 十一、命令与参数速查

```bash
# 构建（首次需拉 SDK，注意代理）
GOPROXY=https://goproxy.cn,direct go build -o mcpserver.exe ./cmd/mcpserver

# 启动
mcpserver.exe -root D:\collections                        # stdio（AI 挂载）
mcpserver.exe -root D:\collections -http 127.0.0.1:8189   # + HTTP /mcp
mcpserver.exe -root D:\collections -readonly              # 只读
mcpserver.exe -root D:\collections -http 0.0.0.0:8189 -http-token <令牌>   # 跨主机

# 调试：手工发 JSON-RPC 看服务是否活着（stdio 需保持管道打开）
printf '%s\n' \
 '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}' \
 '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
 '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
 | mcpserver.exe -root D:\collections
```

**默认约定回顾**：示例默认保存、响应体裁剪 8192 字节、列表 50/200 条、敏感值只回掩码（掩码回传 = 不改，`.`/`?` 不算掩码）、
省略 `enabled` 即启用、写操作建议带 `ifMatch`、删除进 `.trash`、`-root` 下含 `opencollection.yml` 的子目录 = 项目、
`project` 三种写法（uid / 名称 / 路径）。
