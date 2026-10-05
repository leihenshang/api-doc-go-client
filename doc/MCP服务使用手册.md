# MCP 服务使用手册（cmd/mcpserver）

面向**用 AI 工具操作接口集合**的人：怎么装、怎么挂、每个工具能干什么、遇到报错怎么办。
设计原理与实现分层见同目录《MCP服务设计.md》。

---

## 一、它能做什么

把 api-doc-go 的**集合目录**（项目）接给 AI（Claude Desktop / Cursor / Cline 等），
让 AI 直接查接口、读详情、建请求、跑请求、管理环境变量，并把响应留存成示例。共 **16 个工具**：

| 类别 | 工具 | 一句话 |
|---|---|---|
| 查询 | `list_projects` | 有哪些项目（集合） |
| 查询 | `get_project_modules` | 某个项目有哪些模块（目录），每个模块下有哪些请求 |
| 查询 | `get_request_detail` | 某个接口的完整详情：请求 / headers / 说明 / 已保存的响应示例 |
| 查询 | `search_requests` | 模糊搜索：「大概记得内容，记不清名字」 |
| 查询 | `list_envs` | 有哪些环境、每个环境里有哪些变量（**敏感值只回掩码**） |
| 写 | `create_request` | 新建请求（手填 / cURL 导入 / 复制现有） |
| 写 | `create_module` | 新建模块（目录，支持嵌套） |
| 写 | `create_project` | 新建项目（集合目录 + manifest） |
| 执行 | `send_request` | 发请求；默认把响应存成响应示例 |
| 写 | `update_request` | 改请求 / 移动模块（带冲突保护） |
| 写 | `delete_request` | 移除请求（文件进 `.trash`，可人工找回） |
| 写 | `create_env` | 新建环境（可同时写入初始变量） |
| 写 | `rename_env` | 给环境改名（敏感值一起搬，旧文件进 `.trash`） |
| 写 | `delete_env` | 删除环境（文件进 `.trash`，可人工找回） |
| 写 | `set_env_var` | 新增/更新**单个**变量（按名 upsert；敏感值回填掩码 = 不改） |
| 写 | `delete_env_var` | 删掉某个变量 |

> **敏感值（`secret: true`）的值永远只回掩码 `••••••`**，AI 拿不到真实密钥 —— 这是有意的。
> 轮换密钥就把新值传给 `set_env_var`；不想动它就把掩码原样传回（见 5.11）。

**AI 侧看到的返回**是一段可读文本（不是结构化 JSON），超大内容会被裁剪并明确标注 ——
细节见「六、输出裁剪」。

---

> **不想单独跑二进制？** 客户端本身内置了同一个 MCP 服务：设置 →「MCP 服务」分区里配置监听地址与端口即可，
> 见文末《附：客户端内置的 MCP 服务》。

## 二、快速开始

### 1. 准备项目根目录

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

> 目录里没有 `opencollection.yml` 会被跳过；Bruno 集合（有 `bruno.json`）也会跳过，
> 并在 `list_projects` 里说明原因（需先用客户端的「导入」功能转换）。

### 2. 构建

```bash
cd api-doc-go-client
GOPROXY=https://goproxy.cn,direct go build -o mcpserver.exe ./cmd/mcpserver
```

> **依赖**：`github.com/modelcontextprotocol/go-sdk`（官方 MCP Go SDK，v1.8.0）。
> 部分网络下 `proxy.golang.org` 不可达，请用 `goproxy.cn`（或你惯用的代理）拉取。

### 3. 挂到 AI 工具

以 Claude Desktop 为例（其他工具把同样的「命令 + 参数」填进各自的 MCP 配置即可）：

```json
{
  "mcpServers": {
    "api-doc-go": {
      "command": "D:\\tools\\mcpserver.exe",
      "args": ["-root", "D:\\collections"]
    }
  }
}
```

- **Cursor**：项目内 `.cursor/mcp.json`，字段同上。
- **Cline**：`MCP Servers` 设置页填 Command / Arguments。
- 配置后**重启该 AI 工具**（stdio 型 MCP 在进程启动时握手）。

### 4. 确认挂上了

在 AI 里问一句：**「列出我的接口项目」**。正常会看到 `list_projects` 的调用与项目清单；
若 AI 报「找不到工具 / 服务器未连接」，看第九节「排查」。

---

## 三、启动参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `-root` | ✅ | 项目根目录，其下含 `opencollection.yml` 的子目录即项目 |
| `-http` | ❌ | 留空 = 只走 stdio；给地址（如 `127.0.0.1:8189`）则**同时**提供 streamable HTTP（路径 `/mcp`） |
| `-http-token` | ❌ | HTTP 的 Bearer 令牌。**监听非回环地址时不给会自动生成并打印**（建议手动指定，便于固化到客户端配置） |
| `-http-allow-origin` | ❌ | 允许的浏览器来源（Origin），逗号分隔，`*` = 任意。仅浏览器端客户端需要，同时是 DNS rebinding 防护 |
| `-readonly` | ❌ | 只读模式：不注册 `create_*` / `update_request` / `delete_request`；`send_request` 仍可用但**不保存**响应示例 |

