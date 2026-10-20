# apidoc/server 代码审核报告

**审核范围**：`/home/zq-deepin/my-project/api-doc-go/api-doc-go/server`（只读，未修改任何文件）

## 统计概览

| 指标 | 数值 |
|---|---|
| Go 文件总数 | 113（82 源文件 + 31 测试文件） |
| 源码行数（非测试） | 11,045 |
| 测试行数 | 4,672（测试/源码 ≈ 0.42） |
| 测试函数数 | 105 |
| SQL 迁移文件 | 62（postgres 31 + sqlite 31，版本 0001–0016） |
| 最大文件 | `internal/service/content.go` — **835 行** |
| Top 5 最大源文件 | `service/content.go` 835 · `handler/content.go` 500 · `repository/content.go` 436 · `dto/dto.go` 383 · `service/sync_apply.go` 358 |
| service 层规模 | 24 个源文件 / 约 4,300 行（占非测试代码 39%） |
| 零测试覆盖的包 | `internal/config`、`internal/job`、`middleware/auth.go`、`middleware/cors.go`、`database/db.go` |

---

# Critical

### C-1 未认证的 `/mock` 端点泄漏项目明文机密变量
**`internal/service/mock.go:58`** + **`internal/service/variable_resolve.go:117-140`** + **`internal/router/router.go:31`**

`/mock/:project_id/*path` 注册在鉴权组之外（router.go:31），完全免登录，`project_id` 是自增整数、可直接枚举。`mockBody` 在 mock.go:58 调用 `s.resolveTable(ctx, a.ProjectID, a.ID, 0)` 渲染样例中的 `{{变量}}`，而 `resolveTable` 返回**明文变量值**（variable_resolve.go:137 `table[name] = hit.v`），只跳过 `enc:v1:` 前缀的客户端加密值（variable_resolve.go:124）。

但 `internal/service/variable.go:3` 与 `internal/model/variable.go:22` 明确写着「敏感值（is_secret=1）**明文入库**」「不是安全边界」——即服务端不强制客户端加密。因此任何未认证攻击者枚举 `/mock/1/...`、`/mock/2/...` 即可收割各项目以明文存储的 API token、口令等机密，同时拿到全部响应样例。

对比 `service/code.go:37` 对同类渲染做了 `canRevealValues` 校验并写审计日志——mock 路径完全绕过了这套控制。

**修复**：(a) `resolveTable` 增加 `reveal bool` 参数，mock 路径传 false 并对 `IsSecret==1` 的变量保留占位符；(b) 给 `/mock` 加项目级只读令牌（复用 share_link 机制）而非裸 project_id；(c) 服务端强制 secret 变量必须以 `enc:v1:` 密文入库，拒绝明文写入。

---

### C-2 大面积 IDOR：12 个读端点完全没有鉴权，租户隔离失效
任意已注册普通用户可读取任意项目的全部数据。`internal/service/permission_read_test.go:14` 的注释恰好记录了 P0-1 修复的边界（`GetApi / ListApis / GetDoc / ListDocs / ListGroups / ListFieldMappings`），**边界之外的全部遗漏**：

| 端点 | Handler（未取 principal） | Service（无权限校验） | 泄漏内容 |
|---|---|---|---|
| `GET /api/api/export-postman` | `handler/postman.go:15` | `service/postman.go:21` | 项目全量接口集合（URL/请求头/请求体/响应样例） |
| `GET /api/api/export` | `handler/content.go:274` | `service/export.go:17` | 同上（OpenAPI 格式） |
| `GET /api/project/detail` | `handler/project.go:41` | `service/project.go:87` | 项目标题/描述/版本/写开关 |
| `GET /api/project/members` | `handler/project.go:90` | `service/project.go:135` | 成员 user_id、用户名、昵称、负责人标记、权限位 |
| `GET /api/api/samples` | `handler/content.go:219` | `service/api_sample.go:55` | 接口全部响应示例正文 |
| `GET /api/bug` | `handler/misc.go:77` | `service/misc.go:90` | 项目全部缺陷（标题/正文/评论） |
| `GET /api/bug/detail` | `handler/misc.go:87` | `service/misc.go:85` | 单条缺陷全文 |
| `GET /api/bug/assign` | `handler/misc.go:97` | `service/misc.go:100` | 指派流转记录 |
| `GET /api/operation-log` | `handler/misc.go:204` | `service/misc.go:203` | **审计日志**：谁在何时改了什么 |
| `GET /api/variable/resolve` | `handler/variable.go:144` | `service/variable_resolve.go:17` | 变量名/作用域/来源/是否有值 |
| `GET /api/variable/summary` | `handler/variable.go:164` | `service/variable_resolve.go:50` | 变量名 + 接口存在性 |
| `GET /api/variable-env/references` | `handler/variable.go:68` | `service/variable_env.go:118` | 环境名 + 其下变量清单 |
| `POST /api/doc/like` | `handler/content.go:442` | `service/content.go:697` | 写操作：无鉴权、无幂等，可无限刷 like_count |

`service/project.go:87` 与 `service/postman.go:21` 的函数签名根本不接收 `userID/typ`，说明遗漏是结构性的而非偶发。

**修复**：给上述每个 service 方法补 `userID uint64, typ int8` 形参并在首行调用 `s.ensureProjectPermission(ctx, userID, typ, projectID, model.PermQuery)`（对 `GetBug`/`GetApiSample` 这类只有实体 id 的，先加载实体再用 `entity.ProjectID` 校验，参照 `service/content.go:237-246` 的 `GetApi` 写法）。同时把 `permission_read_test.go` 扩成表驱动测试，遍历全部只读端点断言非成员必得 403——否则同类遗漏会继续复发。

---

# High

### H-1 用户被禁用/降权后，JWT 仍然完全有效（最长 24 小时）
**`internal/middleware/auth.go:42-45`** · **`internal/handler/user.go:58,88`** · **`internal/service/auth.go:31`** · **`internal/service/token.go:154-160`**

JWT 路径下 `Principal{UserID, Type}` 完全取自令牌 claims（auth.go:43），handler 直接用 claims 里的 `Type` 构造 operator（user.go:58、88），`isAdmin()` 据此放行。账号状态只在两处校验：登录时（auth.go:31）和 PAT 路径（token.go:158）。

后果：管理员禁用某账号后，该账号已签发的 JWT 在 `ExpireHours`（默认 24h，config.go:162）内继续拥有**全部权限**；把超管降为普通用户同样不生效。PAT 路径做了正确校验，两条鉴权路径语义不一致。

**修复**：在 `AuthOrPAT` 之后增加一个 `LoadPrincipal` 中间件，按 `claims.UserID` 查库刷新 `Type`/`State`（可加 30–60s 的进程内缓存避免每请求查库）；或引入令牌版本号/`jti` 黑名单，在禁用与角色变更时失效存量令牌。

### H-2 `SyncChanges` 的 limit 未在入口收敛，导致客户端静默丢失变更
**`internal/handler/sync.go:42`** → **`internal/service/sync.go:73-75,85,89,116-118`** → **`internal/repository/sync.go:90-92`**

handler 直接 `strconv.Atoi` 取 limit，无上界；service 只处理 `limit <= 0`；repository 却把 `limit > 1000` **静默改成 200**。于是 `?limit=5000`：

1. repo 只返回 200 行；
2. `hasMore := len(logs) > limit` → `200 > 5000` = **false**（sync.go:89）；
3. `!hasMore && cursor < cur` 成立 → `cursor = cur`（**最新** change_id，sync.go:116-118）；
4. 客户端收到「cursor=最新、HasMore=false」，中间 4800 条变更被永久跳过。

这是无告警的同步数据丢失。**修复**：在 handler 或 service 入口把 limit 夹到 `[1, 1000]`，并让 `ChangesSince` 返回实际使用的 limit（或改为返回错误而非静默改写），保证 `hasMore` 的判定基准与实际取数一致。

### H-3 `SyncSnapshot` 的 limit 无上界 → 内存放大 DoS
**`internal/handler/sync.go:60`** → **`internal/service/sync.go:136-138,164`** → **`internal/repository/sync_page.go:23-32`**

同 H-2，limit 未经收敛直达 `listProjectRowsPage` 的 `Limit(limit)`。`?limit=1000000` 会把最多一百万行 `model.Api`（含 `http_request_body`、`http_return_sample` 等多个 TEXT 列）一次性读进内存并 JSON 序列化。P2-26 的分页优化（sync.go:124-127 注释）因此形同虚设。该端点只需 `PermQuery`，任意项目成员即可触发。

**修复**：`if limit <= 0 || limit > 500 { limit = 200 }`，与 `page.Query.Normalize()`（page.go:17-19 已有 200 上界）保持同一口径。

### H-4 `groupMoveCyclic` 无深度保护 → 可触发不可恢复的无限循环
**`internal/service/content.go:767-779`**，成因在 **`internal/service/content.go:105`**

`UpdateGroup` 在 content.go:105 无条件 `g.PID = req.PID`，**不做任何环检测**，因此用户可以把分组 A 挂到自己的后代 B 下，制造 `p_id` 环。之后任何 `POST /api/sort`（kind=group）都会走进 `groupMoveCyclic`：该函数沿 `pid = g.PID` 上溯，**既无 visited 集合也无深度上限**（对比同文件 `deletedGroupChain` 在 content.go:462 有 `guard < 32`），环存在时永久循环，每次迭代发一条 SQL。

单个请求即可挂死一个 goroutine 并持续打库；重复几次即耗尽连接。`gin.Recovery()` 无法干预（不是 panic）。

附带问题：content.go:774 `return false, nil // 父级不存在，视为无环` 把**数据库错误**也当成「父级不存在」，环检测在 DB 抖动时静默失效。

**修复**：`UpdateGroup` 复用 `groupMoveCyclic` 做前置校验；`groupMoveCyclic` 加 `visited map[uint64]struct{}` 与 `depth < 64` 双重保护；区分 `ErrNotFound` 与真实错误，后者必须向上返回。

### H-5 导入路径的无界递归 → 栈溢出导致进程崩溃
**`internal/service/bruno.go:132-139`** · **`internal/service/postman.go:150-162`**

`brunoGroupEnsurer` 返回的闭包 `ensure(dir)` 在第 136 行先递归 `ensure(path.Dir(dir))`，**无任何深度限制**，而 `dir` 直接来自请求体 `BrunoFile.Path`（`dto/dto.go:376` 只有 `binding:"required"`）。构造 `a/a/a/.../x.yml`（数千层）即可让递归深度等于路径段数。

`postman.go:151` 的 `walk` 同理，深度等于 Postman JSON 的文件夹嵌套层数，完全由请求体控制。

Go 的栈溢出是 **fatal error，不是 panic**，`gin.Recovery()`（router.go:18）捕获不到 → **整个进程崩溃**，所有在途请求与后台任务一起死。`dto/dto.go:382` 的 `Files` 也只有 `min=1` 没有 `max`。

**修复**：两处递归都加显式深度上限（如 32 层，超限返回 `CodeParamError`）；`BrunoImportReq.Files` 加 `max=2000`，`BrunoFile.Path` 加长度上限；导入 handler 用 `http.MaxBytesReader` 限制请求体（见 H-6）。

### H-6 导入端点 `io.ReadAll` 无请求体上限
**`internal/handler/content.go:243`** · **`internal/handler/postman.go:25`**

`body, err := io.ReadAll(c.Request.Body)` 直接把整个请求体读进内存，无 `http.MaxBytesReader`、无 `engine.MaxMultipartMemory`（全仓库 grep 无该配置）。Gin 默认不限制 JSON 请求体大小。并发若干个 GB 级 POST 即可 OOM。同类问题也在上传路径：`handler/misc.go:222` 的大小检查发生在 `c.FormFile` **之后**，此时整个文件已经落盘到临时目录，`MaxMB` 挡不住磁盘耗尽。

**修复**：`c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, N)`（N 取 32–64MB）；`router.Setup` 里设 `engine.MaxMultipartMemory`；上传改为流式读取并在超过 `MaxMB` 时提前中断。

### H-7 `SetLeader` 不清除旧负责人 → 权限永久残留
**`internal/service/project.go:152-163`**

只对新负责人 `m.IsLeader = 1; m.Permission = PermAll`，**从不把原负责人的 `IsLeader` 置 0**。由于 `ensureManage`（project.go:214）与 `ensureProjectPermission`（service.go:123）都以 `IsLeader == 1` 作为「全权限」判据，每一次转让负责人都会**累积**一个拥有成员管理权与全部读写权限的账号。多次转让后项目里可能有一堆「隐形超管」，且无法通过 UI 撤销。

**修复**：在同一事务内先 `UPDATE user_project SET is_leader=0, permission=? WHERE project_id=? AND is_leader=1`，再设置新负责人。

### H-8 导入产生的实体没有 `ext_uid`，对同步永久不可见；并连带破坏快照分页
**`internal/service/extra.go:150-155`** · **`internal/service/bruno.go:165-174`** · **`internal/service/postman.go:227-235`**

三条导入路径构造 `model.Api` 时都**没有设置 `ExtUID`**（对比 Web 路径 `content.go:200`、变量 `variable.go:241`、环境 `variable_env.go:41`、示例 `api_sample.go:31` 全都设置了）。而 `service/sync.go:311-313` 的 `appendItem` 明确 `if uid == "" { return }` 丢弃这类条目。

后果一：**任何通过 OpenAPI/Bruno/Postman 导入的接口，桌面客户端永远同步不到**，用户看到「导入成功」但客户端空空如也。

后果二（连带触发 H-3 之外的分页 bug）：`sync.go:144-161` 的 `counts[i]` 由 `CountApisAll` 统计（**包含** uid 为空的行），而 `snapshotRows` 把它们丢掉，于是 `sync.go:181` 的 `HasMore: int64(offset)+int64(len(rows)) < total` 恒为 true，`len(rows)` 又小于 limit → 客户端按 `offset += len(rows)` 前进会**在某个 offset 上原地打转或提前判定结束**，快照分页对任何用过导入的项目都是错的。

**修复**：三条导入路径统一 `ExtUID: newExtUID()`；`appendItem` 丢弃条目时应同时从 `total` 中扣除（或让 `Count*All` 加上 `ext_uid <> ''` 条件，与 `loadSnapshotSlice` 的取数口径一致）。

### H-9 变量的 `ext_uid` 每次更新都被重新生成 → 同步身份持续漂移
**`internal/service/variable.go:241`** 配合 **`variable.go:177-182`**

