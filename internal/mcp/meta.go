package mcp

import (
	"errors"
	"fmt"
	"strings"
)

// 本文件是 MCP Tool 的**单一事实源**：名称、工具组、风险级别、副作用标记、只读模式注册位、
// 注册描述与一句话摘要（Brief）。tools.go 的注册、T2 的交互授权、T3 的审计、文档生成都读这张表，
// 不允许在别处再硬编码一份工具风险/读写/分组。
//
// 数量不在这里写死（历史上写过 36/37/38/39，每次加工具都会漂移）：工具总数与风险分布
// 由 ToolNames() / meta_test.go 的基线锁定，注释只描述结构。
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

// 工具组：设置页可按组启停，减少 tools/list 的体积。
//
// session 组恒开（发现类工具：工具总览、工作目录、项目列表）—— 关掉它调用方就
// 连"有哪些工具/哪些项目"都问不到，会把用户锁死。
const (
	GroupSession    = "session"
	GroupCollection = "collection"
	GroupDocs       = "docs"
	GroupEnv        = "env"
	GroupExample    = "example"
	GroupNetwork    = "network"
)

// ConfigurableGroups 设置页可勾选的工具组（不含恒开的 session）；顺序即展示顺序。
var ConfigurableGroups = []string{GroupCollection, GroupDocs, GroupEnv, GroupExample, GroupNetwork}

// DisabledGroups 返回被显式关闭的可配置组；enabled 为空表示全部启用（返回 nil）。
// 只给 list_tools 的提示用：调用方需要知道「某类工具不是不存在，而是被设置关了」。
func DisabledGroups(enabled []string) []string {
	on := NormalizeGroups(enabled)
	if len(on) == 0 {
		return nil
	}
	set := make(map[string]bool, len(on))
	for _, g := range on {
		set[g] = true
	}
	out := make([]string, 0, len(ConfigurableGroups))
	for _, g := range ConfigurableGroups {
		if !set[g] {
			out = append(out, g)
		}
	}
	return out
}

// ToolMeta 单个工具的契约元数据。
type ToolMeta struct {
	Name string // MCP Tool 名（snake_case，对外契约）

	Group string // 工具组（见 Group* 常量）；session 组恒开

	Risk RiskLevel // 风险级别

	Write     bool // 是否有文件落盘副作用
	NetEgress bool // 是否产生真实网络外发
	EnvWrite  bool // 是否为环境/变量写（会触碰 environments/*.secrets.yml）

	InReadOnly bool // 只读模式下是否仍注册

	Description string // 完整描述（注册到 mcp.Tool.Description，供拉取 schema 的客户端阅读）
	// Brief 一句话摘要（≤40 字）：list_tools 只回它，避免"省上下文"的工具自己
	// 把全量长描述再塞回上下文。
	Brief string
	Since string // 引入版本
	// Output 可选 outputSchema（仅结构稳定的列表类工具声明；详情类不声明，避免响应 payload 翻倍）。
	Output     any
	Deprecated bool   // 是否废弃（预留，当前全 false）
	AliasOf    string // 非空 = 别名项，指向正式 Tool（当前无任何别名）
}

// NeedsConsent 非低风险即需交互确认（T2 默认对 medium/high 弹窗）。
func (m ToolMeta) NeedsConsent() bool { return m.Risk != RiskLow }

// Mutable 是否落盘修改集合（T2 可据此区分「纯外发 medium」与「落盘写 high」的文案）。
func (m ToolMeta) Mutable() bool { return m.Write }

