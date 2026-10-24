package mcp

import (
	"errors"
	"fmt"
)

// 本文件是 27 个 MCP Tool 的**单一事实源**：名称、风险级别、副作用标记、只读模式注册位、
// 注册描述。tools.go 的注册、T2 的交互授权、T3 的审计、文档生成都读这张表，
// 不允许在别处再硬编码一份工具风险/读写分类。
//
// 旧 9-Tool 别名决策（详见开发设计 01-T1 §7.3）：**默认不实现别名层**。
// 旧名从未在任何发布版本注册过，tools/list 出现双名同义只会放大 AI 选错工具的概率。
// 若将来确有兼容需求：在本表追加 AliasOf 指向正式名的项，并注册一个把入参转发给正式
// handler 的薄适配层（审计同时记录别名与正式名）。ValidateMetas 会拒绝任何 AliasOf
// 非空项被当作正式表随产品发布。

// RiskLevel 工具风险级别。授权策略（T2）按级别 + Write/NetEgress 正交标志决定是否弹窗。
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"    // 只读 / 仅会话状态
	RiskMedium RiskLevel = "medium" // 真实网络外发（send_request）
	RiskHigh   RiskLevel = "high"   // 文件 / 集合 / 环境落盘写
)

// ToolMeta 单个工具的契约元数据。
type ToolMeta struct {
	Name string // MCP Tool 名（snake_case，对外契约，禁改）

	Risk RiskLevel // 风险级别

	Write     bool // 是否有文件落盘副作用
	NetEgress bool // 是否产生真实网络外发
	EnvWrite  bool // 是否为环境/变量写（会触碰 environments/*.secrets.yml）

	InReadOnly bool // 只读模式下是否仍注册

	Description string // 与注册到 mcp.Tool.Description 的文案一致（注册直接读这里）
	Since       string // 引入版本
	Deprecated  bool   // 是否废弃（预留，当前全 false）
	AliasOf     string // 非空 = 别名项，指向正式 Tool（当前无任何别名）
}

// NeedsConsent 非低风险即需交互确认（T2 默认对 medium/high 弹窗）。
func (m ToolMeta) NeedsConsent() bool { return m.Risk != RiskLow }

// Mutable 是否落盘修改集合（T2 可据此区分「纯外发 medium」与「落盘写 high」的文案）。
func (m ToolMeta) Mutable() bool { return m.Write }

