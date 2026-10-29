package mcp

import (
	"errors"
	"fmt"
)

// 本文件是 36 个 MCP Tool 的**单一事实源**：名称、风险级别、副作用标记、只读模式注册位、
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
	// Output 可选 outputSchema（仅列表类工具声明；详情类不声明，避免响应 payload 翻倍）。
	Output     any
	Deprecated bool   // 是否废弃（预留，当前全 false）
	AliasOf    string // 非空 = 别名项，指向正式 Tool（当前无任何别名）
}

// NeedsConsent 非低风险即需交互确认（T2 默认对 medium/high 弹窗）。
func (m ToolMeta) NeedsConsent() bool { return m.Risk != RiskLow }

// Mutable 是否落盘修改集合（T2 可据此区分「纯外发 medium」与「落盘写 high」的文案）。
func (m ToolMeta) Mutable() bool { return m.Write }

// toolMetas 全部正式工具，按代码注册顺序排列；正式工具恒为 36 个（ValidateMetas 锁定）。
var toolMetas = []ToolMeta{
	// ---- 12 个只读工具（只读模式也注册）----
	{
		Name: "list_workspaces", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "列出 MCP 可访问的工作目录（客户端授权的白名单）与其中的项目、当前默认目标。白名单为空时 AI 无法读写任何集合 —— 需要时请让用户在客户端「设置 → MCP 服务 → 可访问的工作目录」里添加。",
	},
	{
		Name: "use_workspace", Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Description: "选定默认工作目录：选定后其余工具的 project 参数即可省略。workspace 传 list_workspaces 返回的工作目录 path，或一个项目标识（名称/路径/uid）；不在白名单内会直接拒绝。",
	},
	{
		Name: "list_projects", Risk: RiskLow, InReadOnly: true, Since: "0.1.0", Output: listProjectsOutput(),
		Description: "列出所有项目（集合）。project = 包含 opencollection.yml 的目录，一个项目一个集合。范围仅限客户端授权的 MCP 工作目录（见 list_workspaces）。返回名称、路径、uid、请求数/模块数/环境列表。",
	},
	{
		Name: "get_project_modules", Risk: RiskLow, InReadOnly: true, Since: "0.1.0", Output: modulesOutput(),
		Description: "获取某个项目的目录（模块）结构与每个模块下的请求数。query 可对模块名与请求名/方法/路径做模糊过滤；include_requests=true 时附带请求清单。带 query 时默认只返回有命中的模块（模块名命中，或其下有命中请求），汇总行始终给出模块总数；传 onlyMatched=false 可看全部模块。",
	},
	{
		Name: "get_request_detail", Risk: RiskLow, InReadOnly: true, Since: "0.1.0", Output: requestDetailFullOutput(),
		Description: "接口详情：请求/headers/概述/说明/响应字段/示例。sections 可只取分段（overview/docs/bodyFields/respDoc/examples）；说明长时用 docsOutline 或 docsStartLine+docsMaxLines；hash 完整可直接回传 ifMatch。",
	},
	{
		Name: "get_request_details", Risk: RiskLow, InReadOnly: true, Since: "0.4.0", Output: requestDetailsOutput(),
		Description: "批量获取多个接口的详情（一次 1–10 个 uid，超出直接报错）：参数与 get_request_detail 相同（sections/include_body/maxBodyBytes/query 对每个卡片生效），结果按 uid 分组，单个 uid 失败不影响其它。用于一次取多张卡片，省掉逐个调用的往返。",
	},
	{
		Name: "search_requests", Risk: RiskLow, InReadOnly: true, Since: "0.1.0", Output: searchRequestsOutput(),
		Description: "搜索接口（名称/URL/方法/header/说明/请求体/响应字段/字段表打分）：exactMatch 整词、fields 限定、includeHash 附 hash；snippet 给命中行。模糊匹配可能返回弱相关结果（results[].weakMatch=true），删除前核对 name/path。",
	},
	{
		Name: "search_docs", Risk: RiskLow, InReadOnly: true, Since: "0.4.0", Output: searchDocsOutput(),
		Description: "搜索文档条目（集合树里独立的 .md）正文：按行匹配，返回每篇文档的 uid/路径/命中行号/片段（每篇最多 5 处，其余用 get_doc 读全文）。pattern 必填；exactMatch=true 走整词匹配；project 省略时在所有已加载项目里搜。用于定位文档内容，避免逐篇 get_doc 全文拉回；命中行号（L<n>）可作为 get_doc 的 startLine 继续精读。",
	},
	{
		Name: "list_envs", Risk: RiskLow, InReadOnly: true, Since: "0.1.0", Output: envsOutput(),
		Description: "列出环境与变量（敏感值只回掩码）。query 过滤环境/变量名；limit（默认 200）/offset 分页，首行给总数。环境即 environments/<env>.yml，请求里用 {{变量名}} 引用这些变量。每个环境带一个 hash，写操作时作为 if_match 传回即可避免覆盖期间的其它改动。",
	},
	{
		Name: "get_doc_template", Risk: RiskLow, InReadOnly: true, Since: "0.2.0",
		Description: "返回「接口说明（docs）」的分节结构契约：与客户端一键「生成接口文档」的 Markdown 骨架逐节对齐（简要描述/请求URI/请求方式/请求头/请求参数表/请求示例/响应字段表/响应示例/备注，含列名与 {{host}} 掩码规则）。写 docs 前先取本模板，保证客户端展示与导出渲染一致。无需项目上下文。",
	},
	{
		Name: "list_docs", Risk: RiskLow, InReadOnly: true, Since: "0.2.0", Output: listDocsOutput(),
		Description: "列出项目里的文档条目（集合树里独立的 .md）。query 按名称/路径过滤；limit（默认 200，最大 1000）/offset 分页，首行给总数；默认带 hash（可直接回传 update_doc.ifMatch，大项目可 includeHash=false 省读盘）。读正文用 get_doc。注意：只含文档条目（.md）；接口请求请用 get_project_modules，接口自带的说明是 request.docs，也不在这里。",
	},
	{
		Name: "get_docs", Risk: RiskLow, InReadOnly: true, Since: "0.6.0", Output: docsBatchOutput(),
		Description: "批量读文档：一次 1–10 个 uid，按 uid 分组；单条失败不影响其他。支持与 get_doc 相同的 startLine/maxLines/outline。",
	},
	{
		Name: "get_doc", Risk: RiskLow, InReadOnly: true, Since: "0.2.0", Output: docDetailOutput(true),
		Description: "读文档正文+完整 hash（可回传 update_doc.ifMatch 防覆盖）。默认全文；长文先 outline=true 看大纲，再 startLine+maxLines 分段（行号仅供阅读）。",
	},

	// ---- 17 个集合写工具（只读模式不注册）：请求 5 + 文档 9（含图片与容器）+ 响应示例 3 ----
	{
		Name: "create_request", Risk: RiskHigh, Write: true, Since: "0.1.0", Output: requestDetailOutput(),
		Description: "新建接口：给 method/url/…，或 curl，或 copyFromUid；可同时写 summary/bodyFields/docs/respDoc/settings；同名或同 uid 默认 [CONFLICT]（onConflict=suffix/overwrite 可改），uid 可固定身份。",
	},
	{
		Name: "create_module", Risk: RiskHigh, Write: true, Since: "0.1.0", Output: moduleWriteOutput(),
		Description: "创建模块（目录）。parent 为空表示在项目根下建；支持嵌套（如 parent=api 建 api/user）。",
	},
	{
		Name: "create_project", Risk: RiskHigh, Write: true, Since: "0.1.0", Output: projectWriteOutput(),
		Description: "在**授权且可写**的工作目录下新建项目。root 必须是工作目录（不能是项目目录本身或项目内子目录，否则 [INVALID] 并提示改传父级）；留空用第一个可写工作目录。白名单外 [FORBIDDEN]、只读 [READONLY]、重名 [CONFLICT]。",
	},
	{
		Name: "update_request", Risk: RiskHigh, Write: true, Since: "0.1.0", Output: requestDetailOutput(),
		Description: "改接口（只改传入字段）：name/method/url/params/headers/body/auth/docs/settings/respDoc/summary/bodyFields 各自覆盖，不传不动；body 与 bodyFields 独立；docs 与 replaces 互斥（old 缺失整单拒绝）；ifMatch=完整 hash 防覆盖；folder 移动模块；成功附改动行确认。",
	},
	{
		Name: "delete_request", Risk: RiskHigh, Write: true, Since: "0.1.0", Output: requestDeleteOutput(),
		Description: "移除接口请求（移入 .trash 可找回）；ifMatch/confirmName 可二次防护，批量用 delete_requests。",
	},
	{
		Name: "delete_requests", Risk: RiskHigh, Write: true, Since: "0.6.0", Output: deleteRequestsOutput(),
		Description: "批量删除接口（1–10 条）：每条必须给 uid + ifMatch（search_requests 的 results[].hash），防误删；dryRun=true 只校验。单条请用 delete_request。",
	},
	{
		Name: "create_doc", Risk: RiskHigh, Write: true, Since: "0.2.0", Output: docDetailOutput(false),
		Description: "新建文档（独立 .md；folder 留空=docs/）。身份=uid/path（name 可重复）；同名/同 uid 默认 [CONFLICT]，onConflict=suffix/overwrite 放宽；uid 可幂等；dryRun 只演练。",
	},
	{
		Name: "update_doc", Risk: RiskHigh, Write: true, Since: "0.2.0", Output: docDetailOutput(false),
		Description: "改文档：content 覆盖（空串清空）/ name 显示名 / folder 移动 / replaces 局部替换（与 content 互斥，old 缺失整单拒绝）；ifMatch 防覆盖；成功附改动行；dryRun 只预览。",
	},
	{
		Name: "delete_doc", Risk: RiskHigh, Write: true, Since: "0.2.0", Output: docDeleteOutput(),
		Description: "移除文档（移入集合 .trash，可人工找回）；dryRun=true 只返回目标路径、不删。",
	},
	{
		Name: "save_doc_image", Risk: RiskHigh, Write: true, Since: "0.5.0", Output: imageOutput(),
		Description: "保存图片到项目 assets/（base64；PNG/JPEG/GIF/WebP≤5MB，同图去重），返回 /doc-assets/<sha256>，可直接写进 Markdown。",
	},
	{
		Name: "rename_module", Risk: RiskHigh, Write: true, Since: "0.6.0", Output: moduleWriteOutput(),
		Description: "改模块显示名（只改 folder.yml；path 是目录名，改名后路径不变）。path 用 get_project_modules 返回的值，如 api/user。",
	},
	{
		Name: "delete_module", Risk: RiskHigh, Write: true, Since: "0.6.0", Output: moduleDeleteOutput(),
		Description: "删除模块：recursive=true 连子模块/请求/文档整棵移入集合 .trash（可找回）；false（默认）只允许空模块，非空会拒绝。",
	},
	{
		Name: "rename_project", Risk: RiskHigh, Write: true, Since: "0.6.0", Output: projectWriteOutput(),
		Description: "改项目显示名（opencollection.yml 的 info.name；目录不变）。project 必须显式指定。",
	},
	{
		Name: "delete_project", Risk: RiskHigh, Write: true, Since: "0.6.0", Output: projectDeleteOutput(),
		Description: "删除项目：confirm 必须等于项目显示名或目录名（防误删）；目录移入授权根下 .trash/<名>-<时间戳>，可人工移回，list_projects 立即刷新。",
	},
	{
		Name: "prune_empty_modules", Risk: RiskHigh, Write: true, Since: "0.6.0", Output: pruneModulesOutput(),
		Description: "清理空模块目录（只有 folder.yml、没有请求/文档/子模块），返回删除数量；集合根与 assets/environments/examples 不动。",
	},
	{
		Name: "save_response_example", Risk: RiskHigh, Write: true, Since: "0.3.0", Output: exampleWriteOutput(),
		Description: "手工存一条响应示例（不发请求）：name/status/headers/body/contentType；重名自动加 (1)；返回示例 uid/path。",
	},
	{
		Name: "update_response_example", Risk: RiskHigh, Write: true, Since: "0.3.0", Output: exampleWriteOutput(),
		Description: "改响应示例：name/status/headers/body/contentType 局部更新（不传不动）；改名同步文件名。",
	},
	{
		Name: "delete_response_example", Risk: RiskHigh, Write: true, Since: "0.3.0", Output: exampleDeleteOutput(),
		Description: "删除一条已保存的响应示例。文件会移入集合的 .trash 目录（可人工找回），不会真正删除；只删示例，不动请求本身。",
	},

	// ---- 5 个环境/变量写工具（只读模式不注册）----
	{
		Name: "create_env", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0", Output: envWriteOutput(),
		Description: "新建环境（environments/<name>.yml），可同时写入初始变量。重名会报错，不覆盖已有环境。环境名只允许字母、数字、- 与 _。",
	},
	{
		Name: "rename_env", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0", Output: envWriteOutput(),
		Description: "给环境改名：按新名写一份（敏感值一起搬过去），再把旧名移入 .trash。目标名已被占用会报错；只改大小写（如 dev→DEV）也会被拒绝——在 Windows 上那是同一个文件，改名会让环境消失。ifMatch 传 list_envs 里的 hash 可检测期间的外部改动并拒绝覆盖。",
	},
	{
		Name: "delete_env", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0", Output: envDeleteOutput(),
		Description: "删除环境（两个文件都移入集合的 .trash，可人工找回）。删之前建议先 list_envs 确认里面没有还要用的变量。",
	},
	{
		Name: "set_env_var", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0", Output: envVarWriteOutput(),
		Description: "写单个环境变量（upsert）：name/value/secret/enabled；敏感值传掩码=不改；ifMatch 用 list_envs 的 hash 防覆盖。",
	},
	{
		Name: "delete_env_var", Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0", Output: envVarDeleteOutput(),
		Description: "从某环境里删掉一个变量。变量不存在会报错（避免「以为删掉了」）。ifMatch 传 list_envs 里该环境的 hash 可获得并发保护。",
	},

	// ---- 唯一的网络外发工具（只读模式也注册，但强制不落盘）----
	{
		Name: "send_request", Risk: RiskMedium, Output: sendRequestOutput(), NetEgress: true, InReadOnly: true, Since: "0.1.0",
		Description: "发送请求并返回状态码/耗时/大小/响应体（响应体按 maxBodyBytes 裁剪）。saveExample 默认 false（探测请求不落盘）；需要留存时显式 saveExample=true，响应存成该请求的响应示例（examples/*.yml）。可以给 uid 发已保存的请求，也可以给 method+url 发临时请求（临时请求不落盘、不存示例；临时请求也需要 project，或先 use_workspace）。",
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
// readOnly=true 时仅 InReadOnly 的工具（12 读 + send_request = 13 个；全模式 29 个）。
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
	const expectTools = 37
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

// ---- outputSchema 构造器（结构化输出的"编译时契约"）----
//
// 评审第二轮指出：只写 items:{type:"object"} 等于没有字段契约——字段改名不会触发校验。
// 这里把每个列表/详情工具的真实字段都写进 schema（required 标出必读字段）。

func objSchema(props map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func arrSchema(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func strSchema() map[string]any  { return map[string]any{"type": "string"} }
func intSchema() map[string]any  { return map[string]any{"type": "integer"} }
func boolSchema() map[string]any { return map[string]any{"type": "boolean"} }

func listProjectsOutput() map[string]any {
	project := objSchema(map[string]any{
		"path":       strSchema(),
		"name":       strSchema(),
		"uid":        strSchema(),
		"requests":   intSchema(),
		"folders":    intSchema(),
		"envs":       arrSchema(strSchema()),
		"defaultEnv": strSchema(),
		"skipped":    strSchema(),
	}, "path", "name")
	return objSchema(map[string]any{"projects": arrSchema(project), "skipped": intSchema()}, "projects")
}

func listDocsOutput() map[string]any {
	doc := objSchema(map[string]any{
		"uid":  strSchema(),
		"name": strSchema(),
		"path": strSchema(),
		"size": intSchema(),
		"hash": strSchema(),
		"icon": strSchema(),
		"seq":  intSchema(),
	}, "uid", "path")
	return objSchema(map[string]any{
		"project": strSchema(), "total": intSchema(), "returned": intSchema(),
		"offset": intSchema(), "docs": arrSchema(doc),
	}, "docs")
}

func searchDocsOutput() map[string]any {
	hit := objSchema(map[string]any{
		"uid": strSchema(), "name": strSchema(), "path": strSchema(), "project": strSchema(),
		"count":   intSchema(),
		"matches": arrSchema(objSchema(map[string]any{"line": intSchema(), "snippet": strSchema()}, "line", "snippet")),
	}, "uid", "path")
	return objSchema(map[string]any{
		"pattern": strSchema(), "total": intSchema(), "returned": intSchema(), "docs": arrSchema(hit),
	}, "docs")
}

func searchRequestsOutput() map[string]any {
	item := objSchema(map[string]any{
		"uid": strSchema(), "name": strSchema(), "method": strSchema(), "url": strSchema(),
		"path": strSchema(), "project": strSchema(), "score": intSchema(),
		"matched": arrSchema(strSchema()), "snippet": strSchema(), "hash": strSchema(), "weakMatch": boolSchema(),
	}, "uid", "path")
	return objSchema(map[string]any{
		"query": strSchema(), "total": intSchema(), "returned": intSchema(), "results": arrSchema(item),
	}, "results")
}

func modulesOutput() map[string]any {
	mod := objSchema(map[string]any{
		"path": strSchema(), "name": strSchema(), "requests": intSchema(), "dirs": intSchema(),
	}, "path", "name")
	return objSchema(map[string]any{
		"project": strSchema(), "moduleTotal": intSchema(), "matched": intSchema(),
		"total": intSchema(), "modules": arrSchema(mod),
	}, "modules")
}

func envsOutput() map[string]any {
	v := objSchema(map[string]any{
		"name": strSchema(), "value": strSchema(), "secret": boolSchema(),
		"enabled": boolSchema(), "keptSecret": boolSchema(), "hash": strSchema(),
	}, "name")
	env := objSchema(map[string]any{
		"name": strSchema(), "default": boolSchema(), "hash": strSchema(),
		"vars": map[string]any{"type": "object", "additionalProperties": v},
	}, "name", "vars")
	return objSchema(map[string]any{
		"project": strSchema(), "total": intSchema(), "returned": intSchema(),
		"offset": intSchema(), "envs": arrSchema(env),
	}, "envs")
}

// docDetailOutput 文档单条/写操作的结果（create_doc / update_doc / get_doc）。
func docDetailOutput(withContent bool) map[string]any {
	props := map[string]any{
		"project": strSchema(), "uid": strSchema(), "name": strSchema(),
		"path": strSchema(), "size": intSchema(), "hash": strSchema(),
		"planned": strSchema(), // dryRun 时 create/overwrite/update
	}
	required := []string{"uid", "path"}
	if withContent {
		props["content"] = strSchema()
	}
	return objSchema(props, required...)
}

// requestDetailOutput create_request / update_request 的结果。
func requestDetailOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "uid": strSchema(), "name": strSchema(),
		"path": strSchema(), "method": strSchema(), "url": strSchema(),
	}, "uid", "path")
}

// deleteRequestsOutput delete_requests（批量）的结果。
func deleteRequestsOutput() map[string]any {
	f := objSchema(map[string]any{"uid": strSchema(), "error": strSchema()}, "uid")
	return objSchema(map[string]any{
		"project": strSchema(), "requested": intSchema(), "dryRun": boolSchema(),
		"deleted": arrSchema(strSchema()), "failed": arrSchema(f),
	}, "requested")
}

// requestDeleteOutput delete_request 的结果（删除前取到的身份信息）。
func requestDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"uid": strSchema(), "path": strSchema(), "name": strSchema(),
		"method": strSchema(), "url": strSchema(),
		"deleted": boolSchema(), "trash": boolSchema(),
	}, "uid")
}

