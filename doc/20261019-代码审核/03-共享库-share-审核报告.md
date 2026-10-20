# api-doc-go-share 深度代码审查报告

**审查对象**：`/home/zq-deepin/my-project/api-doc-go/api-doc-go-share`（HEAD = `9b8bf10`，tag `v0.11.0`，工作区干净）
**规模**：17 个 Go 文件 / 3099 行（其中 1183 行测试，1916 行实现），7 个 Go 包 + `doc/`（**仅 1 个 Markdown，不是 Go 包**）+ `scripts/gowork.sh`
**方法**：README/CHANGELOG/doc 方案 → 逐包通读全部实现与测试 → grep（TODO/panic/`_ =`/类型断言/递归点/静默吞错）→ 交叉验证两个下游消费方（`api-doc-go/server`、`api-doc-go-client`）的真实调用点 → git tag 与 CHANGELOG 对账。
**限制说明**：本机无 Go 工具链（`go`/`gofmt` 均不存在），无法执行 `go build/vet/test`，全部结论来自静态阅读 + 下游调用点取证；涉及"下游行为"的判断均给出了具体文件行号。

---

## 一、Critical

### C-1 `codegen` Python 片段把请求体当 Python 源码直接拼接（代码注入 + 语义错误）
- **位置**：`/home/zq-deepin/my-project/api-doc-go/api-doc-go-share/codegen/codegen.go:178-180`
- **问题**：`fmt.Fprintf(&b, "payload = %s\n", body)` —— body **未经任何引用/转义**直接写进生成的 Python 代码。同一文件的 `url`/`headers` 都走了 `quote()`（:168、:172），Go 片段走了 `strconv.Quote`（:146、:154），只有 body 是裸拼。
- **证据/后果**：
  - body = `hello` → `payload = hello` → `NameError`（这正是 v0.10.3 为 fetch/axios 修掉的 S-05 同类缺陷，python 漏修）；
  - body = `{"ok":true}` → `payload = {"ok":true}` → `NameError: name 'true' is not defined`（**JSON 含布尔/null 的极常见场景直接不可运行**）；
  - body = `1\nimport os; os.system("...")` 或 `__import__('os').system('id')` → 用户复制运行即执行任意代码。
  - `CHANGELOG.md:38` 明确写着"`fetch`/`axios`/`go`/`python` 片段走各自语言的字符串字面量，本来就安全"——该断言对 python 为假。
  - 测试盲区：`codegen_test.go:54-56`、`:76-87` 对 python 只断言 `err == nil` 和"包含 URL/POST"，**从未断言过 `payload =` 那一行**，所以缺陷长期存活。
- **建议**：改为 `payload = ` + `strconv.Quote(body)` 风格的安全字面量（Python 与 Go 转义规则对 `\n`/`\'`/`\\` 兼容，但需额外处理 Python 不接受的 `\x` 之外形态；最稳妥是新增 `pyQuote()`：转义 `\`、`'`、`\n`、`\r`、以及非 ASCII 用 `\uXXXX`），并补 golden 测试：纯文本 body、含 `true/false/null` 的 JSON、多行 JSON、含 `'` 与换行的恶意 body。

### C-2 `codegen` fetch/axios/python 的 HTTP 方法未转义（片段级代码注入）
- **位置**：`codegen/codegen.go:103`（fetch `method: '%s'`）、`:126`（axios `method: '%s'`）、`:183` 与 `:185`（python `requests.request('%s', ...)`）
- **问题**：`method` 直接插进单引号字面量，**没有过 `quote()`**；而同一行的 `url` 过了 `quote()`。curl 走 `shellToken()`（:74、:86-98）、Go 走 `strconv.Quote`（:154）——五种语言里三种漏了同一个字段。
- **证据（method 确实是不可信输入）**：
  - `postman/postman.go:30` 注释"Method 与 URL 保持文件原样"，`postman.go:207` 原样透传导入文件的 `request.method`；
  - `openapi/openapi.go:31` "Method 保持文件原样，大小写由调用方决定"；
  - 两个消费方都只做 `strings.ToUpper(strings.TrimSpace(...))`，**无白名单校验**：`api-doc-go/server/internal/service/code.go:72`、`api-doc-go-client/internal/app/app_misc.go:498`。
  - 因此导入一份 method 为 `GET'; require('child_process').execSync('calc'); //` 的 Postman 集合，用户在客户端点"复制 fetch 代码"并粘贴运行即被注入。
- **建议**：所有语言统一走"字段级转义函数"，禁止裸 `%s`；更根本的做法是把 method 收敛为白名单枚举（`GET/POST/...` + 自定义需显式校验），并在 `codegen.Request` 上加 `Validate()`。补一条与 `TestSnippetCurlNoShellInjection`（:107-132）对称的 `TestSnippetJSNoInjection`，覆盖 method/url/header/body 四个字段 × fetch/axios/python 三种语言。

---

## 二、High

### H-1 `codegen.quote()` 不转义换行 → 多行 body 生成的 fetch/axios/python 片段语法错误
- **位置**：`codegen/codegen.go:191-195`
- **问题**：`quote()` 只替换 `\` 和 `'`。JS/Python 的单引号字符串**不允许内含裸换行**，因此任何多行 body（美化过的 JSON 是绝对主流）都会生成 `body: '{\n  "a": 1\n}'` 这种跨行字面量 → `SyntaxError: Invalid or unexpected token` / `unterminated string literal`。U+2028/U+2029 在 JS 中同样是行终止符，也未处理。
- **对照证据**：Go 片段用 `strconv.Quote`（:146、:154、:157）会正确输出 `\n`，说明作者知道该转义，只是 `quote()` 写漏了。下游 body 来源确认是多行原文：`api-doc-go-client/internal/app/app_misc.go:496`（`renderBodyForCode(r.Body, vars)`）。
- **建议**：`quote()` 增加 `\n`→`\\n`、`\r`→`\\r`、U+2028/U+2029 转义；或按语言拆分 `jsQuote()`/`pyQuote()`。补 golden：`Body: "{\n  \"a\": 1\n}"`。

### H-2 `openapi.exampleOf` 遍历 map → 响应示例选择**不确定**，直接违背本包的核心承诺
- **位置**：`openapi/openapi.go:261-270`（`for _, ct := range body.Content`，`Content` 是 `map[string]struct{...}`，见 :225-227）
- **问题**：当一个响应声明了多个 content type（`application/json` + `application/xml`，或 `application/json` + `application/json; charset=utf-8`）且都带 `example` 时，取到哪一个取决于 Go map 的随机遍历顺序。
- **后果**：同一份 OpenAPI 文件，**服务端导入与客户端导入可能得到不同的响应示例**；同一进程两次导入结果也可能不同。这与 `openapi.go:56` "路径与方法按字典序展开，保证顺序确定"、`CHANGELOG.md:100` "导入顺序由内核排序保证确定" 的承诺直接冲突，也是"共享库消除两端差异"这一立项动机的反例。
- **建议**：优先 `application/json`，否则按 content type 字典序取第一个；把选择规则写进 doc comment 并加守卫测试（构造双 content type 文档，循环 100 次断言结果恒定）。

### H-3 `varx` 嵌套展开无记忆化/无输出上限 → 指数级放大（小文件即可打挂两端）
- **位置**：`varx/varx.go:170-210`（`maxNestDepth = 8`；:205-207 递归；`r.cache` 只缓存内置变量，见 :212-218、:224-226）
- **问题**：深度上限只挡住"无限递归"，挡不住"指数展开"。每层对每个占位符重新跑一遍正则并重新展开其值，**同名变量的解析结果不做记忆化**。若每个变量值含 k 个占位符，工作量 ≈ k^9。
- **可利用性**：k=10 只需约 10 个 `{{a}}{{a}}...` 形式的变量定义（每个约 60 字节），**总计 ~1 KB 的集合文件**即可造成 10^9 次正则替换 → 服务端导出/Mock 渲染与客户端发送同时卡死。这是"解析外部输入"路径上的真实 DoS，且两端共用同一份代码，无法单端兜底。
- **建议**：(1) 对已解析的变量值做记忆化（`map[name]resolvedValue`，与内置缓存同层）；(2) 增加"总输出长度上限"与"总替换次数上限"，超限即返回错误/保留原文并记入 missing；(3) 补 `Fuzz` 与一条放大用例（10×8 嵌套，断言在 N 毫秒内完成）。