`normalizeVariable` 在第 241 行无条件 `v.ExtUID = ensureExtUID("")`（`cur` 恒为空串，故恒生成新 UUID）。`CreateVariable`（variable.go:105）和 `UpdateVariable`（variable.go:177）**都调用它**；`UpdateVariable` 随后在 181-182 行只恢复了 `next.ID` 与 `next.BaseModel`，`ExtUID` 属于 `Syncable` 嵌入结构、**没有被恢复**。

于是每次编辑变量都会换一个新的同步身份：客户端看到旧 uid 消失（且因为是硬删无 tombstone，见 H-10，连消失都感知不到）+ 一个新 uid 出现，等价于 delete+create，本地编辑历史与冲突检测全部失效。

对比 `content.go:226` `updated.ExtUID = ensureExtUID(a.ExtUID)` 与 `content.go:107/576` 都是正确写法——只有变量这条路径错了。

**修复**：`normalizeVariable` 增加 `curExtUID string` 参数并 `v.ExtUID = ensureExtUID(curExtUID)`；或在 `UpdateVariable` 中 `next.ExtUID = old.ExtUID`。补一条「更新后 ext_uid 不变」的断言到 `variable_test.go`。

### H-10 变量/环境/响应示例是硬删除，tombstone 机制对它们完全失效
**`internal/repository/variable.go:20-22,68-70`** · **`internal/repository/api_sample.go:24-26`** · 对照 **`internal/repository/sync.go:302-313`** 与 **`internal/service/sync.go:333-345`**

`DeleteVariableEnv`/`DeleteVariable`/`DeleteApiReturnSample` 都走 `gmHardDelete`（物理删除），但：
- `repository/sync.go:302,309` 的注释写着「列出项目全部环境（**含软删行**），快照需带 Deleted 标记」；
- `service/sync.go:335,344` 计算 `p.Deleted = ex.IsDeleted == model.IsDeletedYes`。

行已被物理删除，`IsDeleted == 1` 的快照行**永远不可能存在**，`Deleted: true` 是死代码。客户端做全量重同步时，服务端删掉的变量只是「从快照里消失」，客户端无法区分「被删」与「不属于本项目」，会保留本地副本并在下次 push 时把它推回服务端——**删除操作被静默复活**。

同时 `service/variable.go:198` 的注释「DeleteVariable **软删除**变量」与实现直接矛盾。api/doc/group 走软删（`generic.go:55`）tombstone 正常，两套语义并存。

**修复**：把这三类实体统一改为 `gmSoftDelete`（与 `uk_variable_key`、`uk_variable_ext_uid` 的 `where: is_deleted = 0` 局部索引设计本就一致），并让 `job/cleanup.go` 负责超期物理清理；或明确放弃 tombstone，改为在快照响应里带上「已删除 uid 列表」。

### H-11 同步载荷回传明文机密变量，绕过掩码、明文权限与审计三重控制
**`internal/service/sync_payload.go:71-81,94-102`** · 对照 **`internal/service/variable.go:121-152`**

`varSyncPayload`（:74）与 `varSyncPayloadWith`（:97）都无条件 `Value: v.Value` 回传原文。而 `SyncSnapshot`/`SyncChanges` 只要求 `PermQuery`（`sync.go:70,133`）。

对比 `/api/variable` 的正规路径：默认走 `maskVariable` 掩码（variable.go:146），要拿明文必须 `with_value=1` **且** 通过 `canRevealValues`（负责人或 update 位，variable.go:133、295-307），**且**写审计日志（variable.go:148-150）。

于是**只读成员**通过 `GET /api/sync/snapshot?project_id=N` 就能拿到全项目明文机密变量，三重控制全部绕过，且不留任何审计痕迹。

**修复**：同步载荷对 `IsSecret == 1` 的变量应用 `varx.MaskedValueOf`；若客户端确实需要明文回传，则把 `SyncSnapshot`/`SyncChanges` 的权限位提升到 `PermUpdate` 并复用 `canRevealValues` + `recordOp` 审计。

### H-12 无 HTTP 服务器超时、无优雅关闭，`ctx` 取消链路是死代码
**`cmd/server/main.go:35-36,48-49,55`**

全仓库 grep `signal.`、`Shutdown`、`http.Server`、`ReadTimeout`、`WriteTimeout`、`IdleTimeout` **零命中**。

1. `engine.Run(addr)`（main.go:55）内部构造的 `http.Server` **不带任何超时** → 完全暴露于 Slowloris：少量慢速连接即可占满 goroutine 与 fd。
2. 无 `signal.Notify` → `docker stop`/k8s 滚动更新发出的 SIGTERM 直接杀进程：在途请求被腰斩（写操作可能只完成一半，见 H-16 的非事务多写）、SQLite WAL 不做 checkpoint、备份 goroutine 可能在 `io.Copy` 中途被杀留下**看似完整的截断备份**（H-13）。
3. main.go:35 的 `ctx, stop := context.WithCancel(...)` + `defer stop()`：`engine.Run` 永久阻塞，返回时走 `log.Fatalf`（跳过 defer），**`stop()` 永远不会被调用** → `job.StartBackup`/`StartCleanup` 的 `ctx.Done()` 分支（backup.go:30、cleanup.go:29）是死代码，两个后台任务从未被优雅停止过。

**修复**：改用显式 `http.Server{Addr, Handler: engine, ReadHeaderTimeout: 10s, ReadTimeout: 30s, WriteTimeout: 120s, IdleTimeout: 120s}`；`signal.NotifyContext(context.Background(), os.SIGINT, os.SIGTERM)` 派生 ctx 传给 job；收到信号后 `srv.Shutdown(ctx)` 带超时等待在途请求，再 `stop()`，最后关闭 DB 连接池。

### H-13 SQLite 备份用 `io.Copy` 复制活动数据库文件，备份可能损坏
**`internal/job/backup.go:46-59`**

直接 `os.Open(cfg.DB.DSN)` + `io.Copy` 复制正在被写入的库文件，既不用 SQLite 的 online backup API 也不用 `VACUUM INTO`。在 WAL 模式下（`database/db.go` 未显式配置 journal mode，但 glebarez 驱动默认行为需确认）`-wal` 文件根本没被复制，备份**缺失全部最近事务**且可能处于页级不一致状态——恢复时才发现是坏库。

附带问题：`backup.go:56` `defer out.Close()` 不检查 Close 错误（磁盘满/flush 失败会产出截断文件却报告成功）；`io.Copy` 中途失败时残留的半成品文件不清理（对比 `handler/misc.go:258` 有 `os.Remove`）；`backup.go:51` 文件名精度到秒，同秒两次备份互相覆盖；**没有任何保留/轮转策略**，备份无限堆积直到撑满磁盘。

**修复**：SQLite 改用 `VACUUM INTO '<dst>'`（单语句、原子、驱动内完成）或 `sqlite3_backup` API；Postgres 走 `pg_dump`（当前 backup.go:18 对非 sqlite 直接 return，等于生产环境无备份）；写入临时名后 `rename`；检查 Close 错误；按 `Backup.Retention` 保留最近 N 份。

### H-14 `migrations/` 下的 62 个 SQL 脚本从未被执行，且 Postgres 脚本零测试覆盖
**`internal/database/migrate.go:20-54`** · **`cmd/server/main.go:31`** · **`internal/database/migrate_test.go:49-63,122-155`**

应用启动只跑 `db.AutoMigrate(entities...)`（migrate.go:50），全仓库无任何迁移执行器（grep `migrations` 只命中 `data/genmigrate/main.go` 的生成器和 `migrate_test.go`）。migrate.go:21 的注释「生产环境可另用 migrations/ 下的 SQL 脚本」描述的是一个**不存在的能力**。这意味着 schema 有两个真源（GORM tag 与 SQL 脚本），必然持续漂移。

测试也没能兜住：
- `TestSQLiteMigrationsApply`（:49）**只执行 sqlite 目录**，16 个 Postgres 脚本从未被任何测试运行过——PG 方言的语法错误、列名拼写错误、类型不匹配全部无法被发现，而 `DB_DRIVER=postgres` 是官方支持的生产配置。
- `TestMigrationVersionParity`（:122）只比对**文件名与版本号**，不比对内容，因此两方言的语义差异（见 H-15）不可见。
- `upFiles()`（:34）只收集 `*.up.sql`，**`.down.sql` 完全不检查**——这正是 `0010_sync_foundation.down.sql` 双方言同时缺失却长期无人发现的原因。

**修复**：引入 `golang-migrate` 或 `goose`（用 `embed.FS` 打包脚本），让 `Migrate` 真正执行版本化迁移，AutoMigrate 仅保留给开发/测试；CI 增加 Postgres service container 跑一遍全部 PG 脚本 + 一轮 up/down 往返；`upFiles` 改为同时校验 up/down 成对存在。

### H-15 `user_info` 的唯一约束是 (name, email) 复合索引，登录名本身并不唯一
**`internal/model/user_info.go:32,34`** · **`migrations/{postgres,sqlite}/0015_user_name_email_partial_unique.up.sql:8-9 / 7-8`** · **`internal/repository/user.go:37`**

两处都创建 `UNIQUE INDEX uk_name_email_active ON user_info(name, email) WHERE is_deleted = 0`——**复合**唯一，不是 name 单列唯一。因此 `name="alice", email="a@x"` 与 `name="alice", email="b@y"` 可以共存。

应用层的 `UserTaken`（repository/user.go:45-69）+ `Register`（service/auth.go:46-52）虽然会拦，但那是**先查后插的 TOCTOU**，两个并发注册请求都能通过检查；DB 约束本该兜底，却因为索引形状兜不住。

而登录走 `UserByName`（repository/user.go:37）→ `Where("name = ?").First(&u)`，`First` 按主键升序取第一条 → **重名时登录会静默命中 id 较小的那个账号**。若攻击者注册一个与管理员同名的账号（配合 `email` 不同），密码校验针对的是管理员那行，攻击者拿不到；但反过来，管理员改名/新建同名账号会导致登录目标不可预测，且 `UpdateUser` 的 `operator.ID != targetID` 判定（auth.go:108）依赖 id 而非 name，容易出现「以为在改自己、实际改了别人」。

`migrate_test.go:87-117` 的 `TestUserNameEmailPartialUnique` 只验证了「(name,email) 完全相同的两行冲突」，**没有验证 name 单列唯一**，给出了错误的安全感。

**修复**：改为两个独立的局部唯一索引：`uk_user_name_active ON user_info(name) WHERE is_deleted = 0` 与 `uk_user_email_active ON user_info(email) WHERE is_deleted = 0 AND email <> ''`；`UserByName` 在 `First` 之外增加 `Count` 断言（>1 视为数据损坏并拒绝登录）；测试补一条「同 name 不同 email 必须被拒」。

### H-16 `AssignBug` 三次写库无事务，且把通知失败当作整体失败
**`internal/service/misc.go:63-82`**

`UpdateBug`（:72）、`CreateBugAssign`（:75）、`Notify`（:81）是三条独立提交的写操作，无 `s.repo.Transaction` 包裹。第 2 或第 3 步失败会留下「已改指派人但没有流转记录」或「有流转记录但没通知」的半截状态。更糟的是第 81 行 `return s.Notify(...)`——通知插入失败会让整个接口返回失败，但缺陷与指派记录**已经落库**，客户端看到失败后重试会再插一条 `bug_assign`。

对比 `service/project.go:31-39`（`CreateProject` 用事务包住项目+成员，注释 P1-1 明确说明理由）和 `service/sync_push.go:81-94`（`execOp` 单事务），这里的缺失是遗漏而非设计。

**修复**：三步包进 `s.repo.Transaction`；`Notify` 改为 best-effort（`_ =` 并记日志），不作为返回值。

### H-17 `ExportPostman`/`OpenAPIExport` 之外：导出被静默截断到 200 条
**`internal/service/export.go:18`**

`q := page.Query{PageNum: 1, PageSize: 200}` 硬编码，`ListApis` 只取第一页。**接口数超过 200 的项目，导出的 OpenAPI 文档会静默丢掉第 201 条之后的全部接口**，响应里没有任何截断标记。用户拿这份文档去生成客户端 SDK 或迁移到新系统，会得到一个看似完整实则残缺的结果。

`ExportPostman`（postman.go:30）用的是 `ListApisByProject`（无分页，正确）——两个导出端点行为不一致。

**修复**：导出路径改用 `ListApisByProject`（repository/content.go:177）取全量；若必须限流，则在响应中返回 `truncated: true` 与 `total`。

### H-18 `GetFileByHash` 用客户端可控的 MIME 直出文件，且缺 `nosniff`
**`internal/handler/misc.go:264,291-297`**

上传时 `fileHeader.Header.Get("Content-Type")`（:264）——**multipart 分部头，完全由客户端指定**——被原样存入 `file_record.mime`（`model/business.go:131`）。下载时 misc.go:295 `c.Header("Content-Type", ct)` 直接回吐，且**没有** `X-Content-Type-Options: nosniff`、没有 `Content-Disposition`。

对比同文件的 `ServeUpload`（misc.go:324-326）：固定按扩展名映射 Content-Type（`fileutil.ContentTypeOf`）+ nosniff + inline，注释（misc.go:301）明确写着「绝不 text/html」。两个下载端点的安全姿态完全相反。

利用链：上传一个真实 PNG（通过 misc.go:240 的 `SniffAllowed` 校验，`fileutil.go:79` 只要求 `image/*` 家族），但把 multipart 分部的 `Content-Type` 设为 `text/html`；PNG 的 tEXt 块或 IEND 之后可以附加任意 HTML/JS（polyglot）。`GET /api/file/<sha256>` 即以 `text/html` 返回，在同源下执行脚本 → **存储型 XSS**，可窃取 localStorage 里的 JWT。

另外 misc.go:285-286 `local := strings.TrimPrefix(f.Path, "/uploads/"); full := filepath.Join(h.cfg.Upload.Dir, local)` **没有做 `filepath.Base` 或前缀包含校验**（对比 `ServeUpload` misc.go:304 有 `filepath.Base`）。当前 `f.Path` 只由 misc.go:264 写入所以尚不可控，但一旦 `file_record.path` 有第二个写入方（或库被直接改），就是路径穿越。

**修复**：`GetFileByHash` 改为 `fileutil.ContentTypeOf(fileutil.NormalizeExt(f.Path))`，丢弃 `f.Mime`；补 `X-Content-Type-Options: nosniff` 与 `Content-Disposition: inline`；`local` 用 `filepath.Base()` 归一，或校验 `strings.HasPrefix(filepath.Clean(full), filepath.Clean(h.cfg.Upload.Dir)+string(os.PathSeparator))`。上传时对 MIME 做白名单校验后再入库。

### H-19 `X-Forwarded-For` 无条件可信 → 登录爆破限流可被完全绕过
**`internal/middleware/ratelimit.go:133-145`** · **`internal/router/router.go:42`**