// moduleWriteOutput create_module / rename_module 的结果。
func moduleWriteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "path": strSchema(), "name": strSchema(),
	}, "path")
}

// moduleDeleteOutput delete_module 的结果。
func moduleDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "path": strSchema(), "recursive": boolSchema(),
		"deleted": boolSchema(), "trash": boolSchema(), "dryRun": boolSchema(),
	}, "path")
}

// pruneModulesOutput prune_empty_modules 的结果。
func pruneModulesOutput() map[string]any {
	return objSchema(map[string]any{"project": strSchema(), "count": intSchema()}, "count")
}

// projectWriteOutput create_project / rename_project 的结果。
func projectWriteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "name": strSchema(), "path": strSchema(), "uid": strSchema(),
	}, "name", "path")
}

// projectDeleteOutput delete_project 的结果。
func projectDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "trashPath": strSchema(), "deleted": boolSchema(), "dryRun": boolSchema(),
	}, "project", "deleted")
}

// envWriteOutput create_env / rename_env 的结果。
func envWriteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "oldName": strSchema(), "name": strSchema(),
		"hash": strSchema(), "varsCount": intSchema(),
	}, "name")
}

// envDeleteOutput delete_env 的结果。
func envDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "name": strSchema(), "deleted": boolSchema(),
		"trash": boolSchema(), "dryRun": boolSchema(),
	}, "name")
}