### H-4 `varx` 变量名文法过窄，客户端被迫私改语义 → **两端解析结果已经不一致**
- **位置**：`varx/varx.go:36`（`\$?[A-Za-z_][A-Za-z0-9_]*`）
- **问题**：名字不支持 `-`、`.` 等常见字符。`{{clxone-ipam}}` 既不被替换、也**不进 missing 名单**（`Names()` 用同一正则，:121），用户完全无感。
- **证据（这是已发生的分叉，不是假设）**：客户端专门写了一层 shim 绕过共享库——`api-doc-go-client/internal/varx/varx.go:5-10` 注释直言"共享包的占位符正则只允许 [A-Za-z0-9_]…因此本包在调用共享解析前，把含非法字符的变量名透明转写成合法别名"，实现见 :25（`broadNameRE` 允许 `_.\-`）、:143-159（`sanitizeName`）、:162-175（`rewrite`）。而服务端**没有**这层：`api-doc-go/server/internal/service/variable_resolve.go:156` 直接 `varx.Build(vars, 0).Resolve(text)`。
- **后果**：同一个 `{{clxone-ipam}}`，桌面端能解析、服务端（导出代码片段 / 内置 Mock / 变量汇总徽标）解析不了且不报缺失。这正是 README.md:26 "不在库里保留兼容分支或开关来掩盖语义分叉" 想杜绝的情况，却在库外发生了。
- **建议**：把名字文法在共享库里放宽（`[A-Za-z_][A-Za-z0-9_.\-]*`，与客户端 `broadNameRE` 对齐）并升 minor 版本，随后**删除客户端 shim**；或提供 `varx.WithNamePattern()` 选项让两端注入同一份文法。无论哪种，都必须让服务端与客户端走同一条路径。

### H-5 `varx.Builtins` 零值/半填 → 导出 API 直接 panic
- **位置**：`varx/varx.go:236-240`（`$timestamp/$isoTimestamp/$randomInt` 的闭包）、`:250-256`（`at()` 无 nil 检查）、`:146-155`（`Values()` 直接 `b.Now()`）
- **问题**：`ResolveWithBuiltins(text, tbl, Builtins{UUID: myUUID})`（只注入 UUID、忘填 Now）在遇到 `{{$timestamp}}` 时执行 `r.b.Now()` → **nil func 调用 panic**。`Builtins{}.Values()` 同样 panic。`$uuid` 分支反而安全（:242-244 有 `gen == nil` 判断），说明这是遗漏而非设计。
- **加重情节**：库内其它地方对 nil 是宽容的（`(*Table).get` 显式支持 nil 接收者，:93-96），下游有理由认为"零值可用"。共享库 panic 会同时打挂服务端请求与桌面端进程。
- **建议**：`at()` 与 `Values()` 在 `b.Now == nil` 时回落 `time.Now`，`b.UUID == nil` 时回落 `randomUUID`（即内部先 `normalize()`）；或让 `ResolveWithBuiltins` 在入口把零值字段补全。补测试：`Builtins{}`、`Builtins{UUID: ...}`、`Builtins{Now: ...}` 三种半填组合。
- **附带**：`randomUUID()`（:259-266）在 `rand.Read` 失败时静默返回 `""`，`$uuid` 会被替换成空串并被缓存（:246），调用方无从察觉——建议返回错误或在 `Builtins` 层暴露失败。

### H-6 `mock.Path` 不处理"无 scheme 的 host:port" → 常见写法永远匹配不上
- **位置**：`mock/mock.go:16-33`（只在 `://` 存在且位于首个 `/` 之前时剥离 host）
- **问题**：`localhost:8080/api/users`（API 工具里极常见的接口地址写法）没有 `://`，于是整串被当路径 → `NormalizeURL` 得到 `/localhost:8080/api/users`，而真实请求路径是 `/api/users` → `Match` 恒返回 -1，Mock 静默 404。
- **证据**：`mock.go:50-51` 的 doc comment 明确承诺"接口地址（可能带 scheme/host/模板变量）与实际请求路径都用它归一，比较口径才一致"；`mock_test.go:24-36` 的用例只覆盖了 `http://`、`https://`、`{{host}}/...`、纯路径四种，**恰好漏掉 `host:port/path`**。模板变量那一半承诺也没兑现（`{{host}}/api/x` → `/{{host}}/api/x`，`mock_test.go:28`、`:71-76` 把"匹配不上"冻结成了预期行为）。
- **建议**：在 `Path` 中增加"首段形如 `host[:port]` 且不含路径特征"的判定（或直接用 `net/url.Parse` + 首段是否含 `.`/`:` 的启发式），并明确"模板变量需先由调用方 `varx.Resolve` 再归一"的契约（写进 doc comment）。补用例：`localhost:8080/api/users`、`127.0.0.1:9000/v1/x?a=1`、`{{host}}/v1/x`（解析后）。

### H-7 `syncapi.Payload` 用"一个巨型结构 + omitempty"承载 6 种实体 → 零值与"未设置"在线上不可区分
- **位置**：`syncapi/syncapi.go:115-146`，尤其 `Priority int`(:122)、`Status int`(:135)、`Seq int`(:137)、`Secret bool`(:142)、`Deleted bool`(:145)；只有 `Enabled *bool`(:143) 是三态
- **问题**：所有字段 `omitempty`，于是 `Priority: 0`、`Secret: false`、`Status: 0`、`Seq: 0` 在 JSON 中**根本不出现**。接收方无法区分"对端把它改成了 0/false"与"对端没提这个字段"。
- **证据（危害是现实的，不是理论的）**：
  - 本包自己的测试被迫承认这一点：`syncapi_test.go:45` "带 omitempty 的字段在零值时不出现在 JSON 里，所以这里逐类型给齐值"——即**无法为零值字段写线格式断言**。
  - 服务端按"全量覆盖"消费：`api-doc-go/server/internal/service/sync_apply.go:39-41`（`e.Title = p.Title`、`e.Priority = p.Priority`），构造侧 `api-doc-go/server/internal/service/sync.go:319`（`SyncPayload{Title:…, Priority:…, Deleted:…}`）。
  - 客户端按"缺省即真"消费：`api-doc-go-client/internal/syncengine/engine.go:810`（`enabled := p.Enabled == nil || *p.Enabled`）、:815/:822（`Secret: p.Secret`）。
  - 两种消费口径并存 = 语义靠约定而非类型保证。一旦某端改成 patch 语义（或新增一个"0 有意义"的字段，如 `Seq: 0` 表示置顶），就会出现 `syncapi.go:4-5` 包注释里痛斥的那种故障："一端加了字段、另一端没加，表现是「某个字段同步丢失」且不报错"。
- **建议**：(1) 至少在包注释里把契约钉死："payload 是**全量状态**，缺失字段等价于零值，禁止按 patch 解释"，并加一条测试断言 `json.Marshal(Payload{Title:"t"})` 不含 `priority`/`secret` 键；(2) 更好的做法是拆成 `GroupPayload`/`APIPayload`/`DocPayload`/`ExamplePayload`/`VariablePayload` 五个结构 + `json.RawMessage` 分发，需要三态的字段统一用指针（`*int`/`*bool`）；(3) 提供 `MustMarshalPayload(v any) json.RawMessage` 等构造助手，避免两端各自拼字段。

### H-8 版本管理：3 个已发布 tag 无 CHANGELOG 记录，其中 v0.6.0 是**导出类型重命名（破坏性）**
- **位置**：`CHANGELOG.md`（12 条：v0.11.0/v0.10.3/v0.10.2/v0.10.1/v0.10.0/v0.9.0/v0.8.1/v0.8.0/v0.5.0/v0.4.0/v0.2.0/v0.1.0）vs `git tag`（13 个：v0.3.0…v0.11.0）
- **证据**：
  - **v0.6.0**（2026-10-08）= `9f7e8b4 fix(collection): rename Var to EnvVar to avoid JS reserved keyword` —— 导出类型 `collection.Var` 改名为 `EnvVar`，**破坏性变更，零 CHANGELOG 记录**，且按 minor 号发布。对照 v0.8.0（`EncodeURL bool→*bool`）老老实实写了"### 变更（破坏性）"（`CHANGELOG.md:60-65`），流程执行不一致。
  - **v0.7.0**（2026-10-10）= `cb3e989 refactor(collection): 重构环境名校验规则，支持更多合法字符` —— 两端共用的**校验规则行为变更**，同样无记录。
  - **v0.3.0** 无任何 CHANGELOG 记录。
  - 反向缺口：**v0.1.0 / v0.2.0 有 CHANGELOG 条目但从未打 tag**，`go get …@v0.2.0` 无法解析，文档指向不存在的发布。
  - tag 形态也不统一：v0.8.0/v0.8.1/v0.9.0/v0.10.0/v0.10.1 是 annotated tag，v0.3.0–v0.7.0/v0.10.2/v0.10.3/v0.11.0 是 lightweight tag（无 message、无 tagger 日期），说明发布靠手工。
- **建议**：补写 v0.3.0/v0.6.0/v0.7.0 三条追溯记录（v0.6.0 必须标注 BREAKING）；为 v0.1.0/v0.2.0 补 tag 或从 CHANGELOG 移除；把发布收敛成脚本（`scripts/release.sh`：校验 CHANGELOG 有对应条目 → 强制 `git tag -a` → 校验 `go.mod` go 指令 → 提示两端 `go get`），并加一条 CI/门禁断言"每个 tag 在 CHANGELOG 里都有条目"。

---

## 三、Medium

### M-1 `collection` 环境/清单/secret 文件**没有 Extra round-trip**，未知字段被静默删除
- **位置**：`collection/env.go:19-26`（`EnvFile`）、`:29-31`（`SecretsFile`）、`:62-72`（`Manifest`）；对照 `collection/request.go:78`、`:86`（`RequestFile`/`FolderFile` 有 `Extra map[string]any`）
- **问题**：`README.md:15` 宣称 collection 提供"YAML codec（未知字段 round-trip）"，但只有请求/分组文件做到了。客户端读入 `environments/dev.yml` 再写回时，任何非 `info/uid/vars` 的键（Bruno 或未来格式的字段）**被无声丢弃**。
- **建议**：给三者补 `Extra` + 复用 `unknownTopLevel`/`marshalWithExtra`（需要把 `knownTopLevel` 参数化）；或至少在 doc comment 里写明"环境文件不做未知字段保留"，避免下游误信 README。