`ClientIP` 优先取 `X-Forwarded-For` 首段，**不校验请求是否来自可信代理**（Gin 的 `SetTrustedProxies` 从未调用）。攻击者每个请求换一个 XFF 值，就得到一个全新的限流桶。`AuthRateLimitPerMin` 默认 10（config.go:170）本意是防爆破，实际形同虚设。

ratelimit.go:25-30 的 `maxEntries=10000` + 溢出桶只解决了**内存放大**，没解决绕过：在填满 1 万个桶之前（1 万个请求内）攻击者完全不受限，而 1 万次密码猜测对弱口令已经足够。

叠加问题：`router.go:42` 的 `authLimit := middleware.RateLimit(...)` 在 `for` 循环**内部**构造，`/api/user/login` 与 `/api/v1/user/login` 各持一个独立限流器 → 交替请求可把 10/min 变成 20/min。

**修复**：调用 `engine.SetTrustedProxies([]string{...})`（由配置提供）并改用 `c.ClientIP()`（Gin 会按可信代理链解析）；无法确定可信代理时回退 `RemoteAddr`；把 `authLimit` 提到循环外只构造一次，让两个前缀共享同一配额；对 `/user/login` 额外增加「按用户名」的失败计数与指数退避。

### H-20 默认超管口令 `admin123` 自动创建，且明文写进日志
**`internal/config/config.go:174`** · **`cmd/server/main.go:40-46`** · **`config/config.example.yaml:51`** · **`Dockerfile:14`**

`defaults()` 内置 `Admin: {Name: "admin", Password: "admin123"}`；`main.go:40` 每次启动都调用 `EnsureAdmin` 自动创建该超管；`Dockerfile` 没有设置 `ADMIN_PASSWORD`。开箱即用的部署 = **已知口令的超级管理员**。

更糟的是 `main.go:45`：`log.Printf("default super admin created: name=%s password=%s ...", cfg.Admin.Name, cfg.Admin.Password)` —— **把超管明文口令写进标准输出/日志**。日志被采集到 ELK/Loki、被 `docker logs` 留存、被 sidecar 转发，口令随之扩散。这与 `handler/handler.go:51-53` 刻意避免泄漏敏感信息的做法自相矛盾。

**修复**：删除 main.go:45 中的 `password=%s`（只记 name，或记「已生成随机口令，请用 `apidoc-seed` 查看」）；`ADMIN_PASSWORD` 未显式设置时**随机生成**并只在首次启动打印一次（或直接拒绝启动并要求配置）；`EnsureAdmin` 创建后强制首登改密。

### H-21 `cmd/seed` 会静默重置已有超管的口令为默认值
**`cmd/seed/main.go:28`** · **`internal/service/auth.go:203-226`**

注释写「初始化/重置超级管理员账号（**幂等**）」，但 `InitAdmin` 在 auth.go:209-216 的分支是：用户已存在 → `u.Pwd = hash; u.Type = Superuser; u.State = Normal; UpdateUser`。即**无条件覆盖口令**为 `cfg.Admin.Password`（默认 `admin123`）。

运维在生产库上跑一次 `apidoc-seed`（比如为了「确保 admin 存在」），就会把已修改过的超管口令静默重置回 `admin123`，同时把被禁用的账号重新启用（`State = Normal`）。这是提权级的运维陷阱。

`EnsureAdmin`（auth.go:230-236）才是安全的那个（存在即不动），main.go:40 用的也是它——两个语义相近、行为相反的函数并存且命名无法区分。

**修复**：`InitAdmin` 拆成 `EnsureAdmin`（存在即返回）与 `ResetAdminPassword`（显式命名、要求 `--force` 或交互式确认）；seed 默认调用前者，后者需显式 flag。同时修 auth.go:231 的 `if _, err := ...; err == nil` —— 该写法把**任何数据库错误**都当成「用户不存在」并继续创建，应显式区分 `ErrNotFound`。

### H-22 Docker 部署下 JWT_SECRET 每次重启随机生成，导致全部 PAT 永久失效
**`internal/config/config.go:133-141`** · **`Dockerfile:14`** · **`internal/service/token.go:46,68`** · **`internal/pkg/crypto/crypto.go:61-63`**

`config.Load()` 在 secret 未配置或等于内置默认值时**随机生成**一个临时密钥（config.go:135-137）。`Dockerfile:14` 的 `ENV` 只设了 `PORT/DB_DRIVER/DB_DSN/UPLOAD_DIR`，**没有 `JWT_SECRET`**，`config/config.yaml` 也不存在（仓库里只有 `config.example.yaml`）→ 容器化部署必然走随机分支。

而 `cfg.JWT.Secret` 是**三把钥匙合一**：
1. JWT 签名（`jwt.Generate`，auth.go:34）；
2. PAT 摘要的 HMAC key（`token.go:46`）；
3. PAT 明文的 AES-256-GCM 密钥派生源（`token.go:68` → `crypto.go:62` 的 `sha256(secret)`）。

容器重启后：所有已签发的 JWT 立即失效（可接受），但**所有 PAT 的 `token_hash` 无法再被匹配**（`VerifyToken` 在 token.go:137 用新密钥算摘要，与库中旧摘要不等；legacy 分支 token.go:140 用的是无密钥 sha256，也匹配不上）→ **每一个 PAT 永久失效**，用户必须全部重建；`RevealToken`（token.go:122）也永久无法解密。数据是「软性损坏」——库还在，但令牌体系整体报废。

多副本部署时每个 Pod 各有独立密钥，PAT 只在签发它的那个 Pod 上可用。

**修复**：(a) 密钥分离——`JWT_SECRET`、`TOKEN_PEPPER`（HMAC）、`TOKEN_CIPHER_KEY`（AES）三个独立配置项；(b) 生产环境（`RunMode == release` 或检测到容器）**缺失密钥时直接 `log.Fatal` 拒绝启动**，而不是随机生成；(c) `Dockerfile` 文档中明确要求注入 `JWT_SECRET`；(d) `crypto.keyOf` 用 HKDF/`scrypt` 替代裸 `sha256`，避免低熵密钥被离线爆破。

---

# Medium

### M-1 `SetPermission` 不校验权限位取值，`-1` 可获得全部权限
**`internal/service/project.go:174`** · **`internal/dto/dto.go:78`** · **`internal/model/permission.go:18`**

`m.Permission = req.Permission` 直接赋值，DTO 只有 `json:"permission"` 无任何 binding 约束。`model.Has(perm, need)` 实现为 `perm&need == need`，因此 `Permission = -1`（int8，全 1）或 `15` 会通过所有位检查；`Permission = -1` 甚至让 `MyPermission` 返回一个前端无法解释的负数。负责人可给任意成员授予超出 `PermAll` 语义的位组合。

**修复**：`dto.SetPermissionReq.Permission` 加 `binding:"min=0,max=15"`；service 层再断言 `req.Permission & ^model.PermAll == 0`。

### M-2 `BaseRev == 0` 完全跳过冲突检测，可强制覆盖任意已有实体
**`internal/service/sync_push.go:176`**

`if exists && op.BaseRev > 0 && op.BaseRev != curRev` —— 注释说「新建 base_rev=0 允许」，但条件里 `op.BaseRev > 0` 使得**对已存在的实体**发送 `base_rev: 0` 也能绕过冲突判定，直接 last-write-wins 覆盖。恶意或有 bug 的客户端只要恒发 `base_rev=0`，就能无声抹掉服务端上他人的修改，乐观并发控制形同不存在。

**修复**：改为 `if exists && op.BaseRev != curRev`（新建时 `exists == false`，天然不受影响）；若需兼容老客户端，用一个显式的 `force: true` 字段并加权限位与审计。

### M-3 `sync_op` 幂等键全局唯一且由客户端生成，可跨租户投毒
**`internal/service/sync_push.go:51`** · **`internal/repository/sync.go:102-112`** · **`migrations/*/0010_sync_foundation.up.sql:39`**

`sync_op.op_id` 是**主键**（无 project 维度），`GetSyncOp(ctx, op.OpID)` 也按 op_id 全局查。客户端完全控制 op_id 取值，因此：
- 攻击者可预先占用一批 op_id，让其他项目的合法 push 命中「已处理」分支（sync_push.go:52-55）并拿到一个**属于别人项目的** `Status/ItemRev/Reason` 响应——既是 DoS 也是信息泄漏；
- 客户端复用 op_id 时，第二次 push 直接返回缓存结果，实际写入被跳过。

另外 sync_push.go:51 的 `if prev, err := ...; err == nil && prev != nil` **吞掉了查询错误**：DB 抖动时幂等检查被跳过，op 被重复执行——恰好在最需要的时刻失效。

**修复**：主键改为 `(project_id, op_id)` 复合，查询同时带上 project_id；`err != nil` 时必须返回 `rejected` 而非继续执行。

### M-4 `SyncPush` 的 ops 数量无上限，每条一个独立事务
**`internal/service/sync_push.go:49-58,81`**

`for _, op := range req.Ops` 无长度校验，`execOp` 内每条 op 开一个 `s.repo.Transaction`。一次 10 万条 ops 的请求 = 10 万个串行事务，在 SQLite 上会长时间独占写锁，阻塞全站所有写入。`SortNodes`（content.go:711-716）有同样的问题（`dto/dto.go:216` 只有 `min=1` 没有 `max`）。

**修复**：`len(req.Ops) > 500` 直接拒绝；`SortReq.Items` 加 `max=200`（与 `service/content.go:281` 的 `batchMax` 对齐）；考虑把整批 ops 放进一个事务（配合 H-16 的事务化改造）。

### M-5 `recordChange` 在独立事务中执行且静默丢弃错误，违背自身注释承诺
**`internal/service/content.go:19-60`**

`appendChangeTx` 的注释（:19-21）说「在**同一事务**内写变更流水并推进条目版本，两步必须同时成功」，但这只在 `r != nil`（sync 路径）时成立。Web 路径统一走 `recordChange`（:55-60），传 `nil` → `s.repo.Transaction` 另开一个事务，且 `_, _ =` **把返回值和错误全部丢弃**。

于是 `CreateApi`（:202 写实体）与 `recordChange`（:205 写流水）是两个独立事务：进程在两者之间崩溃 → 实体已落库但无 change_log → 客户端**永远拉不到这条新接口**，且服务端没有任何日志痕迹。这正是注释里 P1-1/B-12/B-15 声称已经修好的问题，在 Web 路径上依然存在。

`variable.go:100` 的 `logVarChange` 是同一模式的复制。

**修复**：把 Web 写路径也改成「实体写 + AppendChange + SetLastChange」单事务（参照 `sync_push.go:81-94` 的 `execOp`）；至少要把错误记进日志（`log.Printf`）而不是 `_, _ =` 全丢。

### M-6 `UpdateApi` 丢失 `source` 字段
**`internal/service/content.go:483-498`** · **`internal/model/api.go:22`**

`apiFromCreate` 逐字段映射 DTO → 实体，但**没有 `Source`**（DTO `ApiCreateReq` 里也没有该字段）。`UpdateApi`（content.go:223-229）用 `apiFromCreate` 构造新对象后 `Save`（全列更新），于是任何一次编辑都会把导入接口的 `source`（`openapi-import`/`bruno-import`/`postman-import`，见 extra.go:52-56、bruno.go:173、postman.go:234）**清空为 ""**。

后果：前端的「导入来源」标记丢失；后续「只重新导入新增项」之类的增量逻辑无法识别哪些是导入数据。

**修复**：`UpdateApi` 中 `updated.Source = a.Source`（与 content.go:226-227 保留 `ExtUID`/`LastChangeID` 的做法一致）。

### M-7 乐观锁是 TOCTOU，未落到 SQL 条件上
**`internal/service/content.go:212,220,229`** · **`internal/repository/generic.go:39-41`**

`ensureNotStale`（service.go:87-101）比较的是**读取时刻**的 `a.UpdateTime`（content.go:212 加载）与客户端传来的 `expected`，随后 content.go:229 才写入。两个并发请求可以同时通过第 220 行的检查，然后先后 `Save` —— 后写者静默覆盖先写者，乐观锁失效。`gmSave` 是无条件 `Save(v)`，不带 `WHERE update_time = ?`。

`UpdateDoc`（content.go:565）与 `UpdateVariable`（variable.go:164）同一模式。

**修复**：把校验下沉到 UPDATE 语句：`Model(&Api{}).Where("id = ? AND update_time = ?", id, expected).Updates(...)`，`RowsAffected == 0` 时返回冲突错误；或用 `SELECT ... FOR UPDATE` 把读-改-写包进一个事务。

### M-8 `ext_uid` 唯一索引是全局的，但查询是项目内的 → 跨租户阻塞
**`internal/model/api.go:8`** · **`model/group.go:16`** · **`model/business.go:18`** · **`model/api_return_sample.go:10`** · **`model/variable.go:52`** · 对照 **`internal/repository/sync.go:122,135,148,279,292`**

所有 `uk_*_ext_uid` 都是 `uniqueIndex` 单列（`WHERE is_deleted = 0 AND ext_uid <> ''`），**不带 project_id**；而 `FindApiByExtUID` 等一律 `WHERE project_id = ? AND ext_uid = ?`。约束维度与查询维度不一致：

- 同一份集合被导入两个项目、或客户端使用可预测/派生的 uid 时，第二个项目的写入会撞全局唯一约束 → `applyUpsert` 返回 `"invalid"`（sync_apply.go:98），错误信息完全无法定位原因；
- 恶意客户端可**预先占用一批 ext_uid**，阻止其他项目同步（跨租户 DoS）。

**修复**：索引改为 `uniqueIndex:uk_api_ext_uid,priority:1` on `(project_id, ext_uid)`，与查询条件对齐（迁移脚本 0010 的 16-18 行同步修改）。

### M-9 `applyUpsert` 总是把同步创建的分组标记为「接口分组」
**`internal/service/sync_apply.go:36`**

`e = &model.Group{ProjectID: req.ProjectID, Type: model.GroupTypeAPI}` —— 硬编码 `GroupTypeAPI(1)`。同步协议（`SyncPayload`）不携带分组类型，因此客户端推上来的**文档分组**会被建成接口分组。

后果：`ListGroups(ctx, pid, GroupTypeDoc)` 查不到它，文档树里目录消失；而 `ListDocGroups`（content.go:139）传 `-1` 不过滤类型，两处行为不一致。`model/group.go:4-9` 定义了 4 种类型，同步只能产出 1 种。

**修复**：`SyncPayload` 增加 `group_type` 字段（同步协议版本 `syncapi.Proto` 相应升级）；过渡期可按「该 uid 下挂的是 doc 还是 api」推断，或默认 `GroupTypeUndefined` 让两侧都能看见。