```bash
# 本地 AI 挂载（最常用）
mcpserver.exe -root D:\collections

# 顺便开一个 HTTP 口（远程/容器场景）
mcpserver.exe -root D:\collections -http 127.0.0.1:8189

# 给「只想让它看」的团队成员/机器人开只读口
mcpserver.exe -root D:\collections -readonly

# 跨主机（WSL2 / 局域网另一台机器）：绑全网卡 + 令牌
mcpserver.exe -root D:\collections -http 0.0.0.0:8189 -http-token <你的令牌>
```

两个常见用法约定：

- **stdio 模式**下 stdout 属于 JSON-RPC 协议，**不要**把日志打到 stdout（本服务日志统一走 stderr）；
- `-http` 模式下进程**等信号退出**，不会因为 stdin 关闭而退出（只开 HTTP 时 stdin 通常是关的）。

---

## 四、项目模型（搞懂这三条就够用）

1. **一个项目 = 一个集合目录**，由 `-root` 扫描得到；目录变化后用 `list_projects` 的 `refresh: true` 重扫。
2. **三种方式指定项目**（工具入参都叫 `project`）：**uid** → **目录名/集合名** → **路径**（绝对或相对 `-root`）。写错会报错并列出候选。
3. **懒加载**：启动只扫目录，第一次访问某项目才真正打开。所以
   `search_requests` 不带 `project` 时**只搜「已加载」的项目** —— 新的项目先 `get_project_modules` 或 `list_projects`（不带 query）打开一次。

另外：**读操作前会自动重扫集合与索引**，所以刚创建的请求立刻就能被搜到，不用手动刷新。

---

## 五、工具详解

> 入参名均为 JSON 字段（下方表格里的「参数」）。带 `-` 的是必填。

### 5.1 `list_projects` —— 项目列表

| 参数 | 说明 |
|---|---|
| `project` | 按项目名或路径**模糊过滤**；留空返回全部 |
| `refresh` | `true` = 重新扫描 `-root`（新增/删除了项目目录时用） |

返回：每个项目的 `路径 / 名称 / uid / 请求数 / 模块数 / 环境列表`；不可用的目录会带跳过原因。
项目统计在**首次访问该项目后**才有值（懒加载），刚启动时是 0，属正常。

### 5.2 `get_project_modules` —— 项目目录（模块）

| 参数 | 说明 |
|---|---|
| `project` | 项目路径 / 名称 / uid |
| `query` | 对**模块名**与模块下**请求名 / 方法 / 路径**做模糊过滤 |
| `includeRequests` | `true` = 附带每个模块下的请求清单（含 uid） |

返回：模块树 + 每模块请求数/子模块数；根目录下的请求归到 `(根目录)`。
典型用法：先 `list_projects` 拿项目 → 再本工具拿到「模块 + 请求 uid」清单。

### 5.3 `get_request_detail` —— 接口详情

| 参数 | 说明 |
|---|---|
| `project` | 项目 |
| `uid` | 接口 uid（由 `search_requests` / `get_project_modules` 得到） |
| `query` | 在**示例名 / 文档名**里再筛一遍 |
| `includeBody` | `true` = 返回响应示例的响应体与请求体全文（默认**不返回**） |
| `maxBodyBytes` | 响应体裁剪上限，默认 8192 |

返回：method/url/params/headers/body/auth/settings/grpc + 请求内 `docs` + 集合里关联的 `docs/*.md` + **已保存响应示例列表**（名称/状态码/耗时/大小/路径）。
**返回一个 `hash`**（磁盘内容哈希，展示为前 12 位）—— 改这个接口时把它作为 `update_request.ifMatch` 传入即可获得冲突保护（服务端按 git 风格前缀比对，≥8 位即可，见 5.9）。

### 5.4 `search_requests` —— 模糊搜索

| 参数 | 说明 |
|---|---|
| `query` | 关键词：名称 / URL / 方法 / 路径 / header / 说明 / 请求体片段都能命中（中文按子串匹配） |
| `project` | 限定项目；**省略 = 所有已加载项目** |
| `limit` | 返回条数上限，默认 50，最大 200 |

返回：按相关度降序，每条带 `score`（分数）与 `matched`（命中了哪些字段：name/url/header/docs…），
外加 `uid` / `path` / `hash`。**这是「不记得名字」时最该用的工具。**

### 5.5 `create_request` —— 新建请求