// toolMetas 全部正式工具，按代码注册顺序排列。
var toolMetas = []ToolMeta{
	// ---- 恒开的会话/发现类（只读模式也注册）----
	{
		Name: "list_tools", Group: GroupSession, Risk: RiskLow, InReadOnly: true, Since: "0.7.0",
		Brief:       "工具总览：名称、用途、必填参数",
		Description: "一次返回全部工具名、一句话用途与必填参数（filter 过滤，mode=read|write 分类）；要完整入参再取该工具的 schema。",
		Output:      toolsCatalogOutput(),
	},
	{
		Name: "list_workspaces", Group: GroupSession, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief:       "列出可访问的工作目录与项目",
		Description: "列出 MCP 可访问的工作目录（白名单）、其中的项目与当前默认目标；白名单为空时需在客户端设置里添加。",
	},
	{
		Name: "use_workspace", Group: GroupSession, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief:       "选定本会话默认工作目录",
		Description: "选定后其余工具可省略 project；默认目标按会话保存，重连或服务重启后需重新选定。",
	},
	{
		Name: "list_projects", Group: GroupSession, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief:       "列出项目（集合）",
		Description: "列出授权目录下的项目（集合）及请求数、模块数与环境；refresh=true 重新扫描。",
		Output:      listProjectsOutput(),
	},

	// ---- 集合读（只读模式也注册）----
	{
		Name: "get_project_modules", Group: GroupCollection, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief:       "看模块结构与请求清单",
		Description: "返回项目的模块树与每个模块的请求数；query 过滤，includeRequests=true 附带请求清单。",
		Output:      modulesOutput(),
	},
	{
		Name: "get_request_detail", Group: GroupCollection, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief: "接口详情（单个或批量）",
		// 详情类不声明 outputSchema：同一工具两种形态（单条 / 批量 items）无法用一份稳定 schema 表达。
		Description: "取接口详情（请求、头、概述、说明、字段表、示例）：uid 取单个，uids 批量 1–10 个；sections 可只取分段，hash 可回传 ifMatch。",
	},
	{
		Name: "search_requests", Group: GroupCollection, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief:       "搜索接口",
		Description: "按名称/URL/方法/头/说明/请求体/响应字段搜索接口；exactMatch 整串匹配，fields 限定范围。",
		Output:      searchRequestsOutput(),
	},

	// ---- 文档读（只读模式也注册）----
	{
		Name: "search_docs", Group: GroupDocs, Risk: RiskLow, InReadOnly: true, Since: "0.4.0",
		Brief:       "搜索文档正文",
		Description: "在文档条目正文里按行搜索，返回 uid/路径/命中行号；定位后再用 get_doc 精读。",
		Output:      searchDocsOutput(),
	},
	{
		Name: "list_docs", Group: GroupDocs, Risk: RiskLow, InReadOnly: true, Since: "0.2.0",
		Brief:       "列出文档条目",
		Description: "列出文档条目（.md）及路径与 hash；query 过滤，limit/offset 分页。不含接口说明。",
		Output:      listDocsOutput(),
	},
	{
		Name: "get_doc", Group: GroupDocs, Risk: RiskLow, InReadOnly: true, Since: "0.2.0",
		Brief: "读文档正文（单个或批量）",
		// 同上：单篇 / 批量两种形态，不声明 outputSchema。
		Description: "读文档正文与完整 hash：uid 取单篇，uids 批量 1–10 篇；长文用 outline 或 startLine+maxLines。",
	},

	// ---- 环境读（只读模式也注册）----
	{
		Name: "list_envs", Group: GroupEnv, Risk: RiskLow, InReadOnly: true, Since: "0.1.0",
		Brief:       "列出环境与变量",
		Description: "列出环境与变量（敏感值只回掩码）；query 过滤，limit/offset 分页。",
		Output:      envsOutput(),
	},

	// ---- 集合写（只读模式不注册）：请求 6 + 容器 6 + 文档 4 + 示例 3 ----
	{
		Name: "create_request", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.1.0",
		Brief:       "新建接口请求",
		Description: "新建接口：手填字段、curl 或 copyFromUid；folder 指定模块；可同时写 summary/bodyFields/docs/respDoc/settings。",
		Output:      requestDetailOutput(),
	},
	{
		Name: "update_requests", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.8.0",
		Brief:       "批量修改接口",
		Description: "一次改 1–10 个接口（每条 uid + 要改的字段）；单条失败不影响其它，失败明细逐条回报；dryRun 只校验。",
		Output:      batchUpdateOutput(),
	},
	{
		Name: "import_collection", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.7.0",
		Brief:       "批量导入外部集合",
		Description: "把 Postman/OpenAPI/api-doc-go 集合正文一次导入成多条请求；dryRun 只解析并回报条数。",
		Output:      importSummaryOutput(),
	},
	{
		Name: "create_module", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.1.0",
		Brief:       "新建模块",
		Description: "在项目根或 parent 下新建模块（目录），支持嵌套。",
		Output:      moduleWriteOutput(),
	},
	{
		Name: "create_project", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.1.0",
		Brief:       "新建项目（集合）",
		Description: "在授权且可写的工作目录下新建项目；root 留空用第一个可写工作目录。",
		Output:      projectWriteOutput(),
	},
	{
		Name: "update_request", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.1.0",
		Brief:       "修改或移动接口",
		Description: "只改传入字段；folder 移动模块；ifMatch 防覆盖；body 与 bodyFields 相互独立。",
		Output:      requestDetailOutput(),
	},
	{
		Name: "delete_request", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.1.0",
		Brief:       "删除单个接口",
		Description: "移入 .trash 可找回；ifMatch/confirmName 可二次防护。",
		Output:      requestDeleteOutput(),
	},
	{
		Name: "delete_requests", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.6.0",
		Brief:       "批量删除接口",
		Description: "1–10 条，每条必须 uid+ifMatch；dryRun 只校验。",
		Output:      deleteRequestsOutput(),
	},
	{
		Name: "rename_module", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.6.0",
		Brief:       "改模块显示名",
		Description: "只改 folder.yml 的显示名，路径与目录不变。",
		Output:      moduleWriteOutput(),
	},
	{
		Name: "delete_module", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.6.0",
		Brief:       "删除模块或清理空模块",
		Description: "recursive=true 整棵移入 .trash；pruneEmpty=true 清理全部空模块（忽略 path）。",
		Output:      moduleDeleteOutput(),
	},
	{
		Name: "rename_project", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.6.0",
		Brief:       "改项目显示名",
		Description: "改 opencollection.yml 的 info.name，目录不变；project 必填。",
		Output:      projectWriteOutput(),
	},
	{
		Name: "delete_project", Group: GroupCollection, Risk: RiskHigh, Write: true, Since: "0.6.0",
		Brief:       "删除项目",
		Description: "confirm 必须等于项目显示名或目录名；目录移入授权根 .trash。",
		Output:      projectDeleteOutput(),
	},

	// ---- 文档写 ----
	{
		Name: "create_doc", Group: GroupDocs, Risk: RiskHigh, Write: true, Since: "0.2.0",
		Brief:       "新建文档",
		Description: "新建独立 .md；folder 默认 docs/；可指定 fileName；dryRun 只演练。",
		Output:      docDetailOutput(false),
	},
	{
		Name: "update_doc", Group: GroupDocs, Risk: RiskHigh, Write: true, Since: "0.2.0",
		Brief:       "修改文档",
		Description: "content 覆盖或 replaces 局部替换；name/folder 可改；ifMatch 防覆盖。",
		Output:      docDetailOutput(false),
	},
	{
		Name: "delete_doc", Group: GroupDocs, Risk: RiskHigh, Write: true, Since: "0.2.0",
		Brief:       "删除文档",
		Description: "移入集合 .trash 可找回；dryRun 只返回目标路径。",
		Output:      docDeleteOutput(),
	},
	{
		Name: "save_doc_image", Group: GroupDocs, Risk: RiskHigh, Write: true, Since: "0.5.0",
		Brief:       "保存图片到 assets",
		Description: "base64 保存 PNG/JPEG/GIF/WebP（≤5MB），返回 /doc-assets/<sha256>，可直接写进 Markdown。",
		Output:      imageOutput(),
	},

	// ---- 响应示例写 ----
	{
		Name: "save_response_example", Group: GroupExample, Risk: RiskHigh, Write: true, Since: "0.3.0",
		Brief:       "手工保存响应示例",
		Description: "不发请求，直接存一条响应示例；重名自动加序号。",
		Output:      exampleWriteOutput(),
	},
	{
		Name: "update_response_example", Group: GroupExample, Risk: RiskHigh, Write: true, Since: "0.3.0",
		Brief:       "修改响应示例",
		Description: "局部更新 name/status/headers/body/contentType（不传不动）。",
		Output:      exampleWriteOutput(),
	},
	{
		Name: "delete_response_example", Group: GroupExample, Risk: RiskHigh, Write: true, Since: "0.3.0",
		Brief:       "删除响应示例",
		Description: "文件移入集合 .trash 可找回；只删示例，不动请求本身。",
		Output:      exampleDeleteOutput(),
	},

	// ---- 环境写 ----
	{
		Name: "create_env", Group: GroupEnv, Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Brief:       "新建环境",
		Description: "新建 environments/<name>.yml，可同时写入初始变量；环境名只允许字母、数字、- 与 _。",
		Output:      envWriteOutput(),
	},
	{
		Name: "rename_env", Group: GroupEnv, Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Brief:       "环境改名",
		Description: "按新名写一份并搬敏感值，旧文件移入 .trash；目标名已占用或只改大小写会拒绝。",
		Output:      envWriteOutput(),
	},
	{
		Name: "delete_env", Group: GroupEnv, Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Brief:       "删除环境",
		Description: "两个文件都移入集合 .trash，可人工找回。",
		Output:      envDeleteOutput(),
	},
	{
		Name: "set_env_var", Group: GroupEnv, Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Brief:       "写环境变量",
		Description: "upsert 变量；secret=true 落 .secrets.yml 并对只回掩码；敏感值传掩码表示不改。",
		Output:      envVarWriteOutput(),
	},
	{
		Name: "delete_env_var", Group: GroupEnv, Risk: RiskHigh, Write: true, EnvWrite: true, Since: "0.1.0",
		Brief:       "删环境变量",
		Description: "变量不存在会报错；ifMatch 传 list_envs 的 hash 可获得并发保护。",
		Output:      envVarDeleteOutput(),
	},

	// ---- 唯一的网络外发工具（只读模式也注册，但强制不落盘）----
	{
		Name: "send_request", Group: GroupNetwork, Risk: RiskMedium, NetEgress: true, InReadOnly: true, Since: "0.1.0",
		Brief:       "发送请求（可存示例）",
		Description: "发已保存请求（uid）或临时请求（method+url），返回状态码/耗时/响应体；saveExample=true 时存为响应示例。",
		Output:      sendRequestOutput(),
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

// groupEnabled 组是否启用：groups 为空 = 全部启用；session 组恒开。
func groupEnabled(group string, groups []string) bool {
	if group == GroupSession || len(groups) == 0 {
		return true
	}
	for _, g := range groups {
		if g == group {
			return true
		}
	}
	return false
}

// NamesForMode 返回指定模式 + 工具组下应注册的工具名集合。
// readOnly=true 时只保留 InReadOnly；groups 为空表示全部组启用。
func NamesForMode(readOnly bool, groups []string) []string {
	out := make([]string, 0, len(toolMetas))
	for _, m := range toolMetas {
		if readOnly && !m.InReadOnly {
			continue
		}
		if !groupEnabled(m.Group, groups) {
			continue
		}
		out = append(out, m.Name)
	}
	return out
}

// NormalizeGroups 过滤未知组、去重并按 ConfigurableGroups 的固定顺序返回。
// 空输入返回空切片（= 全部启用），与配置文件兼容。
func NormalizeGroups(in []string) []string {
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		g := strings.ToLower(strings.TrimSpace(raw))
		if g == "" || g == GroupSession {
			continue
		}
		for _, known := range ConfigurableGroups {
			if g == known {
				seen[g] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, g := range ConfigurableGroups {
		if seen[g] {
			out = append(out, g)
		}
	}
	return out
}

// ValidateMetas 完整性自检：name 唯一非空、分组合法、风险级别合法、无别名正式项、
// 描述/Brief/版本齐备、副作用与风险自洽。供 RegisterTools 装配期与测试调用。
func ValidateMetas() error {
	if len(toolMetas) == 0 {
		return errors.New("mcp meta: 工具表为空")
	}
	validGroups := map[string]bool{GroupSession: true}
	for _, g := range ConfigurableGroups {
		validGroups[g] = true
	}
	seen := make(map[string]bool, len(toolMetas))
	hasListTools := false
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
		if !validGroups[m.Group] {
			return fmt.Errorf("mcp meta: 工具 %q 的工具组非法 %q", m.Name, m.Group)
		}
		switch m.Risk {
		case RiskLow, RiskMedium, RiskHigh:
		default:
			return fmt.Errorf("mcp meta: 工具 %q 风险级别非法 %q", m.Name, m.Risk)
		}
		if m.Description == "" {
			return fmt.Errorf("mcp meta: 工具 %q 缺描述", m.Name)
		}
		if m.Brief == "" {
			return fmt.Errorf("mcp meta: 工具 %q 缺 Brief", m.Name)
		}
		if n := len([]rune(m.Brief)); n > 40 {
			return fmt.Errorf("mcp meta: 工具 %q 的 Brief 过长（%d 字，上限 40）", m.Name, n)
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
		// session 组恒开：必须只读且始终注册，否则关掉其它组会连发现能力一起丢掉。
		if m.Group == GroupSession && (!m.InReadOnly || m.Risk != RiskLow || m.Write) {
			return fmt.Errorf("mcp meta: session 组工具必须 low+只读+InReadOnly: %q", m.Name)
		}
		if m.Name == "list_tools" {
			hasListTools = true
		}
		seen[m.Name] = true
	}
	if !hasListTools {
		return errors.New("mcp meta: 缺少 list_tools（发现入口）")
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

// toolsCatalogOutput list_tools 的结果：工具名 + 用途 + 必填参数（轻量总览，不含完整 schema）。
func toolsCatalogOutput() map[string]any {
	item := objSchema(map[string]any{
		"name": strSchema(), "risk": strSchema(), "write": boolSchema(),
		"description": strSchema(), "required": arrSchema(strSchema()),
	}, "name", "description")
	return objSchema(map[string]any{
		"total": intSchema(), "returned": intSchema(), "tools": arrSchema(item),
	}, "tools")
}

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
		"path": strSchema(), "method": strSchema(), "url": strSchema(), "planned": strSchema(),
	}, "uid", "path")
}

// deleteRequestsOutput delete_requests（批量）的结果。
func deleteRequestsOutput() map[string]any {
	f := objSchema(map[string]any{
		"index": intSchema(), "uid": strSchema(), "code": strSchema(), "error": strSchema(),
	}, "uid")
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

// batchUpdateOutput update_requests 的结果（成功 uid 列表 + 失败明细）。
func batchUpdateOutput() map[string]any {
	f := objSchema(map[string]any{
		"index": intSchema(), "uid": strSchema(), "path": strSchema(),
		"code": strSchema(), "error": strSchema(),
	}, "uid")
	return objSchema(map[string]any{
		"project": strSchema(), "requested": intSchema(), "dryRun": boolSchema(),
		"updated": arrSchema(strSchema()), "failed": arrSchema(f),
	}, "requested")
}

// importSummaryOutput import_collection 的结果（批量导入的条数与失败明细）。
func importSummaryOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "format": strSchema(), "parent": strSchema(),
		"imported": intSchema(), "skipped": intSchema(),
		"failures": arrSchema(strSchema()), "dryRun": boolSchema(),
	}, "format", "imported")
}

// moduleWriteOutput create_module / rename_module 的结果。
func moduleWriteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "path": strSchema(), "name": strSchema(),
	}, "path")
}

// moduleDeleteOutput delete_module 的结果（含 pruneEmpty 模式的 count）。
func moduleDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"project": strSchema(), "path": strSchema(), "recursive": boolSchema(),
		"pruneEmpty": boolSchema(), "count": intSchema(),
		"deleted": boolSchema(), "trash": boolSchema(), "dryRun": boolSchema(),
	}, "project")
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
		"name": strSchema(), "path": strSchema(), "status": intSchema(), "planned": strSchema(),
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

// docDeleteOutput delete_doc 的结果（只带身份与路径，避免暴露内容）。
func docDeleteOutput() map[string]any {
	return objSchema(map[string]any{
		"uid": strSchema(), "path": strSchema(), "deleted": boolSchema(), "trash": boolSchema(), "dryRun": boolSchema(),
	}, "uid")
}