### M-10 `applyUpsert` 用零值无条件覆盖字段，与 VarEnv 分支的处理不一致
**`internal/service/sync_apply.go:39-40,77-79,117-119,156-160,227-228`** vs **`:196-198`**

group/api/doc/example/variable 五个分支都是 `e.Title = p.Title`、`e.ApiName = p.Name`、`e.Content = p.Content` 直接赋值；只有 VarEnv 分支写了 `if p.Name != "" { e.Name = p.Name }`（:196）。

因此一个只带部分字段的 payload（例如客户端只想改 priority，发 `{"priority": 5}`）会把 title/name/content **清空**。这与 `sync_payload.go:107-138` 的 `applyApiExtra` 精心设计的「键存在才覆盖、缺键不动」语义（:105-106 注释）直接矛盾——extra 里的富字段受保护，主字段反而不受保护。

**修复**：统一采用 `applyApiExtra` 的「显式存在才覆盖」语义（把 `SyncPayload` 的可选字段改为指针，或用 `map[string]json.RawMessage` 判断键是否存在）。

### M-11 `applyUpsert`/`applyDelete` 中父级查找吞掉错误，与 P1-3 修复自相矛盾
**`internal/service/sync_apply.go:42,81,121,162,239`**

主实体查找都严格走了 `lookupRejected`（:30-34 等，注释 P1-3 明确「查询出错必须中止，否则会被当成不存在导致重复创建或整条覆盖」），但**父级引用查找**全部是 `if pg, _ := r.FindGroupByExtUID(...); pg != nil` —— 错误被 `_` 丢弃。DB 抖动时 `pg == nil`，返回 `Reason: "parent_missing"`，客户端会认为「父分组不存在」而做出错误的修复动作（重建父级 / 放弃同步），而真实原因只是一次查询失败。

**修复**：父级查找同样走 `lookupRejected`，区分「确实不存在」与「查询失败」，后者返回 `Reason: "lookup failed"`。

### M-12 `sync_apply.go` 六分支重复代码约 310 行
**`internal/service/sync_apply.go:28-261`（applyUpsert）与 `:271-352`（applyDelete）**

六个实体类型的处理逻辑结构完全相同：`lookupRejected` → 不存在则新建 → 赋值 → create/update → `appendChangeTx` → 返回 `ItemRev`。差异只有「查哪个表」「赋哪些字段」「调哪个 Create/Delete」。同样的六路重复也出现在 `sync_push.go:120-175`（冲突检测）和 `sync_payload.go:17-54`（loadPayload）。

后果已经显现：M-9（Type 硬编码）、M-10（覆盖语义不一致）、M-11（父级查找吞错）都是「改了五处漏了第六处」的直接产物。`repository/sync.go` 里 `err == gorm.ErrRecordNotFound` 的判断也复制了 10 次（:105,123,136,149,183,233,258,280,293）。

**修复**：抽象一个 `syncEntity` 接口（`FindByUID / New / Apply(payload) / Create / Update / Delete`），六个实体各实现一次，`applyUpsert`/`applyDelete`/`loadPayload`/冲突检测各收敛为一个泛型函数。这能把 ~700 行压到 ~250 行，并让 M-9/M-10/M-11 这类不一致在结构上不可能发生。

### M-13 `SyncChanges` 静默丢弃载荷构造失败，客户端可能用零值覆盖本地数据
**`internal/service/sync.go:99-102`** · **`internal/service/sync_payload.go:20,30,38,44,50,146-148`**

sync.go:100 `if err == nil { it.Payload = payload }` —— 出错时 `it.Payload` 保持 nil，但 `it.Op` 仍是 `upsert`、`ItemRev` 仍是新版本号。客户端收到「upsert + 空载荷 + 新 rev」，很可能把本地实体覆盖成零值。这正是 `sync_apply.go:17-21`（P0-3）在服务端 push 方向上明确防范的场景，在 pull 方向上却没有防。

更糟的是 `sync_payload.go:144-154` 的 `docSyncPayload`：出错/未找到时**返回空 `SyncPayload{}` 而不是 error**，`loadPayload:35` 直接 `json.Marshal` 它 → 返回一个合法的空 JSON 且 `err == nil`。于是文档类实体的「查不到」会被 sync.go:101 当成成功，客户端拿到 `{"title":"","content":""}` 并**清空本地文档**。其余五个分支返回 error（虽然也被 sync.go:100 吞掉），只有 doc 这条路径连错误信号都没有。

`sync_payload.go:20,30,38,44,50` 的 `if err != nil || g == nil { return nil, errors.New("not found") }` 还把**数据库错误与「记录不存在」混为一谈**并丢弃原始错误。

**修复**：`loadPayload` 失败时必须让整条 item 失败（返回 error 给 `SyncChanges`，进而 500/重试），或至少 `it.Op = "delete"`/跳过该 item 并**不推进 cursor**；`docSyncPayload` 改为返回 `(SyncPayload, error)` 与其他五个分支对齐；区分 `ErrNotFound` 与真实 DB 错误。

### M-14 `AppendChange` 的 `FOR UPDATE` 行锁错误被丢弃
**`internal/repository/sync.go:20-24`**

```go
if db.Dialector.Name() == "postgres" {
    var proj model.Project
    _ = db.Raw("SELECT * FROM project WHERE id = ? FOR UPDATE", projectID).Scan(&proj).Error
}
```
这把行锁是 R3「保证 change_id 分配与提交有序」的**唯一机制**（注释 sync.go:14、:20 都这么说），但错误被 `_ =` 完全丢弃。锁获取失败（项目行不存在、锁超时、语句被中断）时代码照常往下走，并发 change_id 乱序的保证静默失效——而这正是该机制要防的问题。

另外 `projectID` 不存在时 `Scan` 不报错、`proj` 为零值、锁也没加上，同样静默通过。

**修复**：检查错误并返回；`RowsAffected == 0`（项目不存在）时显式报错。

### M-15 数据库连接池与 SQLite 并发参数完全未配置
**`internal/database/db.go:20-31,43`**

`gorm.Open` 之后没有任何 `SetMaxOpenConns`/`SetMaxIdleConns`/`SetConnMaxLifetime`（全仓库 grep 零命中）：
- **Postgres**：`database/sql` 默认最大连接数**无限制**，流量突增时会把 PG 的 `max_connections`（默认 100）打满，连带影响其他服务；也没有 `ConnMaxLifetime`，被中间件/防火墙静默断开的连接会被继续复用。
- **SQLite**：`sqlite.Open(cfg.DB.DSN)`（db.go:43）的 DSN 是裸文件路径 `data/apidoc.db`，**没有 `_pragma=busy_timeout`、没有 `_journal_mode=WAL`、没有 `_txlock=immediate`**，也没有 `SetMaxOpenConns(1)`。glebarez/sqlite 是纯 Go 驱动，并发写会立刻返回 `SQLITE_BUSY` 而不是等待。结合 M-5（Web 写路径的多个独立事务）与 H-12（无优雅关闭），生产 SQLite 部署下写冲突错误会频繁出现。

`db.go:26` 也没有 `Ping()` 验证连通性，配置错误要等到第一个请求才暴露。

**修复**：sqlite DSN 追加 `?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` 并 `sqlDB.SetMaxOpenConns(1)`；postgres 设 `SetMaxOpenConns(25)`、`SetMaxIdleConns(5)`、`SetConnMaxLifetime(30*time.Minute)`；`Open` 末尾加 `sqlDB.PingContext(ctx)`。

### M-16 项目软删除只标记 project 行，内容与成员关系全部保持可用
**`internal/repository/user.go:127-129`** · **`internal/service/project.go:102-107`** · **`internal/service/service.go:112-127`**

`DeleteProject` 只 `UPDATE project SET is_deleted = 1`，不动 `user_project`、`group`、`api`、`doc`、`variable`、`share_link`。而所有细粒度鉴权都走 `ensureProjectPermission` → `s.repo.Member(...)`（service.go:116），`Member` 查的是 `user_project`（仍然 `is_deleted = 0`）→ **校验通过**。

于是「已删除」项目里的接口/文档/变量/Bug 依然可以通过 `GetApi`、`ListApis`、`ListDocs`、`SyncSnapshot` 等正常读写（这些端点只校验成员关系，不再回查 project 是否存活）。删除项目在数据面上几乎是装饰性的。

（`SharePayload` 反倒做对了：share.go:116 用 `ProjectByID`（走 `active()`）→ 已删项目返回「分享的项目不存在或已删除」。）

**修复**：`ensureProjectPermission` 在成员校验之外增加项目存活校验（或在 `Member` 里 JOIN project 过滤 `project.is_deleted = 0`）；`DeleteProject` 在同一事务内级联软删成员关系与内容，或明确文档化「项目删除是可撤销的软状态，内容保留」。

### M-17 `AddMember` 对已存在成员静默忽略权限变更
**`internal/service/project.go:117-131`**

成员已存在时走 `m.IsDeleted = IsDeletedNo; return s.repo.SaveMember(ctx, m)` —— `m` 是从库里读出来的，`Permission`/`IsLeader` 保持**旧值**，`req.Permission` 被完全忽略。而成员不存在时（:119-125）却会用 `req.Permission`。

同一个 API 对「新成员」和「已有成员」语义不同，且没有任何错误提示。调用方以为改了权限，实际什么都没发生（要改权限得用另一个端点 `SetPermission`）。`repository/user.go:164-192` 的 `AddMember`（revive 分支会更新 permission）又是第三种语义。

**修复**：已存在成员时同样应用 `req.Permission`（若 > 0），或直接返回明确的业务错误「该用户已是项目成员，请用 SetPermission 修改权限」。

### M-18 `QuitProject` 不检查是否为唯一负责人
**`internal/service/project.go:179-186`**

负责人可以直接退出，`m.IsDeleted = 1` 后项目**再没有 `IsLeader == 1` 的成员**。此后 `ensureManage`（:203-217）对所有普通成员都返回「仅项目负责人可管理成员」→ 没人能再加成员、改权限、设负责人，项目变成孤儿，只有全局管理员能救。

叠加 H-7（`SetLeader` 不清旧负责人）：一边可能零负责人，一边可能多负责人，两个方向都失控。

**修复**：退出前检查 `IsLeader == 1` 且项目内无其他负责人时拒绝，提示「请先转让负责人」；或自动把负责人转给最早的成员。

### M-19 `ListVariables` 与 `ListUsers` 的邮箱唯一性/格式均无校验
**`internal/service/auth.go:118-120`** · **`internal/dto/dto.go:20,27`**

`RegisterReq.Email` 与 `UserUpdateReq.Email` 都只有 `json:"email"`，**没有 `binding:"email"`**，任意字符串都能入库。`UpdateUser`（auth.go:118-120）设置 email 时**不做 `UserTaken` 查重**（`Register` 在 auth.go:46 做了），因此两个用户可以拥有完全相同的邮箱。

`model/user_info.go:34` 的 email 参与的是复合唯一索引（见 H-15），单独重复不会被 DB 拦住。将来接入「邮箱找回密码」时，一个邮箱对应多个账号会直接导致账号接管风险。

**修复**：DTO 加 `binding:"omitempty,email,max=100"`；`UpdateUser` 修改 email 时调用 `UserTaken(ctx, "", req.Email, "", operator.ID)`；配合 H-15 建立 email 单列局部唯一索引。

### M-20 `UpdateUser` 的 `State` 无取值校验，产生「能登录但不能用 PAT」的僵尸状态
**`internal/service/auth.go:131-138`** · **`internal/dto/dto.go:30`**

`if req.State > 0 { u.State = req.State }` —— 只判正负，不校验是否在 `{1, 2}` 内（`model/user_info.go:13-16` 只定义了这两个值）。管理员可以写入 `State = 99`。

此后：登录路径 `auth.go:31` 判 `u.State == UserStateDisabled`（99 ≠ 2）→ **允许登录**；PAT 路径 `token.go:158` 判 `u.State != UserStateNormal`（99 ≠ 1）→ **拒绝**。同一账号在两条鉴权路径上结论相反。

`req.Type` 反倒有校验（auth.go:175-177 检查 `[UserTypeNormal, UserTypeSuperuser]`），但只在 `wantType` 为真时才走到。

**修复**：DTO 加 `binding:"omitempty,oneof=1 2"`（Type 同理 `oneof=1 2 3`）；service 层用 `model.IsSupportedLang` 那样的白名单函数统一校验；两处状态判断统一为 `!= UserStateNormal`（更安全的方向）。

### M-21 `UpdatePwd`/`Register` 无口令强度校验，bcrypt 72 字节截断未处理
**`internal/dto/dto.go:11,17,37`** · **`internal/service/auth.go:53,194`** · **`internal/pkg/password/password.go:14`**

三个口令字段都只有 `binding:"required"`——**一个字符的密码也能通过**。`router_test.go:192` 与 `service_test.go:60` 用的测试口令就是 `p123456`，说明实际接受弱口令。

`password.Generate` 用 `bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)`：cost 10 偏低（OWASP 当前建议 12+）；且 bcrypt **静默截断超过 72 字节的口令**，用户设了一个 100 字节的 passphrase，前 72 字节相同的另一个口令也能通过验证。

**修复**：DTO 加 `binding:"required,min=8,max=72"`；service 层增加复杂度校验（或接入 zxcvbn 类强度评估）；`bcrypt.DefaultCost` 提到 12（并压测确认登录延迟可接受）；`Generate`/`Verify` 前显式检查 `len(plain) > 72` 并报错，或先做 `sha256` 预哈希（需注意 base64 编码以避免 NUL 字节问题）。

### M-22 登录失败存在用户枚举时序侧信道
**`internal/service/auth.go:23-30`**

用户不存在时在 auth.go:25 立即返回，**不做任何哈希运算**；用户存在时才执行 `password.Verify`（bcrypt cost 10，约 60–100ms）。响应文案虽然统一为「用户名或密码错误」，但响应时间差了两个数量级，攻击者可据此枚举有效用户名。

`AuthRateLimitPerMin` 本可缓解，但已被 H-19 绕过。

**修复**：用户不存在时也对一个固定的 dummy hash 执行一次 `password.Verify`，抹平时间差。

### M-23 CORS 通配 `*`，且预检请求绕过限流
**`internal/middleware/cors.go:13-19`** · **`internal/router/router.go:18,26`**

`Access-Control-Allow-Origin: *` 无条件返回（注释自己承认「开发期宽松策略，生产应收敛为白名单」，但没有任何配置项可以收敛——`config.go` 里没有 CORS 相关字段）。任意站点都能读取所有免登录端点的响应：`/api/share/:token`（项目全量文档）、`/api/health`、`/api/meta`、`/mock/*`（含 C-1 泄漏的机密）。