三种来源（互斥，按顺序判断）：

| 方式 | 参数 | 例子 |
|---|---|---|
| 手填 | `name` + `method` + `url`（+ `headers` / `params` / `bodyType`+`bodyRaw` / `auth` / `docs` / `folder`） | `{"project":"shop","folder":"api/user","name":"login","method":"POST","url":"{{host}}/api/login"}` |
| cURL 导入 | `curl`（其余字段忽略，名称仍要 `name`） | `{"project":"shop","name":"from-curl","curl":"curl -X POST https://x/api/login -H 'Content-Type: application/json' -d '{\"u\":\"a\"}'"}` |
| 复制现有 | `copyFromUid`（其余字段忽略） | `{"project":"shop","name":"login-copy","copyFromUid":"<uid>"}` |

- 同名在分组内**自动加序号**（不会覆盖已有请求）。
- `headers` / `params` 的元素形状：`{"name":"X-Test","value":"1","enabled":true}`。
- `bodyType` 取 `none` / `raw` / `form`；`auth` 形状见 `type`+`token`/`username`+`password`/`key`+`value`+`in`。
- 返回落盘后的 `uid` 与 `path`。

### 5.6 `create_module` / 5.7 `create_project`

- `create_module`：`project` + `name`（+ `parent` 父模块路径，留空 = 根）。支持嵌套（`parent=api` 建 `api/user`），返回模块路径。
- `create_project`：`name`（写进 manifest 的显示名）+ `dirName`（目录名，留空=与 name 相同）+ `root`（留空 = 启动参数 `-root`）。建完 `list_projects` 立刻能看到。

### 5.8 `send_request` —— 发送并（默认）保存响应示例

| 参数 | 说明 |
|---|---|
| `project` | 项目 |
| `uid` | 发已保存的请求（与下面的临时请求二选一） |
| `method` + `url`（+ `headers` / `body`） | **临时请求**：不落盘、也不会存示例 |
| `env` | 环境名；留空 = 默认（第一个）环境 |
| `saveExample` | 默认 `true`：把响应存成 `examples/<目录>/<请求名>/<示例名>.yml`；`false` = 只发不存 |
| `exampleName` | 示例名；留空按 `"{状态码} 响应"` 命名 |
| `includeBody` | 是否返回响应体，**不传 = 返回**；`false` 时只回状态码/耗时/大小 |
| `maxBodyBytes` | 响应体裁剪上限，默认 8192 |

返回：状态码 / 协议 / 耗时 / 大小 / Content-Type / 响应体（可能已裁剪）/ 是否已存示例 + 示例路径。
存下的示例**客户端里能直接回看**（响应面板的「本次响应 ▾」）。

### 5.9 `update_request` —— 改请求（局部更新）

| 参数 | 说明 |
|---|---|
| `project` / `uid` | 目标 |
| `name` / `url` / `method` / `docs` / `headers` / `body` / `auth` | **只改传入的字段**（其余保持原样） |
| `folder` | 移动到别的模块 |
| `ifMatch` | 传 `get_request_detail` 返回的 `hash`（12 位前缀即可，服务端按 git 风格前缀比对）→ 磁盘文件在此期间被外部改动就**拒绝写入**（不覆盖别人的改动）；**不传 = 最后写者赢**（与桌面客户端一致） |

改名会自动同步文件名。被拒绝时返回的提示是「文件已被外部修改…重新 `get_request_detail` 读取后合并，或 `create_request` 另存为新请求」。

### 5.10 `delete_request` —— 移除请求

`project` + `uid`。文件**移入集合的 `.trash/`**（带时间戳，可人工找回），不会真正删除。
误删恢复：在集合目录的 `.trash/` 里把文件挪回原位并 `list_projects`（`refresh: true`）。

---

### 5.11 环境变量工具（6 个）

环境 = `environments/<env>.yml`，请求里用 `{{变量名}}` 引用其中的变量。

| 工具 | 参数 | 说明 |
|---|---|---|
| `list_envs` | `project`（必填）、`query` | 列出环境与变量。`query` 对**环境名和变量名**都做模糊过滤**每个环境还带一个 `hash`**，写操作时回传即可获得并发保护（见下） |
| `create_env` | `project`、`name`（必填）、`vars[]`（可选） | 新建环境。重名报错，不覆盖。`vars` 里的每条：`{name, value, secret, enabled}`；**`enabled` 不传 = 启用** |
| `rename_env` | `project`、`name`、`newName`（必填）、`ifMatch`（可选） | 改名。两个文件一起搬，旧文件进 `.trash`。目标名已被占用会报错；**只改大小写（`dev`→`DEV`）也会被拒绝**（Windows 上那是同一个文件，改名会让环境消失）。建议带 `ifMatch` |
| `delete_env` | `project`、`name` | 删环境，两个文件都进 `.trash`。**环境不存在会报错**（不会静默成功） |
| `set_env_var` | `project`、`env`、`name`、`value`（必填）、`secret`、`enabled`、`ifMatch` | 按变量名 upsert，**只改这一条**（其它变量、顺序、启用状态都不动）。`enabled: false` 停用某变量（**新增变量也认**）。**强烈建议带 `ifMatch`** |
| `delete_env_var` | `project`、`env`、`name`（必填）、`ifMatch` | 删一个变量。变量不存在会报错。建议带 `ifMatch` |