### M-2 `MergeExtra` + `marshalWithExtra` 可产出**重复 YAML 键**，导致文件再也读不回来
- **位置**：`collection/request.go:111-125`（`MergeExtra` 不校验键名）、`:193-214`（`marshalWithExtra` 无条件把 Extra 追加到已知字段之后）
- **问题**：`knownTopLevel`（:90）只在 **解码** 时过滤。若通过 `MergeExtra(map[string]any{"http": …})`（或未来某个调用方手工塞 `f.Extra["info"]=…`）注入了已知键，编码结果会出现两个 `http:`/`info:` 映射键；yaml.v3 解码重复键会报错 → 该请求文件**永久损坏**（读不回来），而 `Encode()` 返回 nil error。
- **建议**：`marshalWithExtra` 追加前跳过 `knownTopLevel` 中的键（或返回错误）；`MergeExtra` 同样过滤。补测试：`MergeExtra({"info": 1})` 后 `Encode()` → `DecodeRequest()` 必须成功且只有一个 `info`。

### M-3 未知字段的编解码失败被静默 `continue`（数据丢失无信号）
- **位置**：`collection/request.go:181-184`（`node.Content[i+1].Decode(&v) != nil { continue }`）、`:207-212`（`value.Encode(extra[k])` 失败 `continue`）；同类：`openapi/openapi.go:110-112`（path 级 parameters 解析失败当"无参数"）、`:176-181`（`ExampleJSON` 非法则整个 example 被丢）、`postman/postman.go:226-241`/`:252-267`（url/description 两种形态都不匹配则返回 `""`）
- **问题**：全库共 6 处"吞掉错误继续走"。单看每处都有理由（`openapi.go:103` 甚至写了"宁可少几条参数，也不该让整次导入失败"），但**没有任何一处向调用方回传警告**——`Decode` 只返回 `(*Document, error)`，没有 warnings 通道。用户导入后看到"少了几个参数/示例"，无从排查。
- **建议**：为 `openapi.Decode`/`postman.Decode`/`collection.DecodeRequest` 增加可选的 `[]Warning`（或 `DecodeWithWarnings`），把"跳过了什么、为什么"结构化返回；两端 UI 统一提示。这是共享库最该沉淀的一类"两端必须一致的行为"。

### M-4 命名规则三套长度上限互相矛盾，且缺 Windows Reserved 名处理
- **位置**：`collection/name.go:19`（env 60）、`:49`（entry 120）、`:63-65`（sanitize 截到 100）
- **问题**：
  1. `ValidEntryName` 允许 120 rune，但 `SanitizeFileName` 截到 100 → 两个"都合法"的不同名字（前 100 rune 相同）会**落成同一个文件名**，条目互相覆盖。
  2. 截断发生在 `Trim(name, ". ")` 之后（:59 vs :63），所以结果可能以 `.` 或空格结尾（Windows 非法）。
  3. 两个函数都不挡 Windows 保留设备名（`CON`、`NUL`、`COM1`…`LPT9`）——而 `ValidEntryName` 的注释（:44-45）明说"名称会被直接当作文件名或目录名使用"。桌面客户端要在 Windows 上跑（仓库里有 `api-doc-go-client/scripts/build-win.ps1`），创建 `CON.yml` 会失败或行为诡异。
  4. `invalidChars`（:13）漏了 `\x7f` 与其它 Unicode 控制/格式字符。
- **建议**：统一为一个 `MaxNameLen` 常量（并让 `ValidEntryName` 的上限 == `SanitizeFileName` 的截断长度）；截断后再 `Trim(name, ". ")`；增加 Windows 保留名黑名单（含 `CON.yml` 这种"主名"判定）；`invalidChars` 补 `\x7f-\x9f`。

### M-5 `ValidEntryName` 先 TrimSpace 再判定 → 校验的字符串与使用的字符串不是同一个
- **位置**：`collection/name.go:47-53`
- **问题**：`s = strings.TrimSpace(s)` 后判定，`"用户列表 "`（尾随空格）返回 `true`，但调用方通常拿**原始**名字去建文件 → Windows 上尾随空格非法。`ValidEnvName`（:27-40）则完全不 Trim，`"dev "` 直接判非法 —— 两个"口径一致"（:46 注释如此声称）的函数对空白的处理相反。
- **建议**：`ValidEntryName` 不做 Trim（或返回规范化后的名字：`NormalizeEntryName(s) (string, bool)`），并在 doc comment 里写清"是否允许首尾空白"。补用例：`"foo "`、`" foo"`、`"foo\u00a0"`。

### M-6 `SortNodes` 文档与实现不符，且对 nil 元素 panic
- **位置**：`collection/name.go:74-85`
- **问题**：注释说"分组在前（按名称），请求在后（按 seq 升序，同 seq 再按名称）"，但代码对**所有**节点先比 `Seq` 再比 `Name`（:81-84）——一个 `Seq: 5` 的分组会排在 `Seq: 0` 的分组之后，与注释相反。`collection_test.go:116-131` 的用例里两个 folder 的 Seq 都是 0，恰好掩盖了差异。另外 `nodes[i].Type` 对 nil 元素直接 panic（无防御）。
- **建议**：明确规则（要么改注释，要么分组只按 Name）；`Node.Type` 的三个取值应导出为常量（见 L-8）；补 `Seq` 不同的分组用例与 nil 元素用例。

### M-7 `collection` 的 JSON tag 半成品状态 → 意外扩大/扭曲了对下游的 API 面
- **位置**：`collection/request.go:71-79`（只有 `GRPC` 有 json tag，`Info/Meta/HTTP/Settings/Docs/Extra` 没有）、`collection/types.go:31-37`（`Body.Raw` 是 `yaml:"raw,omitempty" json:"raw"` —— YAML 省略、JSON 恒输出）、`:35`（`Form []KV` json 无 omitempty → `"form":null`）、`collection/env.go:9-26`（`EnvVar`/`EnvFile` 全字段双 tag）
- **问题**：如果 JSON 不是受支持的表示，就不该有零散的 json tag（现在 `json.Marshal(RequestFile)` 会得到 `{"Info":…,"Meta":…,"grpc":…,"Extra":…}` 这种大小写混杂、且 `Extra` 以 Go 字段名泄漏出去的形态）；如果 JSON 是受支持的（`EnvVar` 显然是给 Wails 绑定用的），那 `RequestFile` 系列就是漏配。两种解释都说明缺一层明确决策。
- **建议**：定一条规则并写进包注释："collection 的落盘表示是 YAML；JSON tag 仅用于 Wails 绑定，绑定用到的类型必须全字段配齐"。然后要么给 `RequestFile` 全家补齐 json tag（并统一 omitempty 口径），要么把 `Body`/`GRPCBlock` 上多余的 json tag 删掉。

### M-8 `Manifest.Encode(version)` 有副作用，且 `opencollection` 版本字段从不校验
- **位置**：`collection/env.go:90-94`（`m.OpenCollection = version` 修改接收者）、`:81-88`（`DecodeManifest` 不检查版本）
- **问题**：(1) 序列化方法改调用方对象，是典型的意外副作用（连续两次 `Encode("1.0.0")`/`Encode("2.0.0")` 后 `m` 被留在 "2.0.0"）；(2) 读入侧对格式版本零校验——一份 `opencollection: 99.0` 的清单会被静默接受，"版本兼容策略"在文件格式层面完全缺位（库层面有 semver + Proto，文件格式层面什么都没有）。
- **建议**：`Encode()` 不改接收者（内部拷贝或改成 `EncodeWithVersion(version)` 返回字节）；`DecodeManifest` 校验 `OpenCollection` 是否在支持集合内，不支持时返回明确错误；把支持的格式版本作为导出常量（如 `const OpenCollectionVersion = "1.0.0"`）。

### M-9 `RequestSettings` 三态策略只做了一半
- **位置**：`collection/types.go:8-15`
- **问题**：v0.8.0 把 `EncodeURL` 从 `bool` 改成 `*bool`，理由是"零值 false 会被误当成显式关闭"（`CHANGELOG.md:64`）——完全正确，但 `TimeoutSec int`(:9) 与 `MaxRedirects int`(:11) 仍是值类型 + omitempty，**"显式设置 0 秒/0 次跳转"依然无法表达**，同一个结构体里两套语义。
- **建议**：一并改为 `*int`（破坏性，按 v0.8.0 的流程走：CHANGELOG 标注 + 两端同步升级），或在注释里写明"0 = 未设置，无法表达显式 0"。