`cors.go:16-19` 对 `OPTIONS` 直接 `AbortWithStatus(204)`，而 CORS 中间件在 router.go:18 注册、限流在 router.go:26 注册 → **所有 OPTIONS 请求完全不受限流**，可无限刷。

另外缺少 `Access-Control-Max-Age`（每个跨域请求都要一次预检）与 `Vary: Origin`（缓存投毒风险）。

**修复**：`config.go` 增加 `Security.AllowOrigins []string`，CORS 中间件按白名单回显具体 Origin 并设置 `Vary: Origin`；生产默认拒绝跨域；把限流中间件提到 CORS 之前，或对 OPTIONS 也计数；补 `Access-Control-Max-Age: 600`。

### M-24 上传签名用非常量时间比较
**`internal/handler/misc.go:314`** · **`internal/pkg/fileutil/fileutil.go:119-126`**

`c.Query("s") != fileutil.Sign(name, h.cfg.Upload.SignKey)` —— 普通字符串比较，短路返回。`Sign` 是 HMAC-SHA256 截断到 128 位，虽然空间够大，但非常量时间比较在理论上允许逐字节时序探测。项目里已有正确示范：`pkg/jwt/jwt.go:77` 用了 `hmac.Equal`。

**修复**：`hmac.Equal([]byte(c.Query("s")), []byte(fileutil.Sign(name, key)))`（注意先做长度检查以避免 `hmac.Equal` 对不等长的快速返回被利用——实际上 `hmac.Equal` 本身已处理）。

### M-25 内容寻址未去重，且 `GetFileByHash` 取哪一条不确定
**`internal/service/extra.go:36-42`** · **`internal/model/business.go:129`** · **`internal/repository/content.go:405-411`**

`FileRecord.Hash` 是 `index` 而非 `uniqueIndex`（business.go:129），`SaveFileWithHash` 也不查重（extra.go:37-41）。上传同一张图片两次 → 两个不同的随机文件名（`fileutil.RandomName`）+ 两条 file 记录 + 两份磁盘副本。S13「内容寻址」的去重目的完全没达成，磁盘随上传次数线性膨胀。

`GetFileByHash` 用 `First(&f)`（content.go:407）取 id 最小的一条，且**不过滤 `is_deleted`**（用的是 `r.db` 而非 `r.active(ctx)`）——已软删的记录仍会被返回并对外服务。

**修复**：`Hash` 改为 `uniqueIndex`（配合 `clause.OnConflict` 做 upsert）；`SaveFileWithHash` 先按 hash 查，命中则复用已有记录与文件路径；`GetFileByHash` 改用 `r.active(ctx)`。

### M-26 上传失败时残留孤儿文件；同一文件被读三遍
**`internal/handler/misc.go:250-268`**

`c.SaveUploadedFile`（:250）落盘后，若 `SaveFileWithHash`（:264）失败，代码直接 `fail(c, err)` 返回，**不删除已落盘的文件**（对比 :256-260 的 `os.ReadFile` 失败分支有 `os.Remove`）。数据库无记录 → 文件永远无法被引用也无法被清理，纯磁盘泄漏。

同时同一个文件被完整处理三遍：`fileHeader.Open()` + `io.ReadFull` 读前 512 字节嗅探（:232-239）→ `SaveUploadedFile` 写盘（:250）→ `os.ReadFile(dst)` **整个文件读进内存**算 sha256（:256）。`MaxMB` 可配置（config.go:69），设成 500 就是每请求 500MB 内存。

**修复**：`SaveFileWithHash` 失败时 `os.Remove(dst)`（用 `defer` + 成功标志位更稳妥）；哈希改为在 `SaveUploadedFile` 之前用 `io.TeeReader` 边读边算，或 `os.Open(dst)` + `io.Copy(hasher, f)` 流式计算。

### M-27 `TouchUserToken` 在每个 PAT 请求上写库
**`internal/service/token.go:161`** · **`internal/repository/sync.go:249-252`**

每次 PAT 鉴权成功都执行一条 `UPDATE user_token SET last_used = ?`，且错误被 `_ =` 丢弃。桌面客户端的同步轮询频率不低，这意味着**每个 API 请求都附带一次写操作**。在 SQLite 上（单写者 + 无 busy_timeout，见 M-15）这会显著加剧写锁争用；在 Postgres 上则产生大量无用 WAL 与表膨胀。

**修复**：节流写入——只有当 `time.Since(e.LastUsed) > 5*time.Minute` 时才更新；或改为异步批量刷（内存聚合 + 定时 flush）。

### M-28 `RevokeToken` 实际是硬删除，`Revoked` 字段是死代码
**`internal/router/router.go:71`** · **`internal/handler/token.go:39-52`** · **`internal/service/token.go:104-107,116,148`** · **`internal/repository/sync.go:267-272`** · **`internal/model/sync.go:69`**

路由叫 `RevokeToken`，handler 注释写「删除，不可恢复」，service 调 `DeleteToken` → repo `Unscoped().Delete()` **物理删除**。而 `model.UserToken.Revoked` 字段存在、`TokenResp.Revoked` 对外暴露、`token.go:116` 与 `:148` 都检查 `e.Revoked == 1` —— 但全仓库 grep 确认**没有任何代码把 Revoked 置为 1**。三处检查是永假的死代码，字段是死字段，API 命名与实际行为不符。

后果：无法「吊销但保留审计记录」，令牌一旦删除就查不到曾经存在过；`ListUserTokens`（repository/sync.go:225）用 `r.db` 而非 `r.active(ctx)`，也不过滤 `is_deleted`，与「软删除」的模型设计（`UserToken` 嵌入了带 `is_deleted` 的 `BaseModel`）自相矛盾。

**修复**：二选一——(a) 真正实现吊销：`UPDATE user_token SET revoked = 1`，`ListUserTokens` 保留展示，`VerifyToken` 拒绝；(b) 承认是删除：移除 `Revoked` 字段与三处检查，把路由/handler 改名为 `DeleteToken`。当前状态是最差的组合。

### M-29 `RevealToken` 用 GET 返回明文令牌，无二次认证、无审计、无缓存控制
**`internal/router/router.go:72`** · **`internal/handler/token.go:54-68`** · **`internal/pkg/response/response.go:40`**

`GET /api/user/token/reveal?id=` 返回 PAT 明文。问题叠加：
1. **GET + 查询参数**：URL 会进 nginx/网关的 access log、浏览器历史、`Referer` 头；
2. **无二次认证**：只要有一个有效 JWT（可能来自 XSS 窃取的 localStorage）就能拿到长期有效的 PAT，把「会话级凭证」升级为「长期凭证」；
3. **无审计**：`RevealToken`（service/token.go:111-127）不调用 `recordOp`，对比 `variable.go:148-150` 查看敏感变量明文时会记日志——同类操作两种待遇；
4. **无 `Cache-Control: no-store`**：`response.go:40` 只写 body，不设任何缓存头，中间代理可能缓存这个含明文的 200 响应。

**修复**：改为 `POST`；要求重新输入口令或 TOTP；调用 `recordOp` 记审计；响应加 `Cache-Control: no-store, Pragma: no-cache`。更根本地，考虑取消「可回显明文」这个需求（S15），改为只在创建时展示一次——那样 `TokenCipher` 列、`crypto` 包和 H-22 的密钥耦合问题都能一并消除。

### M-30 单一密钥承担三种密码学职责，且派生方式为裸 SHA-256
**`internal/service/token.go:46,68`** · **`internal/pkg/crypto/crypto.go:61-63`** · **`internal/config/config.go:133-141`**

`cfg.JWT.Secret` 同时是：JWT HMAC 签名密钥、PAT 摘要的 HMAC pepper、PAT 明文 AES-256-GCM 密钥的派生源。`crypto.keyOf` 只是 `sha256(secret)`——无盐、无迭代、无域分离。

后果：(a) 一处泄漏 = 可伪造任意身份（含超管）+ 可离线解密全量 PAT + 可离线比对 PAT 摘要，三重损失同时发生；(b) 低熵 secret 可被 GPU 快速爆破（无 KDF 拉伸）；(c) 无法独立轮换——换 JWT 密钥就同时废掉所有 PAT（H-22 已经证明了这个后果）；(d) 缺少域分离，理论上存在跨协议攻击面。

**修复**：拆成三个独立配置项；`keyOf` 改用 `hkdf.Expand`（以固定 info 字符串做域分离）或 `scrypt`；提供密钥轮换流程（PAT 摘要支持多版本 pepper 并存，逐个升级，参照 `token.go:51-56` 已有的 legacy 升级机制）。

### M-31 `ListShareLinks` 掩码后仍暴露令牌前 6 位；`DeleteShareLink` 注释与实现相反
**`internal/service/share.go:74,80-85,87-101`** · **`internal/repository/share.go:17-19`**

`maskToken` 保留前 6 个十六进制字符（24 位）。分享令牌总长 32 hex（128 位，share.go:145-150），泄漏 24 位把爆破空间从 2^128 降到 2^104——绝对值上仍然安全，但「掩码保留前缀」这个模式本身没有必要（列表场景不需要区分令牌），且一旦将来令牌长度缩短就会变成真问题。

更实际的问题：`share.go:87` 注释「失效一个分享链接（**软删除**）」、`model/share.go:6` 也写「删除即失效（软删除）」，但 `repository/share.go:18` 用的是 `gmHardDelete` —— **物理删除**。于是 `model/share.go:13` 的 `uniqueIndex:uk_share_token,where:is_deleted = 0` 局部索引条件毫无意义，且分享链接的审计痕迹（谁在什么时候开过分享、给谁）被彻底抹掉，`OperationLog` 里只剩一条 `share-delete`。

**修复**：`maskToken` 改为完全不回令牌（用 `id` + `remark` + `create_time` 标识）；软/硬删除二选一并同步修正注释与索引定义。

### M-32 `SharePayload` 无分页、无独立限流、无访问审计
**`internal/service/share.go:104-142`** · **`internal/repository/share.go:38-42`** · **`internal/repository/content.go:177-181`**

免登录端点一次性返回项目**全部** group + api + doc（`ListGroups(pid, -1)`、`ListApisByProject`、`DocsByProject` 三个都是无分页全量查询，`share.go:37` 的注释直接写着「不分页」）。一个有 5000 个接口、每个带若干 TEXT 列的项目，单次响应可达数十 MB，且服务端要把它们全部读进内存再序列化。

该端点只有全局限流（300/min/IP，且可被 H-19 绕过），没有针对分享令牌的独立配额；`SharePayload` 也不写任何访问日志，令牌被泄漏后无法察觉、无法追溯。

**修复**：分享视图分页或按目录懒加载；对 `/share/:token` 加独立的、按令牌维度的限流；记录访问日志（令牌 id、IP、UA、时间），并在 `ListShareLinks` 里暴露最近访问信息。

### M-33 `SendMessage` 无鉴权绑定、无频率限制，且 `randomCode` 存在潜在越界 panic
**`internal/service/misc.go:152-176`** · **`internal/handler/misc.go:155-165`**

`SendMessage(ctx, req)` 不接收 `userID`，`req.ReceiveSource`（手机号/邮箱）完全由请求体决定，也不校验格式或归属。任何登录用户可以：
- 无限写入 `message` 表（每请求一行，只有全局 300/min 限流）→ 表膨胀 DoS；
- 对任意手机号/邮箱触发验证码生成（若将来接入真实短信/邮件通道，就是短信轰炸 + 费用损耗）。

`randomCode`（:166-176）声明 `var sb [6]byte` 却循环 `for i := 0; i < n; i++` —— **`n > 6` 时数组越界 panic**。当前唯一调用点传 6，但这是一个等着被踩的地雷（`gin.Recovery()` 能挡住，但会 500）。

`model/business.go:104` 的 `Code string json:"code"` 让验证码成为可序列化字段，任何将来返回 `Message` 的端点都会直接泄漏验证码。

**修复**：`SendMessage` 接收 `userID` 并校验 `ReceiveSource` 归属；按 (userID, receive_source) 加独立限流（如 1/min、10/day）；`randomCode` 改为 `sb := make([]byte, n)`；`Message.Code` 的 json tag 改为 `"-"`。

### M-34 `UpdateBug` 无条件覆盖 Comment
**`internal/service/misc.go:40-55`**

`Title`/`Content`/`Level`/`Status` 都用 `if req.X != ""/> 0` 保护（:40-51），唯独第 52 行 `b.Comment = req.Comment` 是无条件赋值。客户端只想改状态、没带 comment 字段时，**已有评论被清空**。

`DocUpdateReq` 的 `Content`（content.go:571）与 `Icon`（:575）也是无条件覆盖，但那两处有注释说明是刻意设计（「空串表示清除图标」）；`Comment` 没有任何说明，看起来是遗漏。

**修复**：`BugUpdateReq.Comment` 改为 `*string`，nil 表示不改（与同文件 `IsTop *int8`（misc.go:53）的处理方式一致）。

### M-35 `CreateFieldMapping` 把数据库错误当成「不重复」
**`internal/service/content.go:788-790`**

```go
if _, err := s.repo.FieldMappingByField(ctx, req.ProjectID, req.Field); err == nil {
    return nil, newBiz(errcode.CodeFailed, "字段重复")
}
```
`err != nil` 有两种可能：`ErrNotFound`（确实不重复，应继续）和**真实 DB 错误**（应中止）。当前写法把后者也当成「不重复」并继续 INSERT。`model/business.go:72-78` 的 `FieldMapping` **没有 (project_id, field) 唯一索引**，DB 层无兜底 → 重复字段映射会被写入，`applyFieldMapping`（content.go:515）用 `First` 取到哪条不确定。

同类模式：`service/auth.go:209,231`（`InitAdmin`/`EnsureAdmin` 的 `err == nil` 判断）、`service/project.go:114-116`（`UserByID` 出错一律回「用户不存在」）、`service/content.go:774`（环检测出错视为无环）、`service/sync_push.go:51`（幂等查询出错跳过检查）。这是一个**贯穿全仓库的反模式**：用 `err == nil` / `err != nil` 二分代替 `errors.Is(err, ErrNotFound)` 三分。

**修复**：统一改为
```go
_, err := s.repo.FieldMappingByField(...)
switch {
case err == nil: return nil, newBiz(..., "字段重复")
case errors.Is(err, repository.ErrNotFound): // 继续
default: return nil, err
}
```
并给 `field_mapping` 加 `(project_id, field) WHERE is_deleted = 0` 局部唯一索引。建议加一条 lint 规则禁止对返回 `ErrNotFound` 的调用做 `err == nil` 判断。