**名字规则**（服务端强校验，不合规直接报错）：

| 对象 | 规则 | 原因 |
|---|---|---|
| 环境名 | 字母、数字、`-`、`_` | 环境名就是文件名 |
| 环境名判重 | **大小写不敏感**（`DEV` 视为 `dev`） | Windows/macOS 文件系统不区分大小写，`DEV.yml` 就是 `dev.yml`。若按大小写敏感判重，`create_env("DEV")` 会绕过检查并**原地覆盖** `dev`，连密钥一起销毁，且无报错、无 `.trash` 备份 |
| 改名 | **不允许只改大小写** | 同上：`dev`→`DEV` 会先写回同一文件、再把同一文件删掉 → 环境凭空消失。要改大小写请换一个拼写完全不同的名字 |
| 变量名 | `[A-Za-z_][A-Za-z0-9_]*` | 必须能被 `{{name}}` 引用（与客户端渲染语法一致），也顺带排除了路径分隔符 |

#### 敏感值的三条约定（重要）

1. **读不到真值**：`list_envs` / `set_env_var` 的返回里，敏感变量的值一律是 `••••••`。
2. **掩码回传 = 「这条密钥我不动」**：`set_env_var` 收到的值是掩码（或 `••••`/`●●●●`/`····`/`******` 这类被改写过的占位）时，
   **原密钥保持不变**，返回里会写「传回的是掩码，密钥保持原样未改动」。要轮换就传新值。
   > 只认 `• ● · *` 这几种装饰字符。**`.` 和 `?` 不算掩码** —— 它们是现实中可能真实出现的密码字符；
   > 把它们当掩码会让「把密钥改成 `...`」被静默忽略，而调用方看不出自己根本没改成。
3. **空值会被拒绝**：对已存在的敏感变量传 `value: ""` 会**报错**而不是清空 —— 静默把密钥清掉是最坏的帮倒忙。
   确实要清空就把 `secret` 改成 `false` 并给一个新值（它会变成普通变量）。

#### 并发保护（`ifMatch`）

环境是**整份重写**的文件（改一个变量也要重写整个 `<env>.yml`），所以：

- `list_envs` 的每个环境都带一个 `hash`（内容哈希，展示前 12 位）；
- `set_env_var` / `delete_env_var` / `rename_env` 传 `ifMatch: "<hash>"` → 磁盘文件在你读取之后
  被别人（客户端界面 / 另一个 AI 会话 / 外部编辑器）改过就**拒绝写入**，并提示重新 `list_envs` 合并；
- **不传 = 最后写者赢**（与客户端行为一致），可能覆盖别人的改动。

推荐节奏：`list_envs` → 改 → 带 `ifMatch` 写入。`set_env_var` / `rename_env` 成功后会返回**新的 hash**，
连续操作可链式带上，不用反复 `list_envs`。

停用的变量（`enabled: false`）不参与 `{{name}}` 解析：它还在文件里、也还会被 `list_envs` 列出来，
但发送请求时不生效。输出里会标注「已停用」，别把它当成「没生效」去反复排查。

> **新建集合自带一个环境**：客户端第一次打开某个集合时会自动建 `dev`（含 `host=http://127.0.0.1:8080`），
> 所以 `create_env` 直接建 `dev` 会报「已存在」。

## 六、典型工作流（可以直接对 AI 这么说）

### ① 摸清一个陌生项目

> 「列出我的接口项目，并看看 `shop-api` 里有哪些模块、每个模块下大概有哪些请求」

AI 会依次调用 `list_projects` → `get_project_modules(project=shop-api, includeRequests=true)`。

### ② 「登录接口在哪？」

> 「帮我找一下登录相关的接口，我不记得具体名字，大概是 POST 提交用户名密码」

AI 会调用 `search_requests(query="login")`（或 `登录`），返回按相关度排序的清单；
可用 `score`/`matched` 判断可信度，需要细节再 `get_request_detail`。

### ③ 新建接口并跑通，把响应留下来

> 「在 `shop-api` 的 `api/user` 下新建一个接口 `login`，POST `{{host}}/api/login`，JSON 体 `{"username":"a","password":"b"}`，然后发一次并把响应存成示例」