// envVarWriteOutput set_env_var 的结果。
func envVarWriteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "env": strSchema(), "name": strSchema(), "value": strSchema(),
		"secret": boolSchema(), "enabled": boolSchema(), "keptSecret": boolSchema(), "hash": strSchema(),
	}, "env", "name")
}

// envVarDeleteOutput delete_env_var 的结果。
func envVarDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "env": strSchema(), "name": strSchema(),
		"deleted": boolSchema(), "dryRun": boolSchema(),
	}, "env", "name")
}

// exampleWriteOutput save/update_response_example 的结果。
func exampleWriteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "requestUid": strSchema(), "uid": strSchema(),
		"name": strSchema(), "path": strSchema(), "status": intSchema(),
	}, "uid")
}

// exampleDeleteOutput delete_response_example 的结果。
func exampleDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "requestUid": strSchema(), "uid": strSchema(),
		"deleted": boolSchema(), "trash": boolSchema(), "dryRun": boolSchema(),
	}, "uid")
}

// imageOutput save_doc_image 的结果。
func imageOutput() map[string]any {
	return objSchema(map[string]any{
		"url": strSchema(), "sha256": strSchema(), "size": intSchema(),
		"deduped": boolSchema(), "markdown": strSchema(), "name": strSchema(),
	}, "url", "sha256")
}