### M-36 LIKE 查询未转义通配符；全文搜索走不可索引的前缀通配
**`internal/repository/search.go:43,55,68`** · **`repository/user.go:80`** · **`repository/content.go:164,248`** · **`repository/variable.go:106`**

所有关键词查询都是 `"%" + keyword + "%"` 或 `keyword + "%"` 直接拼接（参数化，**不是 SQL 注入**），但 `%` 和 `_` 未转义：
- `keyword = "%"` 匹配全表；`keyword = "_"` 匹配任意单字符 → 用户可用通配符绕过搜索意图；
- `search.go:57` 的 `description LIKE '%...%'` 与 `:70` 的 `content LIKE '%...%'` 作用在 TEXT 列上，**前导 `%` 使任何索引都不可用** → 每次搜索都是全表顺序扫描。搜索是登录后可自由调用的高频端点，构成低成本 DoS 放大器。

**修复**：抽一个 `escapeLike(s string) string`（转义 `\`、`%`、`_`）并在所有拼接点使用，SQL 侧加 `ESCAPE '\'`；全文搜索改用 SQLite FTS5 / Postgres `pg_trgm` + GIN 索引（`gin_trgm_ops` 支持 `LIKE '%x%'` 走索引）。

### M-37 `page.Query.Offset()` 的 PageNum 无上界，深分页可打库
**`internal/pkg/page/page.go:10-23`**

`Normalize()` 只夹 `PageSize`（1–200），**`PageNum` 只保证 ≥ 1，没有上界**。`Offset()` 返回 `(PageNum-1)*PageSize`。`?page_num=100000000&page_size=200` → `OFFSET 19999999800`，数据库必须扫描并跳过 200 亿行（实际会扫完全表），单次请求即可造成显著 CPU/IO 消耗。`int` 溢出在 32 位平台上还会产生负 OFFSET。

**修复**：`Normalize()` 增加 `if q.PageNum > 100000 { q.PageNum = 100000 }`；更好的做法是改用 keyset 分页（`WHERE id < ? ORDER BY id DESC LIMIT ?`），彻底消除深分页成本。

### M-38 `SyncSnapshot` 的计数与取数不在同一快照，分页可漏可重
**`internal/service/sync.go:144-171`**

6 次 `Count*All`（:146-151）与随后的 6 次 `List*Page`（:220-261）是 12 条独立查询，**不在一个事务/可重复读快照内**。并发写入时：计数阶段之后新增的行会让 `total` 偏小、区间划分错位；删除的行会让 `Order("id")` 的偏移整体前移。结果是客户端翻页时**漏掉或重复收到条目**，而 `HasMore`（:181）基于失真的 `total` 计算，可能提前结束或永不结束。

**修复**：把整个 `SyncSnapshot` 包进一个 `REPEATABLE READ`（PG）/ `BEGIN DEFERRED`（SQLite）只读事务；或改用 keyset 分页（`WHERE (type_rank, id) > (?, ?)`）消除 OFFSET 漂移。

### M-39 `CleanupChangeLog` 的 `Offset(99_999)` 是昂贵的全表扫描
**`internal/repository/sync.go:193-213`**

`Order("id DESC").Limit(1).Offset(99_999).Select("id").Scan(&keepMin)` 要数据库定位到第 10 万行——即使有 id 索引，也是 10 万次索引遍历。这在每次清理任务运行时都会执行（`job/cleanup.go:33`，默认每小时）。

同时 `:199` 的 `if cnt <= 100_000 { return 0, nil }` 意味着**只按时间清理是永远不发生的**：注释（:192）说「保留 ≥90 天或 ≥10 万条（取大）」，实际是「不到 10 万条就完全不清理」，一个低流量部署的 change_log 会保留数年。

**修复**：改用 `SELECT MIN(id) FROM (SELECT id FROM change_log ORDER BY id DESC LIMIT 100000)` 或直接 `WHERE id < (SELECT MAX(id) - 100000 FROM change_log)`；按时间清理的逻辑独立出来，不要与条数阈值耦合。

### M-40 `retentionDays <= 0` 会连带禁用 change_log 清理
**`internal/job/cleanup.go:15-18,23-24,32-33`**

`if retentionDays <= 0 { return }` 直接退出整个 `StartCleanup`，但 `cleanupChangeLog` 有自己独立的 90 天/10 万条策略（repository/sync.go:192），与 operation_log 的保留天数无关。运维设置 `LOG_RETENTION_DAYS=0`（意图：永久保留操作日志）会**顺带永久禁用 change_log 清理** → change_log 无界增长。

`cleanup.go:42` 的 `DELETE FROM operation_log WHERE create_time < ?` 也是**单条大事务、无分批**，在百万行级别的表上会长时间持有写锁（SQLite）或产生巨量 WAL（PG）。`cleanup.go:54` 的 `repository.New(db, nil)` 传入 **nil config**，任何将来解引用 `r.cfg` 的 repo 方法都会 nil panic。`cleanup.go:55` 用 `context.Background()` 而非任务的 ctx，关闭时无法中断。

**修复**：把两个清理任务的启用条件分开；DELETE 改为 `LIMIT 5000` 循环分批；`repository.New(db, cfg)` 传入真实配置（`StartCleanup` 已经收到 `cfg` 的话）或改为接收 `*repository.Repository`；使用父 ctx。

### M-41 两个后台 goroutine 都没有 panic 恢复
**`internal/job/backup.go:25-38`** · **`internal/job/cleanup.go:22-36`**

`gin.Recovery()`（router.go:18）只覆盖请求 goroutine。这两个后台 goroutine 里的任何 panic（GORM 反射错误、nil 解引用如 cleanup.go:54 的 nil cfg、驱动内部错误）都会**直接终止整个进程**，且没有栈信息落盘以外的任何补救。

**修复**：两个 goroutine 主体包一层 `defer func() { if r := recover(); r != nil { log.Printf("job panic: %v\n%s", r, debug.Stack()) } }()`，并考虑在 panic 后重启循环而不是退出。

### M-42 时间/时区处理跨数据库不一致
**`migrations/postgres/0010_sync_foundation.up.sql:32,44,52-59`（timestamptz）** vs **`migrations/sqlite/0010_sync_foundation.up.sql:32,44,52-59`（DATETIME）** · **`internal/job/cleanup.go:41`** · **`internal/service/token.go:80,151`** · **`internal/service/share.go:50,113`**

Postgres 用 `timestamptz`（存 UTC 瞬时点，读取时按会话 TimeZone 转换），SQLite 用 `DATETIME`（glebarez 驱动按 Go `time.Time` 的 Location 写文本）。所有业务比较都用 `time.Now()`（**服务器本地时区**）：
- `token.go:151` `e.ExpireAt.Before(time.Now())`
- `share.go:113` `time.Now().After(*link.ExpireTime)`
- `cleanup.go:41` `time.Now().AddDate(0,0,-retentionDays)` 与 `create_time` 列比较

在 `TZ` 未设置的容器里 Go 用 UTC，而 PG 会话时区可能是别的值；`config.go:187` 的 PG 默认 DSN 里写了 `TimeZone=UTC`，但用户自定义 DSN 时不会带。跨时区部署或 DST 切换时，PAT 过期、分享链接过期、日志清理的判定都会偏差最多一个 UTC 偏移量（几小时）。

`model/base.go:16-17` 的 `autoCreateTime`/`autoUpdateTime` 同样依赖驱动的时区行为，而 `ensureNotStale`（service.go:87-101）用 RFC3339 字符串比较 `UpdateTime` —— 客户端与服务端时区不一致时，乐观锁会误判冲突或漏判。

**修复**：全链路统一 UTC——容器设 `TZ=UTC`，PG DSN 强制 `TimeZone=UTC`，业务代码用 `time.Now().UTC()`，`ensureNotStale` 比较前对两侧做 `.UTC()` 归一；在 `database.Open` 里对 SQLite 连接执行 `PRAGMA`/DSN 参数固定时区。补一条跨时区的回归测试。

### M-43 `handler.fail` 与 `bindJSON` 泄漏内部错误细节；`Mock` 向匿名用户回吐原始错误
**`internal/handler/handler.go:59,68`** · **`internal/handler/share.go:67`**

`handler.go:51-53` 明确写了「非业务错误原文可能含 SQL/驱动细节：只落日志，对外统一文案」——这个原则是对的，但同文件 :59 与 :68 的 `bindJSON`/`bindQuery` 却直接 `"参数错误: " + err.Error()` 返回给客户端。Gin 的 validator 错误包含完整的结构体字段名、tag 名与期望类型（例如 `Key: 'ApiCreateReq.ProjectID' Error:Field validation for 'ProjectID' failed on the 'required' tag`），等于把内部 DTO 结构免费送给攻击者。

更严重的是 `handler/share.go:67`：`strings.ReplaceAll(err.Error(), `"`, "'")` 把**原始错误字符串返回给完全未认证的 `/mock` 调用者**。数据库错误会带着 SQL 片段、表名、驱动信息一起出去。而且这个手工 JSON 拼接只转义了 `"`，**不转义 `\` 和控制字符** → 错误信息里含反斜杠时产出非法 JSON，含 `\r\n` 时可能构成响应分割。`service/mock.go:43-44` 的 404 body 也是同样的手工拼接（把 `shmock.NormalizeURL(reqPath)` 这个攻击者可控的路径直接嵌进 JSON 字面量）。

**修复**：`bindJSON`/`bindQuery` 只返回字段名列表或统一文案，详细错误落日志；`handler/share.go` 与 `service/mock.go` 一律用 `json.Marshal` 构造响应体，对外只暴露预定义的错误分类（`"project_not_found"` / `"no_match"`），原始 `err` 只进日志。

### M-44 0016 迁移在建唯一索引前不去重，脏库上会直接失败
**`migrations/{postgres,sqlite}/0016_user_project_uniq.up.sql`**

`CREATE UNIQUE INDEX IF NOT EXISTS uk_user_project ON user_project(user_id, project_id) WHERE is_deleted = 0` —— 但 0016 之前**没有这个约束**，`repository/user.go:155-157` 的注释（P1-9）明确承认「原来是『查不到就 INSERT』，两个并发请求会各插一行」。也就是说存量库里**很可能已经有重复的活跃成员行**。

在这种情况下 `CREATE UNIQUE INDEX` 会直接报唯一性冲突而失败，迁移中断，且没有前置的 `DELETE ... WHERE id NOT IN (SELECT MIN(id) ...)` 去重步骤。

同类问题：0015 建 `uk_name_email_active` 前也不清理存量重复的 (name, email)；0003 建 `uk_project_title` 前同理。

**修复**：每个建唯一索引的迁移前面加一段去重 SQL（保留 `MIN(id)`，其余置 `is_deleted = 1` 或删除），并在 `migrate_test.go` 里增加「先插入重复数据再跑迁移」的脏库测试。

### M-45 SQLite 迁移不幂等，与 Postgres 版行为不对称
**`migrations/sqlite/0010_sync_foundation.up.sql:9,11,13,21`** vs **`migrations/postgres/0010_sync_foundation.up.sql:9-14,21`**

Postgres 版全部用 `ADD COLUMN IF NOT EXISTS`；SQLite 版是裸 `ALTER TABLE ... ADD COLUMN`（SQLite 语法不支持 `IF NOT EXISTS`）。重跑一次就报 `duplicate column name: ext_uid`。

同一迁移在两个方言下的可重入性不同，任何「失败后重跑」的运维操作在 SQLite 上都会炸。其他不对称：0001_init（PG 154 行 vs SQLite 35 行）、0007_variables（29 vs 12）、0008（14 vs 7）、0005_drop_database.down（11 vs 3）、0016（3 vs 7）—— `TestMigrationVersionParity` 只比文件名，这些内容差异全部不可见（H-14）。

`0010_sync_foundation.down.sql` **在两个方言下同时缺失**（其余 15 个版本都有 down）。影响：无法从 0010 回滚；`golang-migrate` 这类工具要求 up/down 成对，缺失会导致「no down migration found」而拒绝执行任何回滚，甚至拒绝启动；`change_log`/`sync_op`/`user_token` 三张表与 6 个新列一旦建立就无法通过迁移撤销，只能手工 DDL。

**修复**：SQLite 的 `ALTER TABLE ADD COLUMN` 用「查 `pragma_table_info` 后条件执行」的方式包装（或接受不幂等但在迁移框架里记录版本，保证不重跑）；补齐 `0010_sync_foundation.down.sql`（`DROP TABLE sync_op/change_log/user_token; ALTER TABLE ... DROP COLUMN ...`，注意 SQLite 3.35+ 才支持 DROP COLUMN）；`migrate_test.go` 的 `upFiles` 增加 up/down 成对性校验，并增加「同一脚本跑两遍」的幂等性测试。

### M-46 `AddMember` 的 revive 更新会同时复活多行软删记录，撞唯一索引
**`internal/repository/user.go:164-192`**

`Updates(revive)` 是 `Unscoped()` + `WHERE user_id = ? AND project_id = ?`，**不带 LIMIT**，会把所有匹配行（包括多条历史软删行）的 `is_deleted` 一起置 0。在 0016 之前的脏库里同一 (user_id, project_id) 可能有多行（正是 P1-9 描述的场景），此时该 UPDATE 会同时激活多行 → 违反 `uk_user_project` → 整个 `AddMember` 失败，用户无法被加入项目。

`user.go:161-163` 的注释说「语句各自独立、不共享事务，因此某一语句失败不会污染后续操作」，但失败就是失败，没有兜底。

**修复**：revive 前先物理清理多余的软删行（只保留 `MIN(id)`），或 revive 时加 `WHERE id = (SELECT MIN(id) FROM user_project WHERE user_id=? AND project_id=?)`；配合 M-44 的迁移前去重。

### M-47 `model/user_project.go` 上两个索引列组合完全重复
**`internal/model/user_project.go:7,11`**

同一对列 `(user_id, project_id)` 上同时定义了 `index:idx_user_project`（普通联合索引）和 `uniqueIndex:uk_user_project,where:is_deleted = 0`（局部唯一索引）。局部唯一索引已经能服务所有 `WHERE user_id=? AND project_id=? AND is_deleted=0` 的查询（`Member`、`ListMembers` 都是这个形状），普通索引基本冗余，只在极少数不带 `is_deleted` 条件的查询上有用（`repository/user.go:172,184` 的 `Unscoped()` 更新——但那是低频写路径）。

代价是每次 INSERT/UPDATE 都要维护两棵 B 树，在 SQLite 单写者模型下直接放大写锁持有时间。

**修复**：确认无查询依赖后删除 `index:idx_user_project`；`repository/user.go:172,184` 的 Unscoped 更新改为按 `id` 定位。

### M-48 `Repository` 接口是死代码，`Service` 实际依赖具体类型
**`internal/service/service.go:18-29,62,67`**