### M-10 `openapi.Decode` 不读 `openapi`/`swagger` 键 → 任意 JSON 都"导入成功"
- **位置**：`openapi/openapi.go:196-204`（`rawDocument` 只有 `Info` 与 `Paths`）
- **问题**：没有版本闸门，也没有 magic-field 检查。后果：
  - Swagger 2.0 文档（`paths.*.get.parameters[].in == "body"` + `schema`、`host`/`basePath`）会被当 3.0 解析，产出一批 `In: "body"`、无示例的畸形参数，**不报错**；
  - OpenAPI 3.1（`examples`、`webhooks`、JSON Schema 2020-12）同样静默降级；
  - 把一份 Postman 集合或任意 `{"a":1}` 喂进来 → 返回 `&Document{}` 与 nil error，调用方无法区分"不是 OpenAPI"与"是个空文档"。
  - 证据：`openapi_test.go:116-129` 的 fixture **根本没有 `openapi` 键**，测试照样通过。
- **建议**：`rawDocument` 增加 `OpenAPI string \`json:"openapi"\`` 与 `Swagger string \`json:"swagger"\``；`Swagger != ""` → 返回"暂不支持 Swagger 2.0"；`OpenAPI` 缺失或非 `3.x` → 返回明确错误（或至少 `ErrNotOpenAPI` 哨兵错误，让两端 UI 给一致提示）。

### M-11 `openapi` 对 `$ref` 静默产出空参数/空操作
- **位置**：`openapi/openapi.go:104-114`（path 级 parameters）、`:211`（操作级 `Parameters []rawParam`）、`:215-221`（`rawParam` 无 `$ref` 字段）
- **问题**：包注释（:5）声明"不解析 $ref 与 schema"，但实现不是"跳过"而是"解析成零值"：`{"$ref": "#/components/parameters/Tenant"}` 会变成 `Param{Name:"", In:"", Required:false}` 并出现在导入结果里（一个空名参数行）；path item 级的 `{"$ref": "#/paths/~1users~1{id}"}` 则让整个路径产出 0 个操作，**无任何提示**。`openapi_test.go:86` 的 fixture 里就放了 path 级 `$ref`，但只断言了"不产生伪方法"，没断言"不产生空参数"。
- **建议**：`rawParam` 增加 `Ref string \`json:"$ref"\``，遇到 `$ref` 就跳过该参数并记 warning（配合 M-3 的 warnings 通道）；path item 含 `$ref` 时同样记 warning。

### M-12 `openapi.Encode` 丢 `operationId`、允许空/重复方法键、且无错误返回
- **位置**：`openapi/openapi.go:116-132`（`Encode`）、`:134-150`（`Operation.encode` 未写 `operationId`，而 `Decode` 在 :237 读了它）
- **问题**：
  1. 导入→导出**丢失 `operationId`**（`TestRoundTrip`（:189-213）恰好没断言这个字段，所以没被发现）；
  2. `Method == ""` 时生成 `pathItem[""]`，是非法 OpenAPI；
  3. 同 path 同方法（`GET` 与 `get`，:125 都 `ToLower`）后者静默覆盖前者；
  4. `Encode()` 签名无 error，上述问题都无法上报。
- **建议**：补写 `operationId`；`Encode()` 改为 `(*Document, error)` 或 `Encode() (map[string]any, []Warning)`，对空方法/重复 path+method 报错；`TestRoundTrip` 增加 `OperationID` 断言。

### M-13 `openapi.encodeResponses` 硬编码 `application/json` 且非法示例静默丢弃
- **位置**：`openapi/openapi.go:167-192`（:179 写死 `"application/json"`；:178 `json.Unmarshal(...) == nil` 才写 example）
- **问题**：源文档里 `application/xml`/`text/plain` 的示例，导入后再导出会被贴上 `application/json`（与 H-2 的随机选择叠加，结果既不确定也不正确）；`ExampleJSON` 若不是合法 JSON（例如导入时保留了 XML 原文），示例**整段消失**且无提示。
- **建议**：`Response` 增加 `ContentType` 字段（`Decode` 时记录命中的 content type，`Encode` 时原样写回）；示例非法 JSON 时退化为 `example` 字符串或记 warning。

### M-14 `postman.decodeItems` 丢空目录（与 `Encode` 不对称，round-trip 破坏）
- **位置**：`postman/postman.go:191-204`（`case len(it.Item) > 0` / `case it.Request != nil`，两者都不满足则**整条丢弃**）；对照 `:106-118`（`Encode` 忠实写出空目录 `"item": []`）
- **问题**：`Encode → Decode` 之后空目录消失。`postman_test.go:103`（构造 `{Name:"空目录", Folder:true}`）与 `:120-123`（断言导出结果含"空目录"）只覆盖了导出侧，`TestRoundTrip`（:127-154）没有空目录用例，所以缺陷不可见。此外：同时带 `item` 和 `request` 的节点会被当目录，**request 被丢弃**。
- **建议**：`len(it.Item) > 0 || (it.Request == nil && it.Name != "")` → 视为目录（空目录保留）；`it.Item` 与 `it.Request` 并存时保留 request 并记 warning。补 round-trip 用例（含空目录、含 `item`+`request` 并存）。

### M-15 `postman` 声称支持 v2.x，但 v2.0 的字符串型 `request` 会让整份 Decode 失败
- **位置**：`postman/postman.go:161-174`（`Request *rawRequest`）、`:187-189`（`rawResponse.Body string`）；包注释 `:1` 写"Postman Collection v2.x"
- **问题**：v2.0 集合允许 `"request": "https://..."`（裸字符串）。此时 `json.Unmarshal` 报 `cannot unmarshal string into Go struct field rawItem.item.request of type postman.rawRequest` → **整个集合导入失败**，错误信息对最终用户不可读。同理 `response[].body` 若非字符串也会炸全文。注意 `urlOf`（:226-241）与 `textOf`（:252-267）已经为 url/description 做了"字符串 or 对象"双形态兼容，说明作者知道 v2.x 的多形态问题，只是漏了 `request`。
- **建议**：给 `rawItem.Request` 写自定义 `UnmarshalJSON`（字符串 → `&rawRequest{URL: json.RawMessage(quoted)}`）；`rawResponse.Body` 同样容错；`Decode` 的错误统一包装成 `postman: 第 N 个条目解析失败: %w`。另外校验 `info.schema`（现在 `Schema` 常量只用于导出，导入侧从不检查），非 Postman 集合应明确报错而不是返回空集合。

### M-16 `postman` 导入静默丢弃 auth / 脚本 / 变量 / 非 raw 请求体
- **位置**：`postman/postman.go:151-189`（`rawCollection`/`rawItem`/`rawRequest` 只建模了 info/item/name/request(method,url,header,body,description)/response.body）、`:243-249`（`bodyOf` 只认 `mode == "raw"`）
- **问题**：真实 Postman 集合里的 `auth`（basic/bearer/apikey）、`event`（pre-request/test 脚本）、集合级与目录级 `variable`、`body.mode == formdata|urlencoded|graphql|file`、`header[].type`、`disabled` 状态**全部被丢弃**，且 `Decode` 无 warning 通道（M-3）。其中 auth 与 variable 尤其关键：`collection.Auth`（`collection/types.go:39-48`）与 `collection.EnvVar` 在共享库里都有对应模型，说明"能表达"，只是 postman 侧没接。
- **建议**：至少把 `auth` 与 `variable` 映射进中立树（`Collection.Auth`、`Collection.Variables`、`Request.Auth`），formdata/urlencoded 映射到 `[]KV`；无法映射的部分通过 warnings 明确告知（"N 个请求的脚本未导入"）。

### M-17 `postman` 用 `TrimSpace` 改写用户内容，导出硬编码 `language: json`
- **位置**：`postman/postman.go:232`、`:238`（url）、`:248`（raw body）、`:258`、`:264`（description）；`:126-132`（`options.raw.language` 恒为 `json`）
- **问题**：raw body 的尾随换行/前导缩进被吃掉（对签名类请求体、YAML/XML 原文是有损的）；导出时 XML/纯文本 body 也被标成 json，Postman 打开后语法高亮与美化都会错。
- **建议**：body 不做 Trim（url/description 可以）；`Request` 增加 `BodyLanguage`/`BodyMode` 字段，导出时按内容嗅探或原样回写。

### M-18 `mock.Match` 每次请求对每条 API 重算归一化，且不支持路径模板
- **位置**：`mock/mock.go:59-73`（:63 循环内 `NormalizeURL(apis[i].URL)`）
- **问题**：
  1. 性能：Mock 服务是热路径，`NormalizeURL` 每次分配 2 个切片 + 若干字符串（:41、:47），N 条 API × M 次请求 = O(N·M) 次重复计算，库未提供预计算索引。
  2. 功能：只支持归一化后的**全等**比较，不支持 `/users/{id}`（OpenAPI 导入的产物）或 `/users/:id` 匹配 `/users/42`。也就是说从 openapi 包导入的接口，在 mock 包里基本无法被真实请求命中——两个共享包之间缺乏协同。
- **建议**：提供 `type Matcher struct{ … }` + `NewMatcher([]Api) *Matcher`（构造时预计算归一化路径与 `map[path][]int` 索引）+ `(*Matcher).Match(method, path) int`；`Match` 保留为兼容包装。模板匹配至少支持 `{param}` 段（与 openapi 导入对齐），并在 doc comment 里写明优先级：精确 > 模板 > 同路径首条兜底。