// requestDetailOutput 的扩展版：详情（含 hash/示例/相关文档摘要）。
func requestDetailFullOutput() map[string]any {
	ex := objSchema(map[string]any{
		"uid": strSchema(), "name": strSchema(), "path": strSchema(),
		"hash": strSchema(), "status": intSchema(),
	}, "uid", "path")
	rel := objSchema(map[string]any{"uid": strSchema(), "name": strSchema(), "path": strSchema()}, "uid", "path")
	return objSchema(map[string]any{
		"project": strSchema(), "uid": strSchema(), "name": strSchema(), "method": strSchema(),
		"url": strSchema(), "path": strSchema(), "hash": strSchema(), "summary": strSchema(),
		"bodyFields": intSchema(), "params": intSchema(), "headers": intSchema(),
		"examples": arrSchema(ex), "relatedDocs": arrSchema(rel),
	}, "uid", "path")
}

// requestDetailsOutput get_request_details 的批量结果。
func requestDetailsOutput() map[string]any {
	item := objSchema(map[string]any{
		"project": strSchema(), "uid": strSchema(), "name": strSchema(), "method": strSchema(),
		"url": strSchema(), "path": strSchema(), "hash": strSchema(), "error": strSchema(),
	}, "uid")
	return objSchema(map[string]any{
		"project": strSchema(), "total": intSchema(), "returned": intSchema(),
		"items": arrSchema(item),
	}, "items")
}