`service.go:18-29` 定义了一个 12 方法的 `Repository` 接口，注释写「便于测试替换」。但 `Service.repo` 的类型是 `*repository.Repository`（:62），`New` 的形参也是具体类型（:67）——**接口从未被使用**。

后果：(a) 误导读者以为 service 可脱离真实 DB 测试（实际所有 service 测试都要起真 SQLite，见 `service_test.go:22-42`）；(b) 接口只列了 12 个方法，而 `*repository.Repository` 有 100+ 个，接口与实现严重脱节，无法作为契约；(c) 死代码需要维护（改 repo 签名时没人会去改它）。

同类死代码/占位：`config.go:245-255` 的 `getEnvInt` 从未被调用（`applyEnvOverrides` 用的是内联的 `overrideInt` 闭包）；`content.go:834-835` 的 `var _ = repository.ErrNotFound`（注释「保证 repository 被引用」——但同文件 :14 的 import 已经被 `importScope` 等真实使用了，这行纯属多余）；`model/sync.go:69` 的 `Revoked`（见 M-28）。

**修复**：删除 `service.Repository` 接口，或者真正落地——把 `Service.repo` 改为接口类型并用 `go:generate` 生成 mock（但 100+ 方法的接口本身也是坏味道，更好的做法是按领域拆成 `UserRepo`/`ProjectRepo`/`SyncRepo` 等小接口）。删除 `getEnvInt` 与 `var _ = repository.ErrNotFound`。

### M-49 `itoa` 被手工实现三次，其中一处对负数返回空串
**`internal/service/extra.go:223-235`** · **`internal/router/router_test.go:207-219`** · **`internal/service/sync_snapshot_query_test.go:129`**

三处各自手写整数转字符串，而标准库 `strconv.Itoa`/`FormatUint` 就在手边（`handler/misc.go:11` 已经 import 了 `strconv`）。

`extra.go:223-235` 的版本有实际 bug：`for n > 0` 循环对 `n < 0` 一次都不执行，返回 `string(buf[20:])` = **空串**。当前调用点（extra.go:171、bruno.go:105、postman.go:198、variable_view.go:112）传的都是计数值不会为负，但 `joinNames`（variable_view.go:112）传的是 `len(names)`，一旦将来有人用它格式化差值或错误码就会静默产出空字符串。

**修复**：三处全部替换为 `strconv.Itoa` / `strconv.FormatUint`。

### M-50 `RunMode` 默认 `debug`，示例配置也是 `debug`
**`internal/config/config.go:158`** · **`config/config.example.yaml:10`**

`defaults()` 里 `RunMode: "debug"`，示例配置同样写 `run_mode: debug`。用户复制示例配置部署 → Gin 运行在 debug 模式：启动时打印全部路由表（含内部端点清单）、`[GIN-debug]` 日志噪声、以及 Gin 在 debug 模式下的额外警告输出。`Dockerfile:14` 也没有设置 `GIN_MODE=release`。

**修复**：默认改为 `release`，开发环境通过 `config.local.yaml` 或 `GIN_MODE=debug` 显式开启；`Dockerfile` 加 `ENV GIN_MODE=release`。

### M-51 Postgres 默认 DSN 使用 `sslmode=disable` 与硬编码口令
**`internal/config/config.go:187`**

`"host=localhost user=postgres password=postgres dbname=apidoc port=5432 sslmode=disable TimeZone=UTC"` —— 明文口令写在源码里，且默认关闭 TLS（数据库流量在网络上明文传输，含全部查询结果与凭证）。用户不配 `DB_DSN` 就会静默使用这个不安全的默认值。

**修复**：默认 DSN 改为 `sslmode=require`（或 `prefer`），口令从 `PGPASSWORD`/`~/.pgpass` 读取而不写在 DSN 里；启动时若检测到 `sslmode=disable` 且 host 非 localhost，打印警告。

### M-52 `/api/health` 不检查数据库，探针恒成功
**`internal/handler/health.go:10-12`**

`response.OK(c, gin.H{"status": "ok", "driver": "ready"})` —— 完全硬编码，不碰数据库。`"driver": "ready"` 这个值看起来是想填驱动名（`cfg.DB.Driver`）但写错了。

后果：DB 挂掉、连接池耗尽、磁盘满时，健康检查依然返回 200，编排系统继续把流量路由到坏实例，`docker healthcheck`/k8s liveness 全部失效。router.go:22-23 还特意把它放在限流之前（「探活/监控不消耗配额」），说明它确实被当作探针使用。

**修复**：拆成 `/api/health`（liveness，进程活着即可，保持现状）与 `/api/ready`（readiness，执行 `sqlDB.PingContext(ctx)` 并检查磁盘/迁移状态，失败返回非 200）；`driver` 字段填 `h.cfg.DB.Driver` 的真实值。

### M-53 `errcode.CodeFailed(14)` 承载了约 20 种语义完全不同的失败
**`internal/pkg/errcode/errcode.go:12`** · **`internal/middleware/ratelimit.go:127`**

`CodeFailed` 被用于：限流（ratelimit.go:127，注释还写着「code 14」）、数据不存在（handler.go:48）、分享链接失效/过期（share.go:111,114）、用户名或密码错误（auth.go:25,29）、项目重名（project.go:81）、字段重复（content.go:789）、环境重名（variable_env.go:27）、乐观锁冲突（service.go:98）、变量重名（variable.go:281）……

客户端无法区分「稍后重试」（限流、冲突）与「永久失败」（重名、不存在），只能一律弹一个 toast。`ratelimit.go:2-3` 的注释还解释了为什么不用 HTTP 429，但业务码层面同样没有给出可区分的信号。

另外 `CodeSuccess = 200`、`CodeForbidden = 403` 与 `CodeParamError = 22`、`CodeFailed = 14`、`CodeUnauthorized = 34` 混用了「HTTP 状态码样式」和「任意小整数」两套编号体系，容易误解。

**修复**：至少为「限流」「乐观锁冲突」「资源不存在」「唯一性冲突」分配独立业务码（前端可据此做差异化处理，如冲突时提示重新载入）；`X-Api-Code` 头（response.go:19）已经为监控暴露了业务码，扩展成本很低。

### M-54 HTTP 状态码恒为 200，且敏感响应缺 `Cache-Control`
**`internal/pkg/response/response.go:1,15-18,31,40`**

「HTTP 恒 200」是文档化的历史契约（response.go:15-18 解释得很清楚，也用 `X-Api-Code` 头做了补偿），但它带来的实际代价没有被处理：
- 中间代理/CDN 会把失败响应当成功缓存（没有任何 `Cache-Control` 头）；
- 含 PAT 明文（`handler/token.go:67`）、变量明文（`variable.go:143`）、渲染了机密的 cURL（`code.go:71-76`）的响应同样**没有 `Cache-Control: no-store`** → 可能被浏览器磁盘缓存、被代理缓存、被 `Referer` 链路上的日志记录；
- 客户端超时/重试库（axios-retry 等）无法基于状态码判断可重试性。

**修复**：短期在所有响应上加 `Cache-Control: no-store`（至少在含敏感数据的端点上），并加 `Pragma: no-cache`；中期按 response.go:18 的规划在 `/api/v2` 上恢复真实 HTTP 语义。

### M-55 上传目录默认公开可读，签名机制默认关闭
**`internal/config/config.go:72-73,164-167`** · **`config/config.example.yaml:31`** · **`internal/handler/misc.go:314`**

`Upload.SignKey` 默认为空，`misc.go:314` 的签名校验只在 `SignKey != ""` 时生效 → **默认所有上传文件公开可读**，唯一保护是 32 位十六进制的随机文件名（`fileutil.RandomName`，128 位熵，不可枚举，尚可）。

但注释（config.go:72）说明这是刻意的：「便于分享页内嵌图片」。问题在于这个默认值同时适用于**私有项目的文档图片**——任何拿到 URL 的人（前员工、日志泄漏、Referer 泄漏）都能永久访问，且没有过期机制。

**修复**：为私有文档图片提供签名 URL 路径（`Sign` 已经实现，只差默认启用 + 加时间戳过期：`Sign(name+expiry, key)` 并在 `misc.go:314` 校验 expiry）；分享页场景由服务端在生成分享 HTML 时注入短期签名 URL。

### M-56 `ListUsers` 允许任意登录用户拉取全量用户表
**`internal/service/auth.go:76-95`** · **`internal/router/router.go:65`**

`/api/user/list` 对所有登录用户开放，只对非管理员**脱敏**（清空 Email/MobileNumber/LastLoginIP/LastLoginTime，auth.go:86-93），但 `Name`、`NickName`、`Type`、`State`、`UserFace`、`ID` 全部返回，且支持 `keyword` 前缀搜索（repository/user.go:81）与分页遍历。

auth.go:77-79 的注释解释了理由（备忘指派、成员选择、作者名回显需要 id→name 映射），这个需求是真实的。但当前实现返回的字段远超「id→name 映射」所需：`Type`（角色）与 `State`（是否被禁用）是敏感的内部状态，`UserFace` 是个人头像，`keyword` 搜索使得枚举效率远高于逐页翻。任意注册用户可以在几分钟内导出全公司的账号清单（含谁是超管、谁被禁用）。

`handler/user.go:56` 的 `_ = c.ShouldBindQuery(&q)` 还吞掉了分页参数绑定错误。

**修复**：新增一个专用的精简端点 `GET /api/user/directory`，只返回 `{id, name, nick_name}`（满足指派/回显需求）；`/api/user/list` 收敛为管理员专用；`Type`/`State` 只在管理员视图返回；`UserFace` 按需单独取。

### M-57 `ListApiSamples`/`GetBug` 等使用 `gmGet`（含软删）而列表用 `active`（排除软删），读写口径不一致
**`internal/repository/content.go:274-276` vs `:283-295`** · **`internal/repository/generic.go:19-36`** · **`internal/repository/sync.go:225`**

`GetBug` 用 `gmGet`（generic.go:21，**不过滤 is_deleted**），`ListBugs` 用 `r.active`（过滤）。于是软删的 Bug 在列表里看不到，但用 id 直接查详情能查到。`ListUserTokens`（repository/sync.go:225）同样用 `r.db` 而非 `r.active`。`GetFileByHash`（content.go:407）、`ListProperties`（content.go:342）也是。

`generic.go:10-12` 的注释说明了设计意图（「读路径用 getActive，回收站与还原路径用 gmGet」），但实际调用点没有遵守——`GetBug` 是普通读路径却用了 `gmGet`。

由于 Bug/Property/Variable/ApiReturnSample 都是硬删除（`gmHardDelete`），当前不会产生实际的软删行，所以问题被掩盖了；一旦按 H-10 的建议改为软删除，这些不一致会立刻暴露成「回收站里的东西还能被详情接口读到」。

**修复**：审计所有 `gmGet` 调用点，只保留回收站/还原/purge 路径（`GetApiAny`/`GetDocAny`/`GetGroupAny` 命名已经正确表达了意图），其余改为 `getActive`；`ListUserTokens`/`GetFileByHash`/`ListProperties` 改用 `r.active(ctx)`。

### M-58 `SaveProperty` 更新不存在的 id 时静默成功
**`internal/service/misc.go:128-138`** · **`internal/repository/content.go:348-350`**

`p := &model.Property{ID: req.ID, ...}` 后直接 `UpdateProperty` → `Save(v)`。GORM 的 `Save` 对带主键的对象执行 UPDATE，`RowsAffected == 0`（id 不存在或已删除）**不报错**。接口返回成功，前端以为保存了，实际什么都没发生。

同类：`gmUpdateSort`（generic.go:44-47）、`gmSoftDelete`（:55-58）、`gmRestore`（:61-64）、`ClearVariableEnvDefault`（repository/variable.go:51-55）都不检查 `RowsAffected`。对比 `MarkNotificationRead`（repository/content.go:385-396）**做了** `RowsAffected == 0 → ErrNotFound` 的检查，注释还专门说明了 IDOR 理由——说明团队知道这个模式，只是没有推广。

**修复**：`gmSave`/`gmSoftDelete`/`gmRestore`/`gmUpdateSort` 统一检查 `res.RowsAffected == 0` 并返回 `ErrNotFound`（参照 `MarkNotificationRead` 的写法）。

### M-59 `SetDefaultVariableEnv` 与 `DeleteVariableEnv` 的多步写无事务
**`internal/service/variable_env.go:105-111,153-163`** · **`:29-44`**

三处竞态/非原子：
1. `SetDefaultVariableEnv`：`ClearVariableEnvDefault`（:105）与 `UpdateVariableEnv`（:109）是两条独立语句。中间失败 → 项目**没有任何默认环境**（旧的被清了，新的没设上）。
2. `CreateVariableEnv`：`CountVariableEnvs`（:29）与 `CreateVariableEnv`（:45）之间无原子性。两个并发请求都看到 `n == 0` → **两个都被设为默认**（:42-44），而 `SetDefaultVariableEnv` 的互斥假设被破坏，`ListVariableEnvs` 的 `ORDER BY is_default DESC`（repository/variable.go:28）排序变得不确定。
3. `DeleteVariableEnv`：`VariablesByEnv` 引用检查（:153）与 `DeleteVariableEnv`（:161）之间无原子性，且 `model/variable.go` 的 `EnvID` **没有外键约束**。并发创建的变量会指向一个已删除的环境 → 孤儿变量，`varRank`（variable_view.go:16-18）按 `EnvID` 匹配环境时永远匹配不上，变量静默失效。

`VariableEnvTaken`（:22）→ `Create`（:45）的查重也是 TOCTOU，且 `model/variable.go:30-41` 的 `VariableEnv` **没有 (project_id, name) 唯一索引**（对比 `Variable` 有 `uk_variable_key`），DB 层无兜底 → 同名环境可以并存。

**修复**：三处都用 `s.repo.Transaction` 包裹；`VariableEnv` 增加 `uniqueIndex:uk_var_env_name,where:is_deleted = 0` on `(project_id, name)`；`Variable.EnvID` 加外键或在删除环境时同事务处理其下变量。

### M-60 `applyFieldMapping` 的 N+1 查询与静默失败
**`internal/service/content.go:501-530`**

`for _, p := range params` 循环内每个参数都执行一次 `s.repo.FieldMappingByField`（:515）。一个有 100 个返回参数的接口 = 100 条独立 SQL，且发生在 `CreateApi`/`UpdateApi` 的同步路径上。

`:506-507` 的 `json.Unmarshal` 失败直接 `return`（静默跳过整个映射），`:516-517` 的查询错误 `continue`（静默跳过该字段），`:526` 的 `json.Marshal` 错误也被 `if err == nil` 吞掉。用户看到「保存成功」，但字段映射没有生效，无任何提示或日志。