### M-19 `varx.Build` 同优先级的 tie-break 依赖切片顺序且未文档化 → 下游出现真实的不确定性
- **位置**：`varx/varx.go:54-69`（:62-63 `if cur, exists := ranks[v.Name]; exists && cur <= rank { continue }`）
- **问题**："同名同 rank 取先出现的一条"，但 doc comment（:53）只说"按优先级去重"。而客户端恰好是**遍历 map 构造切片**的：`api-doc-go-client/internal/varx/varx.go:198-204`（`for name, value := range vars` → 全部 `ScopeProjectCommon`，同一 rank）。于是同名变量（大小写不同、或别名重写后撞名，见 :143-159 的 `sanitizeName` 去重逻辑）解析结果**每次运行可能不同**。
- **建议**：在 `Build` 的 doc comment 里明确 tie-break 规则；更稳妥的是让 tie-break 与输入顺序无关（同 rank 时按 Name 字典序，或"后出现者覆盖"并写明）；同时提供 `varx.FromMap(map[string]string, Scope) *Table` 便利构造器（内部先排序），消除下游"遍历 map 建切片"的诱因。

### M-20 `varx.Table` 没有导出的查询接口 → 消费方被迫重实现优先级选择
- **位置**：`varx/varx.go:48-51`（`vars` 私有）、`:92-99`（`get` 私有）
- **问题**：`Table` 只能 `Resolve`，无法"查某个名字最终命中哪条变量、来自哪一层"。结果服务端自己写了一遍选择循环：`api-doc-go/server/internal/service/variable_resolve.go:24-33`（`best[v.Name]` + `rank < cur.rank`），并用 `varx.Rank`（`variable_view.go:16-36`）拼装。共享库提供了 `Rank` 却没提供"用 Rank 做选择"的成品，等于把最容易写错的那半段留给了下游——这正是"两端必须一致的行为才进来"原则的漏项。
- **建议**：导出 `func (t *Table) Lookup(name string) (Var, bool)` 与 `func (t *Table) Entries() []Var`（按 rank、name 排序），让服务端删掉本地循环；`Rank` 可以保留但降为内部细节（或改名为 `RankOf` 并说明返回值只用于比较）。

### M-21 `varx` 深度截断时不记 missing，与 doc comment 相反
- **位置**：`varx/varx.go:157-158`（"有环时保留原文并记缺失"）vs `:175-178`（`depth > maxNestDepth` 直接 `return text`，不碰 `missing`）
- **问题**：循环引用/超深嵌套时，输出里留着 `{{a}}` 原文，但 `missing` 是空的 → 调用方（两端 UI 的"未定义变量"提示、服务端的变量汇总徽标）**无法察觉解析失败**。`varx_test.go:166-176` 只断言 `got != ""`，没有断言 missing，所以行为与文档的偏差没被测出来。
- **建议**：深度截断路径上把该占位符的名字记入 `missing`（或返回一个 `ErrDepthExceeded`/warning）；修正 doc comment；测试补 `len(missing) > 0` 断言。

### M-22 `varx` 默认值段的两个细节缺陷
- **位置**：`varx/varx.go:36`（正则）、`:182`（`hasDef := strings.Contains(m, ":")`）
- **问题**：
  1. **默认值不去尾部空白**：`(?:\s*:\s*([^{}]*))?\s*\}\}` 中 `[^{}]*` 贪婪，会先吃掉尾部空格，后面的 `\s*` 匹配空 → `{{port: 8080 }}` 得到 `"8080 "`（前导空白被 `\s*` 吃掉、尾导没有），这个尾随空格会被直接拼进 URL/请求头。
  2. **`hasDef` 用原始匹配串推断**：只有因为名字字符集不含 `:` 才恰好等价于"有默认段"。一旦按 H-4 放宽名字文法（允许 `.`/`-`，甚至将来允许 `:`），`hasDef` 会静默误判。
- **建议**：默认值统一 `strings.Trim(def, " \t")`（或明确不 trim 并写进文档）；`hasDef` 改为判断捕获组是否参与匹配（用 `FindStringSubmatchIndex` 检查 group 2 的偏移是否为 -1，或把默认段整体作为一个捕获组）。补用例：`{{port: 8080 }}`、`{{a:}}`、`{{ a : b }}`。