AI 会：`create_module`（已存在则忽略报错）→ `create_request` → `send_request(saveExample=true, exampleName="成功")`。
之后你在客户端里打开这个请求，响应面板的「本次响应 ▾」就能看到那条示例。

### ④ 从 cURL 导入

> 「把这个命令变成集合里的请求：curl -X POST 'https://api.x.com/v1/orders' -H 'Authorization: Bearer x' -d '{"sku":1}'」

AI 会把命令原样传给 `create_request.curl`，再按你指定的 `name` / `folder` 落盘。

### ⑤ 安全地改一个接口（冲突保护）

> 「把 `shop-api` 里 `ping` 的地址加上 `?from=ai`，别覆盖别人的改动」

AI 会：`get_request_detail(uid=…)` 拿到 `hash` → `update_request(ifMatch=<hash>, url=…)`。
若这期间有人改了同一个文件，写入被拒并提示重新读取合并。

### ⑥ 搭一套环境变量（含密钥）

> 「给 `shop-api` 建一个 `staging` 环境：`host=https://staging.example.com`、`apiKey` 是敏感值，
> 密钥先留空，我一会儿给你；再把 `host` 停用看看效果」

AI 会：`create_env`（`apiKey` 标 `secret: true`、值留空）→ `set_env_var(enabled: false)` 停用 `host` →
`list_envs` 回显。**注意它看不到 `apiKey` 的真值**（只回 `••••••`），所以轮换密钥必须由你把新值给它。

> 「把 `staging` 改名成 `uat`，然后把 `uat` 删掉」

AI 会：`rename_env(dev→uat)`（敏感值跟着搬）→ `delete_env`（文件进 `.trash`，可人工找回）。

### ⑦ 只读模式下的用法

给 `-readonly` 起的服务，AI 只能查 + 试发（不落盘），适合放行给「只想让它答疑/查文档」的机器人。

---

## 七、输出裁剪（保护 AI 上下文，也保护你的 token）

| 内容 | 默认 | 怎么调 |
|---|---|---|
| 发送响应体 | 裁到 **8192 字节** | `maxBodyBytes`（不传 `includeBody` 时根本不返回体） |
| 列表条数 | **50** | `limit`（上限 200） |
| 详情里的示例响应体 | 不返回 | `get_request_detail(includeBody=true, maxBodyBytes=…)` |

被裁剪时返回文本里会**明确写**「已截断：共 N 字节，本次仅返回 M 字节；需要完整内容请用 get_request_detail 的 include_body」——不会静默丢弃。

---

## 八、跨主机与 WSL 使用（含实测结论）

AI 工具跑在 WSL2、集合放在 Windows（或反过来）时，**不要**在 WSL 里直接跑 mcpserver 去读
`/mnt/c/...`（原因见 8.5），而是让 Windows 侧跑服务、WSL 侧走 HTTP。

### 8.1 两步启动

```bash
# Windows 侧：绑 0.0.0.0 并显式给令牌
mcpserver.exe -root D:\collections -http 0.0.0.0:8189 -http-token my-secret-token
```

启动日志会打印令牌、监听地址与健康检查地址。**不给 `-http-token` 且监听非回环时，会自动生成一个并打印**
（宁可麻烦也不裸奔）。

### 8.2 WSL 侧连哪个地址 —— 取决于网络模式

| WSL 网络模式 | 怎么判断 | WSL 里填的地址 |
|---|---|---|
| **mirrored**（推荐，Win11 22H2+ / WSL 2.0+ 默认） | WSL 与 Windows 同一网段，`hostname -I` 与 Windows 局域网 IP 同网段 | `http://127.0.0.1:8189/mcp` ✅ 直接通 |
| **NAT**（老模式） | WSL 在 `172.x` 虚拟网段 | `http://<宿主IP>:8189/mcp`，宿主 IP 在 WSL 里执行 `ip route show default` 取网关地址 |

> ⚠️ **mirrored 模式下别用 `ip route show default` 的结果** —— 那拿到的是路由器地址（实测本机是 `192.168.31.1`），
> 不是 Windows 宿主，连不上。本机 mirrored 模式实测：`127.0.0.1:8189` 可达（healthz 200），
> 而 `192.168.31.1:8189` 返回 `000`（连不上）。

WSL 侧先探通再谈业务：

```bash
curl http://127.0.0.1:8189/healthz     # → {"ok":true,"service":"api-doc-go","version":"0.1.0"}（不需要令牌）
```

### 8.3 客户端配置（以 URL 型 MCP 客户端为例）

WSL 里的 Claude Code / cursor / Cline 之类：

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

命令行自检（WSL 里，完整握手 + 调一次工具）：

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