// toolMetas 全部正式工具，按代码注册顺序排列；正式工具恒为 27 个（ValidateMetas 锁定）。
var toolMetas = []ToolMeta{
	// ---- 10 个只读工具（只读模式也注册）----
	{
		Name: "list_workspaces", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "列出 MCP 可访问的工作目录（客户端授权的白名单）与其中的项目、当前默认目标。白名单为空时 AI 无法读写任何集合 —— 需要时请让用户在客户端「设置 → MCP 服务 → 可访问的工作目录」里添加。",
	},
	{
		Name: "use_workspace", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "选定默认工作目录：选定后其余工具的 project 参数即可省略。workspace 传 list_workspaces 返回的工作目录 path，或一个项目标识（名称/路径/uid）；不在白名单内会直接拒绝。",
	},
	{
		Name: "list_projects", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "列出所有项目（集合）。project = 包含 opencollection.yml 的目录，一个项目一个集合。范围仅限客户端授权的 MCP 工作目录（见 list_workspaces）。返回名称、路径、uid、请求数/模块数/环境列表。",
	},
	{
		Name: "get_project_modules", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "获取某个项目的目录（模块）结构与每个模块下的请求数。query 可对模块名与请求名/方法/路径做模糊过滤；include_requests=true 时附带请求清单。",
	},
	{
		Name: "get_request_detail", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "获取某个接口的详情：请求（method/url/params/body/auth/settings/grpc）、headers、概述（summary）、说明文档（docs）、响应字段文档（respDoc：返回类型/字段表/样例，样例按 include_body 裁剪）以及已保存的响应示例。请求体字段表格在 body.fields（字段名/类型/必填/示例值/说明，手工生成，可为空）。query 可在示例名/文档名里再筛一遍；include_body=true 时返回示例的响应体（按 maxBodyBytes 裁剪）。返回的 hash 可传给 update_request.if_match 做冲突保护。",
	},
	{
		Name: "search_requests", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "模糊搜索接口：按名称/URL/方法/路径/header/说明/body 打分排序（分数越高越相关，matched 列出命中字段）。project 省略时在所有已加载项目里搜。适合「记得大概内容但记不清名字」的场景。",
	},
	{
		Name: "list_envs", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "列出项目的全部环境与其中的变量（敏感变量的值只回掩码 ••••••，拿不到真实密钥）。query 可对环境名与变量名做模糊过滤。环境即 environments/<env>.yml，请求里用 {{变量名}} 引用这些变量。每个环境带一个 hash，写操作时作为 if_match 传回即可避免覆盖期间的其它改动。",
	},
	{
		Name: "get_doc_template", Risk: RiskLow, InReadOnly: true, Since: "0.2.0",
		Description: "返回「接口说明（docs）」的分节结构契约：与客户端一键「生成接口文档」的 Markdown 骨架逐节对齐（简要描述/请求URI/请求方式/请求头/请求参数表/请求示例/响应字段表/响应示例/备注，含列名与 {{host}} 掩码规则）。写 docs 前先取本模板，保证客户端展示与导出渲染一致。无需项目上下文。",
	},
	{
		Name: "list_docs", Risk: RiskLow, InReadOnly: true, Since: "0.2.0",
		Description: "列出项目里的文档条目（集合树里独立的 .md，可挂在任意分组目录下）。注意与接口自带的说明区分：接口说明是 request.docs（见 get_request_detail），不在这里。query 可按文档名/路径模糊过滤。返回每篇文档的 uid/名称/路径/字节数；读正文用 get_doc。",
	},
	{
		Name: "get_doc", Risk: RiskLow, InReadOnly: true, Since: "0.2.0",
		Description: "读取一篇文档的正文（Markdown 原文）与元信息，并返回该文件的 hash：把 hash 作为 update_doc.ifMatch 传回即可避免覆盖期间的外部改动。uid 来自 list_docs。",
	},

	// ---- 11 个集合写工具（只读模式不注册）：请求 5 + 文档 3 + 响应示例 3 ----
	{
		Name: "create_request", Risk: RiskHigh, Write: true, Since: "0.1.0",
		Description: "创建接口请求。来源三选一：直接给 method/url/headers/params/body（完整形态，支持 form/multipart 行）；给 curl（自动解析）；给 copyFromUid（复制已有请求）。还能同时写入 settings（单请求超时/重定向等，不碰全局）、respDoc（响应字段：可只写 params 字段表，样例/响应头缺省不动；gRPC 拒绝）与 docs（结构先取 get_doc_template）。概述用 summary（纯文本一句话），请求体字段表格用 bodyFields（与 body 相互独立，传了只覆盖表格）；三者互不影响。同名会在分组内自动加序号。",
	},
	{
		Name: "create_module", Risk: RiskHigh, Write: true, Since: "0.1.0",
		Description: "创建模块（目录）。parent 为空表示在项目根下建；支持嵌套（如 parent=api 建 api/user）。",
	},
	{
		Name: "create_project", Risk: RiskHigh, Write: true, Since: "0.1.0",
		Description: "在**授权且可写**的工作目录下新建一个项目（集合目录 + manifest），建完即可用 list_projects 看到。dir 必须是白名单内的可写目录，留空时用第一个可写目录；白名单外一律拒绝。",
	},
	{
		Name: "update_request", Risk: RiskHigh, Write: true, Since: "0.1.0",
		Description: "修改已有接口（只改传入的字段）：name/method/url/params/headers/body/auth/docs/settings/respDoc 都是整字段覆盖语义（不传不动；params 传空数组即清空）。respDoc 按子项合并：可只写 params 字段表，样例/响应头/状态码缺省保留原值（gRPC 拒绝）；settings 仅单请求级，不碰全局配置；docs 结构先取 get_doc_template。概述用 summary、请求体字段表格用 bodyFields，各自独立设置：body 整份覆盖不会清掉表格与概述（body.fields/body.descs 传 null 保留原值，显式传空数组/空对象才清空），bodyFields 不传=不动、传空数组=清空；summary 不传=不动、传空串=清空。folder 可移动模块；ifMatch 传详情里拿到的 hash 可检测外部改动并拒绝覆盖（不传则最后写者赢）。改名会自动同步文件名。",
	},
	{
		Name: "delete_request", Risk: RiskHigh, Write: true, Since: "0.1.0",
		Description: "移除接口请求。文件会移入集合的 .trash 目录（可人工找回），不会真正删除。",
	},
	{
		Name: "create_doc", Risk: RiskHigh, Write: true, Since: "0.2.0",
		Description: "新建文档条目（集合树里独立的 .md，可挂在任意分组目录下：folder 留空=集合根）。content 可一次给 Markdown 正文，留空则先建空文档。注意与接口说明区分：接口说明走 create_request/update_request 的 docs 字段。",
	},
	{
		Name: "update_doc", Risk: RiskHigh, Write: true, Since: "0.2.0",
		Description: "修改已有文档（只改传入的字段）：content 覆盖正文（传空串=清空）、name 改显示名（文件名不变）、folder 移动到别的分组目录（传空串=移到集合根）。ifMatch 传 get_doc 返回的 hash 可检测期间的外部改动并拒绝覆盖（不传则最后写者赢）。",
	},
	{
		Name: "delete_doc", Risk: RiskHigh, Write: true, Since: "0.2.0",
		Description: "移除文档条目。文件会移入集合的 .trash 目录（可人工找回），不会真正删除。",
	},
	{
		Name: "save_response_example", Risk: RiskHigh, Write: true, Since: "0.3.0",
		Description: "手工保存一条响应示例（不发送请求，适合把外部文档/日志里的响应片段留成示例）：落到该请求的 examples/ 目录（examples/<模块>/<请求名>/<示例名>.yml）。name 省略=「响应示例」，status 省略=200，contentType 省略时按 body 首字符推断（{ / [ → application/json，其余 text/plain）；size 按 body 字节数记录。重名自动加 (1)(2)。返回示例 uid/path/name —— 改它用 update_response_example，删它用 delete_response_example。",
	},
	{
		Name: "update_response_example", Risk: RiskHigh, Write: true, Since: "0.3.0",
		Description: "修改已保存的响应示例（只改传入的字段）：name 改名会同步把磁盘文件名改成「规范化名.yml」（与新建示例同一套命名规则，显示名与文件名同值；含非法字符/过长会报错且不动任何文件，写成功后才会删旧文件）；status/headers/body/contentType 不传则不动（传空数组可清空响应头，status 传 0 即清零），body 变化时 size 自动跟着改；重名自动加 (1)(2)。exampleUid 来自 get_request_detail 示例行里的 uid=…。",
	},
	{
		Name: "delete_response_example", Risk: RiskHigh, Write: true, Since: "0.3.0",
		Description: "删除一条已保存的响应示例。文件会移入集合的 .trash 目录（可人工找回），不会真正删除；只删示例，不动请求本身。",
	},

	// ---- 5 个环境/变量写工具（只读模式不注册）----
	{
		Name: "create_env", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Description: "新建环境（environments/<name>.yml），可同时写入初始变量。重名会报错，不覆盖已有环境。环境名只允许字母、数字、- 与 _。",
	},
	{
		Name: "rename_env", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Description: "给环境改名：按新名写一份（敏感值一起搬过去），再把旧名移入 .trash。目标名已被占用会报错；只改大小写（如 dev→DEV）也会被拒绝——在 Windows 上那是同一个文件，改名会让环境消失。ifMatch 传 list_envs 里的 hash 可检测期间的外部改动并拒绝覆盖。",
	},
	{
		Name: "delete_env", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Description: "删除环境（两个文件都移入集合的 .trash，可人工找回）。删之前建议先 list_envs 确认里面没有还要用的变量。",
	},
	{
		Name: "set_env_var", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Description: "新增或更新某环境里的一个变量（按变量名 upsert，只改这一条，其它变量与顺序都不动）。强烈建议先 list_envs 拿到该环境的 hash 再带 ifMatch 写入（环境是整份重写，不带校验会覆盖期间的其它改动）。secret=true 时值落盘进 .secrets.yml，之后对外只回掩码；若把掩码原样传回表示「密钥不变」，不会覆盖真值（掩码被改写成 •••• / ????? 这类占位也认）；对已存在的敏感变量传空值会被拒绝，避免误清空密钥。enabled=false 可停用某变量（不参与 {{name}} 解析）。",
	},
	{
		Name: "delete_env_var", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Description: "从某环境里删掉一个变量。变量不存在会报错（避免「以为删掉了」）。ifMatch 传 list_envs 里该环境的 hash 可获得并发保护。",
	},

	// ---- 唯一的网络外发工具（只读模式也注册，但强制不落盘）----
	{
		Name: "send_request", Risk: RiskMedium, NetEgress: true, InReadOnly: true, Since: "0.1.0",
		Description: "发送请求并返回状态码/耗时/大小/响应体（响应体按 maxBodyBytes 裁剪）。saveExample 默认 true：把响应存成该请求的响应示例（examples/*.yml，客户端可回看/删除）。可以给 uid 发已保存的请求，也可以给 method+url 发临时请求（临时请求不落盘、不存示例）。",
	},
}