// sendRequestOutput send_request 的结果。
func sendRequestOutput() map[string]any {
	return objSchema(map[string]any{
		"status": intSchema(), "url": strSchema(), "timeMs": intSchema(),
		"size": intSchema(), "contentType": strSchema(), "body": strSchema(),
		"truncated": boolSchema(), "originalBytes": intSchema(), "headerCount": intSchema(),
		"redirects": intSchema(), "remoteAddr": strSchema(),
		"saved": boolSchema(), "exampleUid": strSchema(), "examplePath": strSchema(),
	}, "status")
}

// docsBatchOutput get_docs 的结果（单篇失败不影响其他：error 只在失败条目出现）。
func docsBatchOutput() map[string]any {
	item := objSchema(map[string]any{
		"uid": strSchema(), "name": strSchema(), "path": strSchema(),
		"size": intSchema(), "hash": strSchema(), "content": strSchema(), "error": strSchema(),
	}, "uid", "path")
	return objSchema(map[string]any{
		"project": strSchema(), "total": intSchema(), "returned": intSchema(),
		"docs": arrSchema(item),
	}, "docs")
}

// docDeleteOutput delete_doc 的结果（只带身份与路径，避免暴露内容）。
func docDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"uid": strSchema(), "path": strSchema(), "deleted": boolSchema(), "trash": boolSchema(), "dryRun": boolSchema(),
	}, "uid")
}