> 第 2 步的 `notifications/initialized` 不能省 —— 协议要求初始化通知之后才能发其它请求。
> 每次握手返回新的 `Mcp-Session-Id`，后续请求都要带上（正规客户端会自动处理）。

实测（Windows 侧绑 `0.0.0.0:8194` + 自动令牌，WSL2 Debian 里执行）：无令牌 `401` → 带令牌握手拿到会话
`YDXOCRJP6LLDVK4BTHEK42S3QN` → `tools/call list_projects` 返回 `共 1 个项目：demo`。

### 8.4 安全与可达性要点

| 项 | 做法 |
|---|---|
| 令牌 | 监听非回环时**必须**有令牌（自动生成或 `-http-token` 指定）。同网段/WSL 侧任何进程都能调工具，没令牌等于把集合交出去 |
| 防火墙 | Windows 首次启动会弹窗，选「专用网络」放行；没弹或被拦就手动放行（管理员 PowerShell：`New-NetFirewallRule -DisplayName mcpserver -Direction Inbound -LocalPort 8189 -Protocol TCP -Action Allow -Profile Private`） |
| 暴露面 | 只在**受信网络**（家里/办公内网）这么做；不要把 8189 直接映射到公网。需要公网访问就走 SSH 隧道：`ssh -L 8189:127.0.0.1:8189 user@host` |
| 浏览器端客户端 | 用 `-http-allow-origin https://your.app` 显式允许来源（同时是 DNS rebinding 防护：不带 Origin 的非浏览器客户端不受影响） |
| 健康检查 | `/healthz` 恒开、不需要令牌、只回版本号 —— 用来探端口通不通，不泄露项目/路径 |
| 建议 | 跨主机暴露时加 `-readonly`：AI 侧只查与试发，不落盘 |

### 8.5 为什么不建议在 WSL 里直接跑 mcpserver 读 `/mnt/c/...`

集合放在 Windows 的 `C:\collections`，在 WSL 里跑服务虽然能读到文件，但：

- **`/mnt/c`（drvfs/9p）不产生可靠的 inotify 事件** → 外部改动监听基本失效（哈希变慢、冲突保护要靠手动重读兜底）；
- 落盘时的 `ExpectHash` 冲突保护依赖索引刷新，索引不刷新就可能覆盖别人的改动；
- 跨文件系统的文件锁/重命名语义更容易出怪问题（尤其是 `.trash` 移动与重命名）。

要跑就把集合放在 WSL 原生文件系统（`~/collections`）并保证只有这一个进程在写；否则用上面的 HTTP 方式
（集合留在 Windows，Windows 侧跑服务，WSL 侧远程调用）。

## 九、排查

| 现象 | 原因 / 处理 |
|---|---|
| AI 说「找不到工具 / 服务器未连接」 | 配置里的 `command` 路径错、没重启 AI 工具、`-root` 没传（必填会直接退出） |
| `list_projects` 空、但 `-root` 下明明有目录 | 该子目录**缺 `opencollection.yml`**（不算项目）；加好文件后 `refresh: true` 重扫 |
| 提示「这是 Bruno 集合目录」 | Bruno 格式需先用客户端「导入」转成 api-doc-go 集合 |
| 提示「找不到项目」/「匹配到多个项目」 | `project` 支持 uid / 名称 / 路径三种；歧义时用返回的候选之一（推荐 `path`） |
| `search_requests` 少了某些项目 | 不带 `project` 时只搜**已加载**项目；先 `get_project_modules` 或 `list_projects` 打开它 |
| 保存被拒「文件已被外部修改」 | 磁盘文件在 AI 读取之后被改过（另一窗口 / 外部编辑器 / 同步）。重新 `get_request_detail` 合并后重试，或 `create_request` 另存。**偶发误报不用慌**：文件监听重建索引有几百毫秒延迟，刚改完立刻读到的 hash 可能是旧的 —— 重新读一次即可 |
| 发送失败「发送失败: …」 | 地址不通 / 超时（默认 30s，受全局设置 `timeoutSec` 约束）/ 证书问题（`insecureSsl`） |
| 写操作提示「unknown tool」 | 用了 `-readonly`：写工具没注册。去掉该参数重启 |
| HTTP 模式起不来 | 端口被占：换一个 `-http 127.0.0.1:8190`；HTTP 路径固定 `/mcp` |
| AI 收到的项目统计是 0 | 懒加载：项目首次被访问后才填统计。访问一次即可 |
| `环境 "dev" 已存在` | 新建集合时客户端会自动生成一个 `dev` 环境（含 `host`）。要么换个名，要么直接用它 |
| `找不到环境 "xxx"` | 环境名拼错或还没建（`list_envs` 看现有）。新建集合默认只有 `dev` 一个 |
| `变量名 "1bad" 不合法` | 变量名必须以字母/下划线开头，只含字母数字下划线 —— 否则 `{{1bad}}` 引用不到 |
| `环境 "DEV" 已存在（磁盘上实际是 "dev"）` | 环境名判重**大小写不敏感**：Windows 上 `DEV.yml` 就是 `dev.yml`，这两个名字是同一个环境 |
| `新环境名 "DEV" 与原环境 "dev" …同一个文件` | 不允许只改大小写改名（否则环境会消失），换一个拼写完全不同的名字 |
| `[conflict] 环境 … 的文件已变化` | 读取之后有人改了它。重新 `list_envs` 看最新内容，合并后带新的 `ifMatch` 再写；或去掉 `ifMatch` 强制覆盖（会丢掉对方的改动） |
| 变量「配了但没生效」 | 看 `list_envs` 输出里有没有「已停用」标注：`enabled: false` 的变量不参与 `{{name}}` 解析 |
| 密钥被改成 `••••••` 了？ | 不会：传回掩码（或 `••••`/`******`/`??????` 这类占位）时服务会**保留原密钥**并说明「传回的是掩码」。真要轮换就传新值；要清空得把 `secret` 改 false |
| WSL 里连不上 Windows 上的服务 | 先 `curl http://127.0.0.1:8189/healthz`：`000` = 地址或防火墙问题（mirrored 模式用 `127.0.0.1`，别用 `ip route default` 的路由器地址）；有响应但 401 = 令牌不对 |
| 401 unauthorized | 客户端没带 `Authorization: Bearer <token>`，或令牌与服务启动时打印的不一致（服务重启会自动换新令牌，需同步到客户端配置） |
| 403 origin not allowed | 请求带 `Origin` 头但不在 `-http-allow-origin` 允许列表（浏览器端客户端要显式允许该来源） |
| `go build` 拉不到 go-sdk | 该网络下 `proxy.golang.org` 不可达 → `GOPROXY=https://goproxy.cn,direct go build …` |