// AllToolMetas 返回全部正式工具元数据的副本（按注册顺序）。
func AllToolMetas() []ToolMeta {
	out := make([]ToolMeta, len(toolMetas))
	copy(out, toolMetas)
	return out
}

// MetaOf 按名查元数据；不存在返回 (zero, false)。
func MetaOf(name string) (ToolMeta, bool) {
	for _, m := range toolMetas {
		if m.Name == name {
			return m, true
		}
	}
	return ToolMeta{}, false
}

// ToolNames 返回正式工具名清单（等价并取代测试里硬编码的名称数组）。
func ToolNames() []string {
	out := make([]string, len(toolMetas))
	for i, m := range toolMetas {
		out[i] = m.Name
	}
	return out
}

// NamesForMode 返回指定模式下应注册的工具名集合：
// readOnly=true 时仅 InReadOnly 的工具（10 读 + send_request = 11 个；全模式 27 个）。
func NamesForMode(readOnly bool) []string {
	out := make([]string, 0, len(toolMetas))
	for _, m := range toolMetas {
		if readOnly && !m.InReadOnly {
			continue
		}
		out = append(out, m.Name)
	}
	return out
}

// ValidateMetas 完整性自检：name 唯一非空、数量正确、风险级别合法、无别名正式项、
// 描述与版本齐备。供 RegisterTools 装配期与测试调用。
func ValidateMetas() error {
	const expectTools = 27
	if len(toolMetas) != expectTools {
		return fmt.Errorf("mcp meta: 期望 %d 个正式工具，实际 %d 个", expectTools, len(toolMetas))
	}
	seen := make(map[string]bool, len(toolMetas))
	for _, m := range toolMetas {
		if m.Name == "" {
			return errors.New("mcp meta: 存在空工具名")
		}
		if seen[m.Name] {
			return fmt.Errorf("mcp meta: 工具名重复 %q", m.Name)
		}
		if m.AliasOf != "" {
			return fmt.Errorf("mcp meta: 别名项不得作为正式表发布: %q -> %q", m.Name, m.AliasOf)
		}
		switch m.Risk {
		case RiskLow, RiskMedium, RiskHigh:
		default:
			return fmt.Errorf("mcp meta: 工具 %q 风险级别非法 %q", m.Name, m.Risk)
		}
		if m.Description == "" {
			return fmt.Errorf("mcp meta: 工具 %q 缺描述", m.Name)
		}
		if m.Since == "" {
			return fmt.Errorf("mcp meta: 工具 %q 缺 Since 版本", m.Name)
		}
		// 一致性：外发与落盘标记必须与风险级别自洽。
		if m.Risk == RiskLow && (m.Write || m.NetEgress) {
			return fmt.Errorf("mcp meta: low 风险工具不应有写/外发副作用: %q", m.Name)
		}
		if m.Risk == RiskHigh && !m.Write {
			return fmt.Errorf("mcp meta: high 风险工具必须标记 Write: %q", m.Name)
		}
		if m.NetEgress && m.Name != "send_request" {
			return fmt.Errorf("mcp meta: 仅 send_request 可标记 NetEgress: %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}