### M-23 `varx` 无转义机制，且 `Resolve` 与上下文无关（JSON/请求头注入面）
- **位置**：`varx/varx.go:33-36`、`:101-104`、`:157-168`
- **问题**：(1) 用户无法书写字面量 `{{`（没有 `\{{`、没有 raw 块），文档正文里想展示占位符语法就会被替换掉；(2) 同一个 `Resolve` 被用于 URL、请求头值、JSON 请求体（下游证据：`api-doc-go-client/internal/app/app_misc.go:473-483`、`:496`），值里含 `"`、`\`、`\r\n` 时会破坏 JSON 体结构 / 造成请求头注入。库层面既不提供上下文感知转义，也不提供 `ResolveForJSON` 之类的变体，风险全部推给下游。
- **建议**：至少提供 (a) 转义约定（如 `\{{` 或 `{{{raw}}}`）并在文档中写明；(b) `ResolveJSON(text)` 或在 `Var` 上加"值需 JSON 转义"的选项；(c) 在 doc comment 里明确"调用方负责按目标上下文校验值（请求头不得含 CR/LF）"。

### M-24 全库无输入大小/深度/输出上限，恶意输入防护完全依赖调用方与第三方库内部实现
- **位置**：`collection/request.go:146-152`、`collection/env.go:34-40`、`openapi/openapi.go:61-65`、`postman/postman.go:48-52`（均直接吃无界 `[]byte`）；`postman/postman.go:191-204`、`:106-118`（递归无深度上限）；`collection/request.go:174-191`（`Extra` 保留任意大小/任意深度的攻击者可控结构并原样回写）
- **现状评估（好消息）**：
  - 库内**没有任何 IO**（`grep` 确认：非测试文件只 import `crypto/rand`/`encoding/json`/`fmt`/`regexp`/`sort`/`strconv`/`strings`/`time`/`unicode` + `gopkg.in/yaml.v3`），所以"zip bomb / 文件读取"这一层不在库内，属于消费方职责——这点边界划得对；
  - JSON 侧有 `encoding/json` 自带的 10000 层嵌套上限兜底；
  - YAML 侧有 yaml.v3 的别名炸弹防护（`~/go/pkg/mod/gopkg.in/yaml.v3@v3.0.1/decode.go:486-490`，比例阈值 + `document contains excessive aliasing`）；
  - 正则是 RE2（线性时间，无灾难性回溯）；UUID 用 `crypto/rand`（`varx.go:11`、`:261`）；时间/UUID 可注入（`:135-144`）便于确定化测试。
- **仍存在的缺口**：无 `MaxInputSize`、无显式 `MaxDepth`（postman 递归、varx 展开的输出体积）、无 `Extra` 体积上限、无"字符串字段长度上限"。因为**两端各自设限就等于没设限**（服务端限 10 MB、客户端限 1 MB → 同一份文件两端行为不同），这类阈值恰恰属于"两端必须一致"的规则，应该沉淀在共享库。
- **建议**：导出 `const MaxImportBytes`、`MaxNestDepth`、`MaxVarValueLen` 等常量，并让 `Decode*` 在超限时返回哨兵错误（`ErrTooLarge`/`ErrTooDeep`）；`postman.decodeItems`/`Item.encode` 加 depth 参数；`varx` 加输出体积上限（配合 H-3）。

### M-25 README 与实际发布状态严重脱节，且**完全没有提到 `syncapi`**
- **位置**：`README.md:7`（"当前 tag `v0.10.3`"）vs `README.md:49`、`:52`（"当前 `v0.5.0`"、`go get …@v0.5.0`）vs 实际 HEAD tag `v0.11.0`
- **问题**：
  1. 同一份 README 里出现两个互相矛盾的"当前版本"，且都比真实 tag 落后；下游照 `:52` 执行 `go get @v0.5.0` 会拿到 6 个版本前的代码（不含 `syncapi`、不含 S-01 命令注入修复）。
  2. `README.md:11-18` 的包表格与 `:72-81` 的目录说明**都没有 `syncapi`**——而它是最新、耦合最强、"改它等于改协议"的那个包（`syncapi/syncapi.go:8-10`）。也没有 `scripts/`。
  3. `README.md:6` "Go 版本：1.25（取两个消费方的较低者）"已失效：两个消费方的 `go.mod` 都是 `go 1.26.0`（`api-doc-go/server/go.mod:3`、`api-doc-go-client/go.mod:3`），生成的 `go.work` 也写着 `go 1.26.0`。
  4. `doc/共享模块方案.md` 仍是 v1 草案，描述了并不存在的 `request`/`contract` 包与已被删除的 CI（v0.4.0 移除），作为"设计与方案文档"被 README 指向（:79）却无任何"已过时"标注。
- **建议**：README 里删掉硬编码版本号（改为"见 CHANGELOG 最新条目 / `git describe`"），补 `syncapi` 与 `scripts/` 两行；给 `doc/共享模块方案.md` 加"历史草案，实现以代码与 CHANGELOG 为准"的抬头。

### M-26 `scripts/gowork.sh` 的 go 版本取值基于硬编码假设
- **位置**：`scripts/gowork.sh:32-34`（`GOVER="$(grep -m1 '^go ' "$SERVER/go.mod" | awk '{print $2}'")`，注释："取服务端声明值（当前是最高的那个）"）
- **问题**：只读服务端的 `go.mod`。若某天客户端的 go 指令更高，workspace 会直接报错（脚本注释自己也承认"go.work 的 go 版本必须 >= 各模块 go.mod 的声明"）。另外 `grep` 无匹配时 `GOVER` 为空 → 生成 `go ` 这样的非法指令（虽然 `set -e` 会让赋值失败退出，但错误信息毫无指向性）。生成的 `go.work` 现在写着 `go 1.26.0`，与 README.md:67-68 的门禁"校验 go.mod 的 go 指令 ≤ 1.25"形成认知冲突。
- **建议**：取三个 `go.mod` 的最大值（`sort -V | tail -1`），为空则报错退出并打印是哪个文件缺 `go` 指令；顺便校验 share 自己的 go 指令 ≤ 门禁值。

### M-27 `scripts/gowork.sh` 把下游仓库的目录名硬编码进共享库，且 `--off` 解析脆弱
- **位置**：`scripts/gowork.sh:10-11`（"发布前置校验由两个下游仓库的 scripts/share-version.sh 负责"）、`:16`、`:18-23`、`:25-30`
- **问题**：
  1. **文档承诺的控制在一侧根本不存在**：`api-doc-go-client/scripts/share-version.sh` 有，但 `api-doc-go/server/` 下**没有 scripts 目录**（已确认），服务端侧无任何版本一致性门禁。而该脚本自己的注释写着"两个下游仓库必须 require 同一个 share 版本"。
  2. `--off` 只在 `$1` 位置生效：`gowork.sh /path --off` 会把 `/path` 当目标并**创建** go.work，与用户意图相反。
  3. `rm -f` 对不存在的目标静默成功，仍打印"已删除 …/go.work"（误导）。
  4. 硬编码 `$PARENT/api-doc-go/server` 与 `$PARENT/api-doc-go-client`：共享库脚本反向依赖消费方的目录布局与仓库名，换机器/换布局即失效（本机布局恰好是 `api-doc-go/api-doc-go/server`，多套了一层，说明这种假设很脆）。
  5. 无 `--help`、无未知参数校验（`gowork.sh --of` 会尝试在名为 `--of` 的目录里写文件后失败）、写完不做 `go build` 自检。
- **建议**：改成 `--off` 可出现在任意位置（先扫参数分离 flag 与 path）；`rm -f` 前判断文件是否存在再决定打印内容；下游路径改为可通过环境变量覆盖（`SERVER_DIR`/`CLIENT_DIR`）并在缺失时给出可操作的提示；加 `--help` 与未知参数报错；补 `--verify`（生成后跑 `go build ./...`）。同时要么给服务端补 `share-version.sh`，要么把 `:10-11` 的措辞改成事实。

---

## 四、Low

| # | 位置 | 问题 | 建议 |
|---|---|---|---|
| L-1 | `syncapi/syncapi.go:19-39` | 协议枚举全是**无类型**字符串常量，字段也是裸 `string`：`Op{Op: TypeGroup}`、`Item{Type: OpUpsert}` 都能编译通过，类型系统零保护；也没有 `ValidOp/ValidType/ValidStatus`、没有 `String()` | 定义 `type EntityType string`/`type OpKind string`/`type Status string` 并给字段换上；提供校验函数。破坏性，需按流程走 CHANGELOG + 两端同步 |
| L-2 | `syncapi/syncapi.go:57-64` vs `:145` | 墓碑有两种表达（`Item.Deleted` 与 `Payload.Deleted`），无"谁为准"的规则；`Op`（客户端→服务端）没有 deleted 标记，删除只能靠 `Op == OpDelete` + 空 payload，全靠口口相传 | 在包注释里写死墓碑判定规则；或删掉 `Payload.Deleted`，只留 `Item.Deleted` |
| L-3 | `syncapi/syncapi.go:75-80`、`:42-48`、`:93-98` | `Snapshot.Offset int` 与 `Cursor int64` 类型不一致；`Changes` 有 `Gap` 而 `Snapshot` 没有；`Meta.Features`、`PushReq.Ops` 无 omitempty → nil 时序列化成 `null`（JS 侧要额外判空） | 统一整型宽度；`Features`/`Ops` 加 omitempty 或保证非 nil 空切片 |
| L-4 | `syncapi/syncapi.go:15-16`、`:83-90` | 并发模型未文档化：只有 `BaseRev`/`ItemRev` 的乐观锁，**没有时间戳/版本向量**，也没写"服务端 rev 单调、客户端必须带 base_rev"、`OpID` 的幂等语义与保留期 | 在包注释补一段"冲突模型"说明（谁赢、OpID 幂等窗口、Gap 后如何恢复）；`Proto` 协商提供 `func (m Meta) Supports(proto int) bool` |
| L-5 | `codegen/codegen.go:78-80` | 用 `-d` 而非 `--data-raw`：body 以 `@` 开头时（`-d '@/etc/passwd'`）curl 会去**读本地文件**并发送其内容 | 改 `--data-raw`；同时考虑 `-H` 里含 CR/LF 的拒绝/转义 |
| L-6 | `codegen/codegen.go:74`、`:247-251` | curl/grpcurl 参数注入：URL/target 虽经 shell 引用，但以 `-`/`--` 开头的值仍会被 curl 当选项解析（引用是 shell 概念，不是 curl 概念）；两处都没有 `--` 终止符或前缀校验 | 对以 `-` 开头的 URL/target 报错或前置 `./`；grpcurl（Go flag 包）可加 `--` 终止符 |
| L-7 | `codegen/codegen.go:38-44`、`:47-67` | 支持语言集合写了三遍（`ParseLang`、校验 switch、分发 switch），且分发以 `return curlSnippet(...)` 兜底——新增语言漏改一处会**静默产出 curl**；无导出的 `Langs()` | 用 `map[Lang]func(Request) string` 单一真源，`ParseLang`/`Snippet`/`Langs()` 都从它派生；未知 lang 返回 error 而非兜底 |
| L-8 | `collection/types.go:76`、`collection/name.go:77`、`collection/request.go:90` | 字符串字面量魔法值：`Node.Type` 的 `"folder"/"request"/"doc"` 在两个文件里各写一遍且无常量；`knownTopLevel` 未导出，下游无法询问"哪些键是已知的" | 导出 `TypeFolder/TypeRequest/TypeDoc` 常量与 `KnownTopLevel()`；`syncapi` 已有 `TypeDoc` 常量，两处口径应统一 |
| L-9 | `collection/env.go:20-22`、`:64-69` | 导出类型里嵌**匿名结构体**（`Info struct{ Name string }`）：下游无法命名/构造/写辅助函数，Wails 绑定生成器对此也敏感（同文件 :7-8 的注释已经记录过一次因绑定生成而被迫改名的教训） | 提成 `EnvInfo`/`ManifestInfo`/`ManifestMeta` 具名类型 |
| L-10 | `collection/request.go:37-41` | `HTTPBlock.IsZero` 手工枚举 Body 的 5 个字段——v0.10.3 就是因为漏了 `Descs` 才出 bug（`CHANGELOG.md:20`）。将来给 `Body` 再加字段，同样的漏判会重演 | 改为 `h.Body == (Body{})`（Body 全是可比较类型，含 map 则不行 → 给 `Body` 加 `IsZero()` 方法集中判空），并加一条"新增字段必须同步 IsZero"的注释 + 反射守卫测试 |
| L-11 | `varx/varx.go:146-155` vs `:220-248` | 内置变量表维护了两份（`Values()` 与 `builtin()`），且取值路径不同（`Values()` 直接 `b.Now()`，`builtin()` 走 `at()` 记忆化）→ 加一个内置变量必须改两处，容易漂移 | 让 `Values()` 复用 `renderer`（内部构造一个 renderer 逐个取），单一真源 |
| L-12 | `varx/varx.go:153` | `$randomInt` = `now.UnixNano()%1000`：既不随机（同纳秒必撞、且一次 Resolve 内被记忆化）、取值域 0-999 也与 Postman 的 0-1000 不一致 | 用 `crypto/rand` 生成 0-1000（与 Postman 对齐），或改名/改文档说明它是"时间派生的伪随机" |
| L-13 | `mock/mock.go:9-13` | `Api.ID uint64` 在 `Match` 中从未被读取，却把消费方的数据库主键拖进了共享契约 | 删掉 `ID`（`Match` 返回下标已足够），或改为 `Match` 返回 `*Api` |
| L-14 | `mock/mock.go:36-48` | `NormalizePath` 不解析 `.`/`..`、不做百分号解码、`""` → `"/"`（空定义 URL 会匹配根路径请求） | 明确是否需要 `path.Clean` 语义与百分号解码；空输入应返回 `""` 而非 `"/"`，避免"空定义匹配一切根请求" |
| L-15 | `postman/postman.go:78-102` | `QueryPairs` 返回 `[][2]string`（导出 API 里罕见的形状，与 `postman.Header`、`collection.KV` 三种 KV 表示并存）；不做百分号解码；`#` 在 `?` 之前时（`/p#f?a=1`）会把 fragment 当 query | 定义 `type QueryPair struct{ Key, Value string }`；先切 fragment 再找 `?`；文档写明"不解码" |
| L-16 | `openapi/openapi.go:16` vs `:24` | 命名歧义：包级 `Version = "3.0.0"`（规范版本）与 `Document.Version`（info.version）同名不同义，`Encode`（:129）里两者同时出现 | 把常量改名为 `SpecVersion`（破坏性小、收益明确） |
| L-17 | `openapi/openapi.go:117`、`postman/postman.go:61`、`collection/request.go:155` | `Encode` 返回类型不统一：openapi/postman 返回 `map[string]any`（无 error，调用方还得自己 `json.Marshal`），collection 返回 `([]byte, error)` | 统一为 `Encode() ([]byte, error)`（内部 marshal，能上报 M-12 那类问题） |
| L-18 | nil 接收者策略不一致 | `(*Table).get` 显式支持 nil（`varx/varx.go:93-96`），但 `(*Document).Encode`（`openapi.go:117`）、`(*Collection).Encode`（`postman.go:61`）、`(*RequestFile).Encode`（`request.go:155`）、`(*EnvFile).Encode`（`env.go:43`）、`(*Manifest).Encode`（`env.go:91`）对 nil 一律 panic | 定一条规则（要么全部 nil-safe，要么全部不保证）并写进各包 doc |
| L-19 | 跨包命名一致性 | `codegen.KV{Name,Value}` / `collection.KV{Name,Value,Enabled,Description,Type}` / `postman.Header{Name,Value}` 三种近重复类型；`collection.DecodeRequest/DecodeEnv/DecodeFolder` vs `openapi.Decode/postman.Decode`；`collection.EnvVar` 因 Wails 保留字从 `Var` 改名（v0.6.0）而 `varx.Var` 保持原名 | 短期：在 README 的包表格里注明各 KV 类型的用途差异；长期：考虑 `share/kv` 或统一命名（`Decode*` 前缀规则写进 CONTRIBUTING） |
| L-20 | 全库 0 个 `Benchmark`/`Fuzz`/`Example`（已 grep 确认） | 对一个"解析不可信外部文件 + 做字符串展开"的库来说，缺 fuzz 是明显短板（H-3、M-24 都属于 fuzz 最容易发现的类别）；缺 Example 也让下游只能读源码学用法 | 至少给 `varx.ResolveWithBuiltins`、`openapi.Decode`、`postman.Decode`、`collection.DecodeRequest` 各加一个 `FuzzXxx`；给每个包加 1-2 个 `Example` |
| L-21 | `codegen/codegen.go:143-162`、`:122-140` | 生成的示例代码质量：Go 片段用 `panic(err)`（:155、:159）、`data, _ := io.ReadAll(...)` 吞错（:160）、有 body 时不设 `Content-Type`；axios/fetch 片段用顶层 `await`（:103、:125）只在 ESM/async 上下文合法 | Go 片段改为 `log.Fatal(err)` 并补 `Content-Type`；JS 片段包一层 `async function main() { … }` 或加注释说明运行环境 |
| L-22 | `codegen/codegen.go:246-251` | `GrpcurlSnippet`：`Service` 非空而 `Method` 空时产出尾随 `/`；`Service` 空而 `Method` 非空时方法整个丢失；全空时产出 `grpcurl ''`；函数无 error 返回 | 入参校验后返回 `(string, error)`，或至少 `Service`/`Method` 必须同时存在才输出位置参数 |

---

## 五、专项评估

### 5.1 API 设计（共享库视角）
**做得好的**：
- 边界纪律强且**可验证**：非测试代码只 import 标准库 + `gopkg.in/yaml.v3`，零 IO / 零框架 / 零网络（已 grep 确认），README.md:20 的"不包含"清单与代码一致。
- `syncapi` 用 type alias 把线协议收敛为单一真源，两端已实际 alias（`api-doc-go/server/internal/service/sync.go:25-34`、`api-doc-go-client/internal/syncengine/client.go:128-137`），并配了按 JSON 键写的守卫测试（`syncapi_test.go:44-88`）——这是本仓库最成熟的一次 API 设计。
- 可测性设计到位：`varx.Builtins` 注入时钟与 UUID（`varx.go:135-144`）。

**问题**：
- **不该导出而导出的**：`mock.Path`、`mock.NormalizePath`、`openapi.Version`、`openapi.DefaultResponseDescription`、`postman.Schema` 在两个下游**均无引用**（已 grep 计数为 0），只在包内使用；`varx.Rank` 仅 1 处引用且用途是"下游自己重实现选择逻辑"（M-20）。这些都属于"导出即承诺"，白白扩大了破坏性变更的半径。
- **该导出而没导出的**：`Table.Lookup/Entries`（M-20）、`Node.Type` 常量（L-8）、`knownTopLevel` 查询（L-8）、warnings 通道（M-3）、大小/深度上限常量（M-24）、`Langs()`（L-7）、`FromMap` 便利构造（M-19）。缺口直接把实现负担推给了下游，并已造成两端分叉（H-4）。
- **命名一致性**：见 L-15/L-16/L-17/L-19——`Encode` 的返回类型、`Decode` 的前缀、KV 类型、`Version` 的语义在包间不统一。
- **耦合点**：(1) `mock.Api.ID` 泄漏了消费方主键（L-13）；(2) `collection.EnvVar` 的命名被 Wails 绑定生成器反向约束（`env.go:7-8` 有注释记录），说明"客户端 UI 框架"实际上也是这个库的隐形约束方，但 README 的"消费方"表格里没有体现这一层；(3) `scripts/gowork.sh` 硬编码下游目录（M-27）；(4) `syncapi.Payload` 把 6 种实体的字段全塞进一个结构，任何一端加实体都要改这个共享结构（H-7）。

### 5.2 版本兼容策略
- **机制**：semver + "库内不留兼容分支，破坏性变更两端同步升级"（README.md:26、CHANGELOG.md:3）+ 下游 `share-version.sh` 门禁 + `gowork.sh` 本地联调。方向正确，CHANGELOG 的**条目质量非常高**（每条都写了"消费方需要改什么/不需要改什么"、是否逐字节兼容、还带 S-01…S-11 的缺陷编号）。
- **执行漏洞**：H-8（3 个 tag 无记录，其中 v0.6.0 是导出类型重命名；2 个有记录的版本无 tag；annotated/lightweight tag 混用）、M-25（README 版本号自相矛盾且落后 6 个版本）、M-27（服务端侧门禁缺失）。
- **缺失的一层**：**数据格式版本**没有策略。`syncapi.Proto` 管住了线协议，但集合文件格式（`Manifest.OpenCollection`）读入时从不校验（M-8），OpenAPI/Postman 导入也不校验规范版本（M-10、M-15）。也就是说"库的 semver"很严，"库所定义/消费的文件格式版本"完全裸奔。
- **0.x 语义**：README.md:85 说"语义化版本，破坏性变更必须两端同步升级"，但 0.x 下 minor 号本就可以破坏（v0.6.0 事实上就是这么用的），而 v0.8.0 又按"破坏性"郑重记录——同一套流程里两种做法并存，建议明确写死："0.x 期间，任何导出符号的签名/语义变更都在 CHANGELOG 标 BREAKING，无论 minor/patch"。

### 5.3 健壮性 / 恶意输入
- 见 M-24 的总体评估：IO 边界划得干净，第三方库自带的防护（yaml.v3 别名比例限制、encoding/json 嵌套上限、RE2 线性时间）是目前的**主要**防线，库自身没有任何显式限额。
- 最需要补的三处：`varx` 展开的指数放大（H-3，唯一可被 1 KB 输入打挂的点）、`postman` 递归无深度参数（M-24）、`collection.Extra` 无体积上限且原样回写（M-24）。
- 静默失败面偏大：全库 6 处"吞错继续"（M-3）+ `Decode` 无 warnings 通道，导致"导入成功但内容少了"成为默认失败模式——对一个导入/导出库来说，这是最需要改的健壮性姿态。
- 安全正面项：curl 的 shell 引用（v0.10.1 修复，`codegen.go:273-276` + 测试 :107-132）、`ValidEntryName` 挡 `..`/点开头（v0.10.3，`name.go:47-53` + 测试 :151-156）、`mock.Path` 的 `://` 误判修复（v0.10.2，`mock.go:18-31` + 测试 :43-47）、`crypto/rand` 而非 `math/rand`。**但 C-1/C-2/H-1 说明"转义"这件事只做了 curl 一半，其余语言与字段仍有洞。**

### 5.4 各包测试覆盖情况

| 包 | 实现行 | 测试行 | 测试数 | 覆盖到的 | **明确未覆盖的（与上文发现对应）** |
|---|---|---|---|---|---|
| `varx` | 267 | 176 | 11 | 基本替换、空值即命中、默认值段、四层优先级、跨环境隔离、内置变量、同次记忆化、掩码、`Names`、嵌套、循环不 panic | 深度上限截断时 missing 为空（M-21）；指数放大/H-3 无压力或 fuzz 用例；`Builtins` 零值 panic（H-5）；`Values()` 从未被测；名字含 `-`/`.`（H-4）；默认值尾随空白（M-22）；tie-break 顺序依赖（M-19）；nil `*Table` |
| `syncapi` | 146 | 106 | 2 | 全部 9 个结构的线格式键、11 个常量字面量、`Proto == 1` | 只有"键名守卫"，**零语义测试**（包内也确实无逻辑）；零值字段被 omitempty 吞掉的行为只在测试注释里承认（`syncapi_test.go:45`）而没有断言（H-7）；`Item.Deleted` vs `Payload.Deleted` 的取舍（L-2）；`null` vs `[]`（L-3） |
| `collection` | 398 | 272 | 9 | Bruno 超集样例解码、未知字段 round-trip、`auth` 标量/映射双形态、`SortNodes`、命名规则、env+secrets round-trip、manifest round-trip、gRPC 段与 `compress`、清单默认定义 | `MergeExtra` 注入已知键 → 重复键损坏（M-2）；`EnvFile`/`Manifest` 未知字段丢失（M-1）；非映射顶层 YAML；`SanitizeFileName` 超长截断与截断后尾点（M-4）；Windows 保留名（M-4）；`SortNodes` 分组 Seq 不同（M-6）与 nil 元素；`IsZero` 未来加字段的反射守卫（L-10）；`Manifest.Encode` 副作用（M-8） |
| `openapi` | 316 | 214 | 7 | 正常解码与排序、非法 JSON、非方法键不产伪接口、path 级参数继承与操作级覆盖、方法体非对象报错、Encode、Round-trip | **多 content type 的不确定性（H-2）**；无 `openapi` 键 / Swagger 2.0 / 3.1（M-10）；`$ref` 参数与 path item（M-11）；`Encode` 丢 `operationId`、空方法键、同 path+method 覆盖（M-12）；非法 `ExampleJSON` 与非 JSON media type（M-13）；超大/超深文档（M-24） |
| `postman` | 267 | 170 | 5 | 嵌套目录、字符串 url 与 `{raw}` url、disabled/空名请求头过滤、raw body、响应样例、中文描述、非法 JSON、Encode（含空目录）、Round-trip、`QueryPairs` | **空目录 Decode 丢失（M-14，Encode 侧测了、Decode 侧没测）**；v2.0 字符串型 `request`（M-15）；formdata/urlencoded/graphql（M-16）；`auth`/`event`/`variable`（M-16）；`info.schema` 校验（M-15）；body/description 被 Trim 的有损性（M-17）；深层嵌套（M-24） |
| `codegen` | 298 | 227 | 11 | `ParseLang`、curl golden、无 body、默认方法、五语言冒烟、单引号转义、curl 注入专项、未知语言、grpcurl（含 TLS/去重/Windows 路径/转义）、fetch/axios body 不二次序列化 | **python 的 `payload =` 行从未被断言（C-1 的直接原因）**；**method 未转义（C-2）**；**多行 body（H-1）**；`-d` vs `--data-raw`（L-5）；以 `-` 开头的 URL（L-6）；`shellToken("")`；grpcurl 的 Service/Method 缺一（L-22） |
| `mock` | 74 | 88 | 4 | `NormalizePath`（7 例）、`NormalizeURL`（4 例）、`Path`（6 例，含 `://` 陷阱）、`Match`（7 种情形，含兜底与空列表） | **`host:port/path` 无 scheme（H-6）**；百分号编码与 `.`/`..` 段（L-14）；路径模板 `{id}`（M-18）；大列表性能（M-18）；空 URL 匹配根路径（L-14） |
| `doc/` | — | — | — | 非 Go 包，仅 `共享模块方案.md`（v1 草案） | 内容与实现已脱节：描述了不存在的 `request`/`contract` 包与已删除的 CI（M-25） |
| `scripts/` | 49 行 bash | 0 | 0 | — | 无任何测试/`shellcheck` 门禁；M-26、M-27 的全部缺陷都无回归保护 |

**总计**：49 个测试函数，0 benchmark / 0 fuzz / 0 example。测试风格是"golden + 缺陷回归守卫"（每个 S-xx 修复都配了用例），质量高于平均水平；但**覆盖面偏向"已修过的 bug"，对未修过的同类缺陷（python body、method 转义、多行 body、空目录解码、多 content type）系统性失明**——C-1/C-2/H-1/H-2/M-14 全部落在"测试恰好没断言那一行"的位置。

### 5.5 `scripts/` 质量
- 只有一个 `gowork.sh`（49 行，mode 100755，已正确入库为可执行）。
- **优点**：`set -euo pipefail`（:12）；`BASH_SOURCE` 定位自身（:14）；路径全部加引号；生成的 `go.work` 带"自动生成、勿手工编辑、勿提交"抬头（:37-38）；结尾打印验证与回滚命令（:48-49）；`.gitignore` 已忽略 `go.work`/`go.work.sum`；注释解释了"为什么放在仓库外"（:9-11）——工程素养明显在线。
- **缺点**：M-26（go 版本取自单一 go.mod 的硬编码假设）、M-27（下游路径硬编码、`--off` 位置敏感、`rm -f` 误导性成功、无 `--help`/未知参数校验、无生成后自检、以及"两端都有 share-version.sh"的文档承诺与事实不符）、L 级：无 `shellcheck` 门禁、无 `--dry-run`、路径含空格时写进 `go.work` 的 `use` 块未加引号（:41-45）。

---

## 六、整体评价（4 句）

1. 这是一个**边界纪律和工程素养都明显高于平均水平**的共享库：零 IO、单一第三方依赖、可注入的时钟/UUID、golden + 缺陷编号（S-01…S-11）式的回归守卫测试，以及一份"每条都写清消费方要不要改代码"的高质量 CHANGELOG；`syncapi` 用 type alias + 线格式守卫测试把"两端字段漂移"变成编译期错误，是全库最漂亮的一次设计。
2. 但它当前**没有兑现自己的核心承诺**：客户端为了绕过 `varx` 的名字文法私建了一层别名重写 shim 而服务端没有（H-4），`openapi.exampleOf` 的 map 遍历让同一份文件在两端/两次导入得到不同结果（H-2），`syncapi.Payload` 的 omitempty 让"零值 vs 未设置"在线上不可区分（H-7）——三处都恰好是"两端行为不一致"这一立项动机要消灭的故障类型。
3. **安全与正确性的短板集中在 `codegen`**：curl 的 shell 注入在 v0.10.1 被认真修掉并写了专项测试，但同一类问题在 python（body 裸拼，C-1）、fetch/axios/python 的 method 字段（C-2）与 `quote()` 不转义换行（H-1）上依然存在，而 CHANGELOG 里"python 本来就安全"的断言恰恰是错的——说明修复是"按报告逐点打补丁"而非"按字段×语言的矩阵系统排查"。
4. 版本与文档的**执行一致性**是最容易也最该先补的一环：3 个已发布 tag 无 CHANGELOG（含一次导出类型重命名）、2 个有记录的版本无 tag、README 里两个互相矛盾且都落后的"当前版本"、README 完全没提 `syncapi`、服务端侧的版本门禁根本不存在（H-8/M-25/M-27）——对一个"下游靠读 CHANGELOG 决定要不要升级"的库来说，这些是流程性风险而非文档瑕疵。

---

## 七、建议的修复顺序（Top 10）

1. **C-1** python body 裸拼 → 走安全字面量 + golden 测试（安全，1 小时）
2. **C-2** fetch/axios/python 的 method 未转义 → 统一转义 + 注入专项测试（安全，1 小时）
3. **H-1** `quote()` 补 `\n`/`\r`/U+2028/U+2029（功能，日常可见，30 分钟）
4. **H-2** `exampleOf` 确定性选择 content type + 守卫测试（正确性，30 分钟）
5. **H-5** `Builtins` 零值不再 panic（稳定性，30 分钟）
6. **H-8 + M-25** 补 v0.3.0/v0.6.0/v0.7.0 的 CHANGELOG、补/删 v0.1.0-v0.2.0 tag、修 README 版本号并补 `syncapi` 行、加"tag ↔ CHANGELOG 一致"门禁（流程，半天）
7. **H-3 + M-24** `varx` 记忆化 + 输出上限，导出 `MaxImportBytes`/`MaxNestDepth` 常量并加 Fuzz（健壮性，1 天）
8. **H-4** 放宽 `varx` 名字文法（或提供注入点），随后删除客户端 shim，让两端重新走同一条路径（一致性，1 天，需两端同步升级）
9. **H-7** `syncapi.Payload` 契约文档化（短期）→ 拆分实体 payload / 三态指针（中期，破坏性，随 `Proto` 升级一起做）
10. **M-3 + M-10/M-11/M-15/M-16** 给 `openapi.Decode`/`postman.Decode`/`collection.Decode*` 加 warnings 通道 + 格式版本闸门，把"静默少内容"变成"可见的告警"（体验，2-3 天）

（附：**H-6** `mock.Path` 的 `host:port` 与 **M-18** 的路径模板匹配建议合并成一次 `mock` 包的能力升级；**M-2/M-4/M-5** 属于 `collection` 命名与编解码的一次集中加固，可打包为一个 minor 版本。）