---

## 十、安全清单

| 项 | 说明 |
|---|---|
| 能做什么 | 在 `-root` 下的集合里读/写请求、建模块/项目、发请求、删请求（进 `.trash`） |
| 不会做什么 | 不碰 `-root` 之外的路径（集合层拒绝绝对路径与 `..`）；不执行任意脚本命令；不自动提交 git |
| 写保护 | `update_request` 支持 `ifMatch` 冲突保护；`delete_request` 只进 `.trash` |
| 建议 | ① 对外/对团队的机器人用 `-readonly`；② 让 AI 指向 **git 工作副本**的集合目录，改动可 review、可回滚；③ 需要留痕时要求 AI 只用「更新」而非「删除 + 新建」；④ 响应示例会进版本库，注意敏感数据 |
| 远程 | 监听非回环地址时**自动生成并打印 Bearer 令牌**（`-http-token` 可指定），每个请求都校验；`/healthz` 无需令牌且只回版本号。仍需自建防火墙或 SSH 隧道，勿直接暴露公网 |

---

## 十一、与桌面客户端的关系

- 两者操作**同一份集合文件**：AI 建的请求、存的示例，客户端里立刻能看到（客户端的外部改动检测会提示冲突）。
- 客户端有的能力 MCP 没有：UI 编辑、变量书签、cURL 导入面板、gRPC 定义（Schema）导入与 Mock 面板、同步绑定面板。
- 反过来 MCP 有客户端没有的：跨项目模糊搜索、脚本化批量操作、给其它 AI 复用。
- 建议用法：AI 负责「查、批量整理、跑回归并存示例」，人工在客户端里精修与提交。

---

## 附：也可以直接用客户端内置的 MCP 服务（不用单独跑 mcpserver.exe）

除了命令行方式，**客户端本身就内置了同一个 MCP 服务**，在「全局设置 → MCP 服务」分区里配置即可
（2026-10-03 新增）。适合「AI 工具和客户端在同一台机器上、想一键开关」的场景。

### A.1 开启步骤（设置界面）

打开「全局设置」（`Ctrl+K` → 设置）→ 找到 **MCP 服务** 分区（独立于「界面 / 网络与安全 / 本地数据」）：

| 字段 | 说明 |
|---|---|
| 启用内嵌 MCP 服务 | 总开关。关闭时不监听任何端口 |
| 监听地址 | `127.0.0.1（仅本机）` / `0.0.0.0（跨主机：WSL / 局域网）` |
| 端口 | 默认 8189，可改（1024–65535） |
| 只读模式 | 默认开：AI 只能查询与试发，不能创建/修改/删除请求 |
| 浏览器来源 | Origin 白名单，逗号分隔；留空 = 拒绝一切浏览器来源 |
| 访问令牌 | 启用且为空时**自动生成**；「重新生成」立即换新（旧令牌马上失效） |
| 状态行 | 运行中显示 `http://地址:端口/mcp` 与可见项目数；失败显示原因（如端口被占用） |