同类 N+1：`service/project.go:143` 的 `ListMembers` 在循环里对每个成员调 `UserByID`（500 人的项目 = 500 条查询），且错误被 `if ...; err == nil` 吞掉。

**修复**：`applyFieldMapping` 改为一次性 `ListFieldMappings(projectID)` 建 map 后在内存里匹配；`ListMembers` 改为收集 `userIDs` 后一次 `WHERE id IN ?` 批量查（`repository/search.go:28` 的 `ProjectsByIDs` 已有同类实现可参照）。

### M-61 `ImportResult` 在事务回滚后仍带着部分计数返回
**`internal/service/extra.go:131-169`** · **`internal/service/bruno.go:51-103`** · **`internal/service/postman.go:136-196`**

三处导入都是：`var res ImportResult` 声明在事务外 → 事务闭包内累加 `res.Imported++`/`res.Skipped++`/`res.addFailure(...)` → 事务失败时 `return res, err`（extra.go:168、bruno.go:102、postman.go:195）。

回滚意味着**一条都没写进去**，但返回的 `res` 里 `Imported` 可能是 500。客户端如果只看 `res.Imported`（或 `err != nil` 时仍然展示部分结果），会向用户报告「成功导入 500 条」而实际是 0 条。

`extra.go:194-198` 的 `resolveGroup` 还有独立的静默失败：`if err := sc.repo.CreateGroup(...); err == nil { ... } return defaultGroup` —— 建组失败（DB 错误、名称超长）时**不报错**，直接把接口挂到默认分组下。用户导入后发现目录结构全乱了，且没有任何失败记录。

**修复**：事务失败时返回 `ImportResult{}` 零值（或明确标记 `RolledBack: true`）；`resolveGroup` 的错误必须向上传播（它已经在事务闭包内，返回 error 即可触发回滚）。

### M-62 `SaveFileWithHash` 无权限校验，`SaveFile` 是死代码
**`internal/service/extra.go:27-33,36-42`**

`SaveFile`（:27）在 router.go 中**没有任何路由挂载**（grep 确认只有 `SaveFileWithHash` 被 `handler/misc.go:264` 调用），是死代码。

`SaveFileWithHash` 本身只被 `UploadImg` 调用，权限由「上传端点需要登录」间接保证，但函数自身不校验 `userID`，`FileRecord` 也**没有 `user_id`/`project_id` 列**（`model/business.go:124-132`）。后果：无法追溯谁上传了哪个文件、无法按项目做配额、无法在删除项目时清理其文件、`GetFileByHash`（handler/misc.go:273）对任意登录用户返回任意文件（内容寻址本身就是全局命名空间）。

**修复**：删除 `SaveFile`；`FileRecord` 增加 `UserID`/`ProjectID` 列，`SaveFileWithHash` 接收并校验；`GetFileByHash` 按归属做权限校验（或明确文档化为全局内容寻址存储，接受该语义）。

### M-63 上传/下载端点的 `int8` 截断与错误吞没
**`internal/handler/misc.go:73-76,200-203`** · **`internal/handler/content.go:45-48,360`** · **`internal/handler/user.go:56`** · **`internal/handler/project.go:29`** · **`internal/handler/misc.go:170,199`**

`status = int8(v)` / `typ = int8(v)` 直接截断：`?status=256` → `int8(256) = 0`，`?status=257` → `1`（= `BugStatusPending`），`?type=-1` 与 `?type=255` 都变成 `-1`。攻击者可以用超范围值命中本不该可达的分支（例如 `ListGroups` 的 `typ = -1` 表示「不过滤类型」，`?type=255` 就能达到同样效果，绕过 DTO 层可能存在的 `oneof` 校验）。

7 处 `_ = c.ShouldBindQuery(&q)`（handler/user.go:56、handler/content.go:360、handler/project.go:29、handler/misc.go:72,170,199）吞掉分页参数绑定错误，非法的 `page_size=abc` 会静默变成 0 再被 `Normalize()` 改成 10，用户不知道自己的参数被忽略了。

**修复**：用 `strconv.ParseInt(s, 10, 8)` 并对超范围返回 `CodeParamError`，或在 DTO 里用 `binding:"oneof=1 2 3"` 声明合法值；`ShouldBindQuery` 的错误统一走 `bindQuery`（handler.go:66-72 已经实现好了，只是没被这些调用点使用）。

### M-64 `handler.principal` 丢弃 `ok`，未鉴权时得到 UserID=0/Type=0 的主体
**`internal/handler/handler.go:31-34`** · **`internal/middleware/auth.go:64-71`**

`p, _ := middleware.Current(c)` 忽略第二个返回值。若某个受保护路由**漏挂**了 `AuthOrPAT`（router.go 里手工维护两套前缀 × 20 个路由组，漏挂完全可能），handler 会拿到 `Principal{UserID: 0, Type: 0}` 并继续执行：`isAdmin(0)` 为 false（安全），但 `userID = 0` 会进入 `ensureProjectPermission` → `Member(ctx, 0, projectID)` → 查不到 → 403（也安全）。

所以当前是**fail-closed** 的，但这是巧合而非设计：任何以 `userID` 为写入值的操作（`CreateDoc` 的 `UserID: userID`，content.go:543；`recordOp` 的 `UserID`，service.go:75；`AppendChange` 的 `actor`）会静默写入 0，产生「无主数据」。而且 `model/user_info.go:6-10` 的 `UserType` 从 1 开始，0 是一个非法值，代码里没有任何地方断言这一点。

**修复**：`principal(c)` 改为 `principal(c) (middleware.Principal, error)`，`ok == false` 时直接 `response.Fail(c, CodeUnauthorized, "")` 并 return；或在 `Principal` 上加 `Valid() bool`（`UserID > 0 && Type >= UserTypeNormal`）并在每个 handler 入口断言。

### M-65 `ensureProjectPermission` 用 `==` 比较哨兵错误，且 `need == 0` 时无条件放行
**`internal/service/service.go:112-127`** · **`internal/model/permission.go:18`** · **`internal/service/project.go:118,208`**

两处问题：
1. `if err == repository.ErrNotFound`（service.go:117、project.go:118、project.go:208）用 `==` 而非 `errors.Is`。当前 `wrapNotFound`（repository/user.go:217-221）返回的是裸哨兵所以能匹配，但一旦有人在 repo 层加一层 `fmt.Errorf("...: %w", ErrNotFound)` 包装（这是很自然的重构），判断就会失效——好在失效方向是 fall through 到 `if err != nil { return err }`，仍然拒绝，属于 fail-closed。但 `project.go:118` 的 `AddMember` 失效后会走到 `:127` 的 `return err`，把「用户不是成员」这个正常分支变成错误返回，功能直接坏掉。
2. `model.Has(perm, need)` 实现为 `perm & need == need`，当 `need == 0` 时 `0 == 0` 恒真 → **无条件放行**。当前所有调用点传的都是 `PermQuery(1)`/`PermCreate(2)`/`PermUpdate(4)`/`PermDelete(8)` 常量，没有 0。但 `service/variable.go:205-208` 已经出现了 `need := int8(model.PermDelete); if ... { need = model.PermUpdate }` 这种**动态计算 need** 的模式，将来出现一条赋值为 0 的路径就是静默的权限绕过。

**修复**：全部改用 `errors.Is`；`model.Has` 增加 `if need == 0 { return false }` 的防御（或 `panic`，因为传 0 一定是调用方 bug）；`ensureProjectPermission` 入口断言 `need != 0`。

### M-66 GORM 查询链复用（`db := r.active(ctx)` 后接两个 finisher）
**`internal/repository/user.go:78-88`** · **`internal/repository/content.go:159-172,243-255,284-293,374-380,422-434`** · **`internal/repository/search.go`** 等约 12 处

模式是：
```go
db := r.active(ctx).Where(...)   // 构造带条件的 *gorm.DB
total, err := gmCount[T](db)     // finisher 1
err = db.Order(...).Find(&items) // finisher 2，复用同一个 db
```
GORM v2 中，非 `Session()` 派生的 `*gorm.DB` 在 finisher 执行后其 `Statement` 会被复用/污染，`Model()`、`Select()`、`Order()` 等子句可能跨查询残留。当前代码能工作（`gmCount` 只加 `Model()`，`Find(&items)` 从 `&items` 推断模型），但这是 GORM 文档明确警告的用法，任何一次重构（比如在 `gmCount` 里加 `Select("COUNT(DISTINCT x)")`）都可能让第二个查询静默带上第一个的 `Select`，产出错误的 items 而不报错。

`repository/repository.go:39-41` 的 `active()` 每次返回一个新的 `WithContext` 会话，这缓解了一部分，但赋值给 `db` 变量后复用仍然是同一个实例。

**修复**：统一改为 `db.Session(&gorm.Session{})` 派生独立会话，或把条件构造抽成 `func scope() *gorm.DB` 闭包，两个 finisher 各调一次。

---

# Low

### L-1 `dropLegacyIndexes` 用字符串拼接构造 DDL
**`internal/database/migrate.go:66`** — `db.Exec("DROP INDEX IF EXISTS " + name)`。`name` 来自同文件 :13/:18 的常量，**不可注入**，但这是全仓库唯一的 SQL 字符串拼接点（其余全部参数化，见下方「SQL 注入结论」），风格上应统一为 `fmt.Sprintf("DROP INDEX IF EXISTS %q", name)` 或加注释说明为何安全。

### L-2 `ListUsers`/`ListApis`/`ListDocs` 的 keyword 走前缀 LIKE，语义与全局搜索不一致
**`internal/repository/user.go:80`** · **`repository/content.go:164,248`** — 用 `keyword + "%"`（前缀匹配），而 `repository/search.go:43` 用 `"%" + keyword + "%"`（包含匹配）。同一个「搜索」概念在两套端点上行为不同，用户会困惑「为什么全局搜索能搜到、列表搜索搜不到」。

### L-3 `maskToken` 对短令牌返回固定 `"******"`，长度信息泄漏
**`internal/service/share.go:80-85`** — `len(tok) <= 6` 时返回 6 个星号，否则返回 `前6位 + (len-6) 个星号`。星号数量等于 `len(tok)-6`，**泄漏了令牌长度**。当前令牌恒为 32 hex，无实际影响，但若将来改用变长令牌就成了侧信道。建议固定返回 8 个星号。

### L-4 `ensureAllPresent` 只比长度，正确性依赖调用方的前置去重
**`internal/service/content.go:307-321`** — `if len(want) == len(have) { return nil }` 提前返回，跳过了逐 id 比对。当前安全（`want` 经 `normalizeIDs` 去重，`have` 来自 `WHERE id IN want` 故必为子集，长度相等即集合相等），但这个正确性论证完全依赖两个远端函数的行为，任何一处改动都会让它静默失效。建议去掉提前返回，直接做集合比对（O(n) 且无隐含前提）。

### L-5 `err == gorm.ErrRecordNotFound` 用 `==` 而非 `errors.Is`，重复 10 次
**`internal/repository/sync.go:105,123,136,149,183,233,258,280,293`** · **`repository/api_sample.go:48`** — GORM 当前返回裸哨兵所以能工作，但这是脆弱的（GORM 内部若改为包装错误即失效），且与同仓库 `wrapNotFound`（repository/user.go:218）**已经正确使用 `errors.Is`** 形成对比。建议全部收敛到 `wrapNotFound` 或 `errors.Is`。

### L-6 `handler/token.go` 与 `handler/sync.go` 直接绑定 service 层类型，破坏分层
**`internal/handler/token.go:15`（`service.CreateTokenReq`）** · **`internal/handler/sync.go:72`（`service.SyncPushReq`）** — 其余 handler 一律绑定 `dto.*`。service 层的类型变更会直接影响 HTTP 契约，且 `CreateTokenReq`（service/token.go:28-32）没有任何 binding tag，`Days` 字段无范围校验（负数 → `AddDate(0,0,-5)` 生成一个**已过期**的令牌，token.go:79-81）。

### L-7 `VerifyPAT` 用 `context.Background()`，丢失请求上下文
**`internal/handler/token.go:71-73`** — `h.svc.VerifyToken(context.Background(), raw)`。中间件签名 `verifyPAT func(raw string) (uint64, int8, error)`（middleware/auth.go:33）不带 ctx，导致 PAT 校验的两次 DB 查询（token.go:137、:154）**无法被客户端断连取消**，也没有超时。高并发下会积累无法回收的查询。建议把 `verifyPAT` 签名改为 `func(ctx context.Context, raw string) (...)`，中间件传 `c.Request.Context()`。

### L-8 `retryAfter` 与 `allow` 分别加锁，存在无害的 TOCTOU
**`internal/middleware/ratelimit.go:99-111,126`** — `allow` 释放锁后 `retryAfter` 重新加锁读同一个 window，两次读取之间窗口可能已被 GC（:62-67）删除，返回兜底值 1。只影响 `Retry-After` 头的精度，不影响限流正确性。可让 `allow` 直接返回 `(bool, int)` 一次算完。

### L-9 限流器溢出桶的硬裁剪会随机删除活跃用户的窗口
**`internal/middleware/ratelimit.go:69-78`** — 到达 `maxEntries` 后按 Go map 的**随机遍历顺序**删除条目直到降回上限，被删的可能是正在限流中的攻击者（等于给他重置了配额），也可能是正常用户（等于放行了他接下来的请求）。注释（:68）承认「多删几条比无限涨好」，是合理的权衡，但更稳妥的做法是按 `w.start` 最旧优先删除（需要额外维护一个时间堆，成本较高），或直接把 `maxEntries` 提高到远超正常并发 IP 数的水平（如 100 万，每条约 50 字节 = 50MB）。

### L-10 限流器是进程内内存态，多副本部署下配额翻倍
**`internal/middleware/ratelimit.go:19-31`** — 无 Redis/共享存储后端。N 个副本 = N 倍配额，`AuthRateLimitPerMin: 10` 在 3 副本下实际是 30/min。当前是单实例部署可以接受，但应在 `config.go` 的注释里明确说明这个限制。

### L-11 `ServeUpload` 用 `filepath.Base` 拍平了子目录
**`internal/handler/misc.go:304`** — `filepath.Base(strings.TrimPrefix(c.Param("path"), "/"))` 是正确的穿越防护，但副作用是 `/uploads/a/b/c.png` 与 `/uploads/c.png` 服务同一个文件。当前上传只用随机平铺文件名（`fileutil.RandomName`），没有子目录，所以无影响；一旦将来引入按日期分目录，这个行为会造成困惑。

### L-12 `.pdf` 在允许上传的白名单内且以 `inline` 提供
**`internal/pkg/fileutil/fileutil.go:17,111-