保存后**立即生效**（后端会重启服务）；「复制连接配置」把地址与令牌一起复制，粘到 AI 工具配置里即可。

### A.2 它与独立 mcpserver 的差别

| | 客户端内嵌服务 | 独立 `mcpserver.exe` |
|---|---|---|
| 配置位置 | 设置界面「MCP 服务」分区 | 命令行参数 |
| 项目根 | **当前打开集合的父目录**（同级集合也会一并对 AI 可见） | `-root` 指定的目录 |
| 生命周期 | 随客户端（打开应用即起，关闭应用即停） | 独立进程 |
| 默认只读 | 开 | 关（要只读自己加 `-readonly`） |
| 多开 | 一个客户端一个实例 | 可多实例（注意端口冲突） |

配置存在客户端的全局设置里（`<用户配置目录>/api-doc-client/config.json` 的 `mcp` 段），与界面其它设置
一起保存、一起跨会话保留。

### A.3 跨主机（WSL）用内嵌服务

1. 设置里：启用 + 监听地址选 `0.0.0.0` + 记下端口与令牌；
2. WSL 里先探通：`curl http://127.0.0.1:<端口>/healthz`（mirrored 模式）或 `curl http://<宿主IP>:<端口>/healthz`（NAT 模式）；
3. AI 工具配置（WSL 里的客户端）：

```json
{
  "mcpServers": {
    "api-doc-go": {
      "url": "http://127.0.0.1:8189/mcp",
      "headers": { "Authorization": "Bearer <设置里复制的令牌>" }
    }
  }
}
```

mirrored 模式（Win11 22H2+）实测可直接连 `127.0.0.1`；NAT 模式要换成宿主 IP。
首次可能需要放行 Windows 防火墙（专用网络）。

> 提示：内嵌服务的**只读模式默认开着**，如果你要 AI 建/改接口，先在设置里把它关掉（改动立即生效）。

## 十二、命令速查

```bash
# 构建（首次需拉 SDK，注意代理）
GOPROXY=https://goproxy.cn,direct go build -o mcpserver.exe ./cmd/mcpserver

# 启动
mcpserver.exe -root D:\collections                 # stdio（AI 挂载）
mcpserver.exe -root D:\collections -http 127.0.0.1:8189   # + HTTP /mcp
mcpserver.exe -root D:\collections -readonly       # 只读

# 跨主机（WSL2 / 局域网）；实测 mirrored 模式下 WSL 里用 127.0.0.1 直连
mcpserver.exe -root D:\collections -http 0.0.0.0:8189 -http-token <令牌>

# 调试：手工发一条 JSON-RPC 看服务是否活着（stdio 需保持管道打开）
printf '%s\n' \
 '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}' \
 '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
 '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
 | mcpserver.exe -root D:\collections
```

| 工具 | 必填 | 常用可选 |
|---|---|---|
| `list_projects` | — | `project`、`refresh` |
| `get_project_modules` | `project` | `query`、`includeRequests` |
| `get_request_detail` | `project` `uid` | `query`、`includeBody`、`maxBodyBytes` |
| `search_requests` | `query` | `project`、`limit` |
| `create_request` | `project` `name` | `folder`、`method`、`url`、`headers`、`curl`、`copyFromUid`、`docs` |
| `create_module` | `project` `name` | `parent` |
| `create_project` | `name` | `dirName`、`root` |
| `send_request` | `project` + (`uid` 或 `method`+`url`) | `env`、`saveExample`、`exampleName`、`includeBody`、`maxBodyBytes` |
| `update_request` | `project` `uid` | `name`/`url`/`method`/`headers`/`body`/`docs`/`folder`、`ifMatch` |
| `delete_request` | `project` `uid` | — |
| `list_envs` | `project` | `query` |
| `create_env` | `project` `name` | `vars[]`（`{name,value,secret,enabled}`） |
| `rename_env` | `project` `name` `newName` | `ifMatch`（建议带） |
| `delete_env` | `project` `name` | — |
| `set_env_var` | `project` `env` `name` `value` | `secret`、`enabled`（不传=启用/保持原状）、`ifMatch`（建议带） |
| `delete_env_var` | `project` `env` `name` | `ifMatch`（建议带） |

默认约定回顾：自动保存响应示例（`saveExample` 默认开）、响应体裁剪 8192 字节、列表 50/200 条、
敏感变量值只回掩码（掩码回传=不改，`.`/`?` 不算掩码）、新建环境与新建变量省略 `enabled` 即启用、写操作建议带 `ifMatch`、
删除进 `.trash`、`-root` 下含 `opencollection.yml` 的子目录=项目、`project` 三种写法（uid/名称/路径）。
