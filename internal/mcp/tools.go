package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/mcpaudit"
	"api-doc-go-client/internal/mcpauth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// guardLogger 记录 guard 拦下的 panic 等内部异常；NewServer 时用 Config.Logger 替换。
var guardLogger = log.New(io.Discard, "[mcp-guard] ", log.LstdFlags)

// 工具集：把 Service 的方法注册成 MCP 工具。Out 一律用 any（不声明输出 schema），
// 结果以「人类/AI 可读文本」放进 Content —— AI 工具链里文本比结构化 JSON 更省 token、也更好读。
type toolSet struct {
	svc      *Service
	readOnly bool     // 只读模式：不注册写工具，send_request 强制不落盘
	groups   []string // 启用的工具组（空 = 全部；session 恒开）

	// authz 风险工具交互授权（T2）：nil = 不拦截（stdio 自动化 / 开关关闭）。
	authz mcpauth.Authorizer
	// audit 调用审计（T3）：nil = 不落审计（测试 / 未注入）。
	audit      mcpaudit.Auditor
	transport  string // 授权弹窗展示："http" | "stdio"
	clientHint string // 授权弹窗展示的来源线索

	// mu/sel 每个 MCP 会话选定的默认工作目录（use_workspace 的结果），key = 会话 id。
	// 为什么要按会话：HTTP 传输下同一个进程会同时服务多个客户端（各自带 Mcp-Session-Id），
	// 进程级单例会变成「A 选了目录，B 的默认目标跟着变」—— 谁都不该改别人的选择。
	// stdio 与内嵌服务只有一个会话，等价于「进程里只有一份」；拿不到会话标识时用空串兜底。
	mu  sync.Mutex
	sel map[string]string

	// required 工具名 → 必填参数名（注册期写入，之后只读），供 list_tools 回报。
	// 从 In 结构体的 json tag 推导（不带 omitempty 即必填），与 SDK 生成 inputSchema
	// 的规则同源，避免再手工维护一份清单而漂移。
	required map[string][]string
}

// noteRequired 记录某工具的必填参数（注册期调用，运行期只读）。
func (ts *toolSet) noteRequired(name string, in any) {
	t := reflect.TypeOf(in)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return
	}
	list := make([]string, 0, 4)
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		comma := strings.Index(tag, ",")
		field := tag
		if comma >= 0 {
			field = tag[:comma]
		}
		if field == "" || field == "-" || strings.Contains(tag, "omitempty") {
			continue
		}
		list = append(list, field)
	}
	ts.mu.Lock()
	if ts.required == nil {
		ts.required = map[string][]string{}
	}
	ts.required[name] = list
	ts.mu.Unlock()
}

// requiredOf 某工具的必填参数（无记录时返回空切片）。
func (ts *toolSet) requiredOf(name string) []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]string(nil), ts.required[name]...)
}

// sessionID 本次工具调用所属的会话标识（拿不到时返回空串 = 进程级那一份）。
func sessionID(req *mcp.CallToolRequest) string {
	if req == nil || req.Session == nil {
		return ""
	}
	return req.Session.ID()
}

// workspace 本会话选定的默认工作目录（空 = 未选定）。
func (ts *toolSet) workspace(req *mcp.CallToolRequest) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.sel[sessionID(req)]
}

// rememberWorkspace 记住本会话选定的默认工作目录。
func (ts *toolSet) rememberWorkspace(req *mcp.CallToolRequest, key string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.sel == nil {
		ts.sel = map[string]string{}
	}
	ts.sel[sessionID(req)] = key
}

// proj 工具入参 project 为空时补上本会话选定的默认目标；没选定就原样返回空，
// 由 Service 报错并提示「先 use_workspace」。
func (ts *toolSet) proj(req *mcp.CallToolRequest, project string) string {
	if strings.TrimSpace(project) != "" {
		return project
	}
	return ts.workspace(req)
}

// RegisterTools 把全部工具注册到 server（测试便捷封装：不带交互授权）。
//
// 名称、描述、风险与读写标记的单一事实源是 meta.go；这里只负责把 handler 挂上去。
// 元数据缺失/重复或实际注册集合与 meta 不一致都属于装配缺陷，直接 panic ——
// 必须在开发期暴露，而不是等外部 AI 调用时才发现契约漂移。
// toolGuardOpts 注册时的 guard 配置（NewServer 注入；测试走 RegisterTools 零值=不拦截）。
type toolGuardOpts struct {
	authorizer mcpauth.Authorizer
	auditor    mcpaudit.Auditor
	transport  string
	clientHint string
}

func RegisterTools(server *mcp.Server, svc *Service, readOnly bool) {
	if err := registerToolsWithOpts(server, svc, readOnly, nil, toolGuardOpts{}); err != nil {
		panic(err)
	}
}

// RegisterToolsWithGroups 同 RegisterTools，但按工具组过滤（空 = 全部；session 恒开）。
// 测试与嵌入式装配共用；内嵌服务走 NewServer(Config.Groups)。
func RegisterToolsWithGroups(server *mcp.Server, svc *Service, readOnly bool, groups []string) {
	if err := registerToolsWithOpts(server, svc, readOnly, groups, toolGuardOpts{}); err != nil {
		panic(err)
	}
}

func registerToolsWithOpts(server *mcp.Server, svc *Service, readOnly bool, groups []string, opts toolGuardOpts) error {
	if err := ValidateMetas(); err != nil {
		return err
	}
	// SDK 在进入 handler 前会按 inputSchema 校验参数；那类错误没有我们的错误码，
	// 这里统一补 [INVALID]（其余未分类补 [INTERNAL]），保证「失败: [CODE] ...」恒成立。
	server.AddReceivingMiddleware(toolErrorCodeMiddleware())
	ts := &toolSet{
		svc: svc, readOnly: readOnly, groups: NormalizeGroups(groups),
		authz:      opts.authorizer,
		audit:      opts.auditor,
		transport:  opts.transport,
		clientHint: opts.clientHint,
	}
	ts.registerReadTools(server)
	if readOnly {
		// 只读模式下仍给 send_request，但明确「不落盘」——AI 仍可试发，只是不污染集合
		ts.registerSendTool(server, true)
	} else {
		ts.registerWriteTools(server)
		ts.registerSendTool(server, false)
	}
	ts.registerDocTemplateResource(server)
	ts.registerStaticResources(server)
	return ts.assertRegistered(server, readOnly)
}

// addTool 按 meta.go 中的名称取描述注册一个工具；名字不在元数据表里即装配缺陷。
// guard 在这一层包住所有 handler：panic recover + 风险工具交互授权，业务 handler 零改动。
// 工具组未启用时直接跳过注册（handler 仍构造，但不会出现在 tools/list）。
// 必须在内部构造**字面签名**的闭包交给 mcp.AddTool —— 若用具名 coreHandler[In] 类型，
// SDK 的 ToolHandlerFor[In,Out] 泛型推断无法把具名类型与类型参数合一。
func addTool[In any](ts *toolSet, server *mcp.Server, name string,
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error)) {
	meta, ok := MetaOf(name)
	if !ok {
		panic(fmt.Sprintf("mcp: 工具 %q 缺少 meta.go 元数据", name))
	}
	if !groupEnabled(meta.Group, ts.groups) {
		return
	}
	var zero In
	ts.noteRequired(meta.Name, &zero)
	wrapped := func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		return runGuarded(ts, meta, ctx, req, func() (*mcp.CallToolResult, any, error) {
			return h(ctx, req, in)
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: meta.Name, Description: meta.Description, OutputSchema: meta.Output}, wrapped)
}

// toolResultCore 已完成授权检查的业务执行单元。
type toolResultCore func() (*mcp.CallToolResult, any, error)

// runGuarded 执行「recover → 风险授权 → 业务」装饰链，并在收尾写一条 T3 审计；
// 拒绝/异常统一转成工具级错误（IsError=true），不是协议级 error —— AI 能读到并纠正。
//
// 一次调用恰好产生一条审计：ok / biz_error / denied / panic 四出口全覆盖。
func runGuarded(ts *toolSet, meta ToolMeta, ctx context.Context, req *mcp.CallToolRequest, fn toolResultCore) (res *mcp.CallToolResult, out any, err error) {
	start := time.Now()
	kind := mcpaudit.KindOK
	var azRes mcpauth.Result
	var project, argsSummary string

	defer func() {
		if r := recover(); r != nil {
			guardLogger.Printf("工具 %s 发生 panic: %v", meta.Name, r)
			kind = mcpaudit.KindPanic
			res = toolError(fmt.Sprintf("内部错误：工具 %s 执行异常（%v）", meta.Name, r))
			out, err = nil, nil
		}
		ts.auditOne(req, meta, start, kind, azRes, project, argsSummary, res)
	}()

	if ts.authz != nil && meta.Risk != RiskLow {
		project = ts.proj(req, argsProject(req))
		argsSummary = summarizeArgs(meta.Name, req)
		var denyReason string
		var azErr error
		azRes, denyReason, azErr = ts.authorize(ctx, req, meta, project, argsSummary)
		if azErr != nil {
			// 授权基础设施异常：fail-closed，记 denied + 原因
			kind = mcpaudit.KindDenied
			return toolError(azErr.Error()), nil, nil
		}
		if denyReason != "" {
			kind = mcpaudit.KindDenied
			return toolError(fmt.Sprintf("用户拒绝了 %s 的授权：%s", meta.Name, denyReason)), nil, nil
		}
	}

	res, out, err = fn()
	if res != nil && res.IsError {
		kind = mcpaudit.KindBizError
	}
	// 写盘成功后失效候选缓存：否则紧跟着的 NOT_FOUND 可能给出写之前的旧清单
	// （fsnotify 回写有延迟，候选的节流刷新会把它当成「刚刷过」而跳过）。
	if meta.Write && err == nil && (res == nil || !res.IsError) && ts.svc != nil {
		ts.svc.ResetCandidateCache()
	}
	return res, out, err
}

// authzTimeout 单次交互授权的最长等待：超时按拒绝处理（fail-closed）。
const authzTimeout = 60 * time.Second

// authorize 构造脱敏授权请求并调用授权器。
// 返回：授权结论；非空 denyReason=被拒绝的对外文案；err=授权基础设施异常。
func (ts *toolSet) authorize(ctx context.Context, req *mcp.CallToolRequest, meta ToolMeta,
	project, argsSummary string) (mcpauth.Result, string, error) {
	azReq := mcpauth.Request{
		SessionID:   sessionID(req),
		Transport:   ts.transport,
		Tool:        meta.Name,
		Risk:        mcpauth.Risk(meta.Risk),
		Write:       meta.Write,
		NetEgress:   meta.NetEgress,
		Project:     project,
		ArgsSummary: argsSummary,
		ClientHint:  ts.clientHint,
		CreatedAt:   time.Now().UnixMilli(),
	}
	cctx, cancel := context.WithTimeout(ctx, authzTimeout)
	defer cancel()
	res, err := ts.authz.Authorize(cctx, azReq)
	if err != nil {
		return res, "", fmt.Errorf("授权服务未就绪：%w", err)
	}
	if res.Decision == mcpauth.DecisionDeny {
		reason := map[string]string{
			"timeout":         "等待确认超时（60 秒未响应，已按拒绝处理）",
			"cancel":          "调用已取消",
			"headless-strict": "无头严格模式禁止风险操作（需在客户端界面确认，或去掉 -require-consent）",
			"user":            "用户拒绝了该操作",
		}[res.Reason]
		if reason == "" {
			reason = "用户拒绝了该操作"
		}
		return res, reason, nil
	}
	return res, "", nil
}

// auditOne 收尾写一条审计；auditor=nil 时静默跳过。
func (ts *toolSet) auditOne(req *mcp.CallToolRequest, meta ToolMeta, start time.Time,
	kind mcpaudit.ResultKind, az mcpauth.Result, project, argsSummary string, res *mcp.CallToolResult) {
	if ts.audit == nil {
		return
	}
	now := time.Now()
	ev := mcpaudit.Event{
		TS:          now.Format(time.RFC3339Nano),
		TSMs:        now.UnixMilli(),
		SessionID:   sessionID(req),
		Transport:   ts.transport,
		Client:      ts.clientHint,
		Tool:        meta.Name,
		Risk:        string(meta.Risk),
		Write:       meta.Write,
		NetEgress:   meta.NetEgress,
		EnvWrite:    meta.EnvWrite,
		Project:     project,
		ArgsSummary: argsSummary,
		ResultOK:    kind == mcpaudit.KindOK,
		ResultKind:  string(kind),
		DurationMs:  time.Since(start).Milliseconds(),
	}
	// 只有真正走过授权判定的调用才有「批准」语义：low 风险不询问，approved 保持 nil（= 未询问）。
	if ts.authz != nil && meta.Risk != RiskLow {
		approved := az.Decision != mcpauth.DecisionDeny
		ev.Approved = &approved
		ev.GrantedBy = az.GrantedBy
		ev.RequestID = az.RequestID
	}
	if kind == mcpaudit.KindPanic {
		ev.ResultSummary = "panic recovered"
	} else if res != nil {
		ev.ResultSummary = mcpaudit.Clamp(mcpaudit.RedactText(textContents(res)), mcpauditResultCap)
	}
	// 只有真正走了交互授权链才带审批字段；low 工具/未启用授权时保持空（omitempty）。
	if ts.authz != nil && meta.Risk != RiskLow {
		if az.Decision != "" {
			approved := az.Decision != mcpauth.DecisionDeny
			ev.Approved = &approved
			ev.GrantedBy = az.GrantedBy
			ev.RequestID = az.RequestID
		}
	}
	if ev.Project == "" {
		ev.Project = ts.proj(req, argsProject(req)) // low 工具也尽量记上目标项目
	}
	if ev.ArgsSummary == "" {
		ev.ArgsSummary = summarizeArgs(meta.Name, req)
	}
	ts.audit.Log(ev)
}

// mcpauditResultCap 结果摘要上限（与 mcpaudit.resultCap 对齐，包间不导出常量，本地镜像）。
const mcpauditResultCap = 1024

// textContents 拼接工具结果里的全部文本块。
func textContents(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// argsProject 从原始入参里只读地取 project（用于授权 key 与弹窗展示，不产生任何副作用）。
func argsProject(req *mcp.CallToolRequest) string {
	if req == nil || req.Params == nil {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &m); err != nil {
		return ""
	}
	p, _ := m["project"].(string)
	return p
}

// toolError 构造工具级错误结果（业务错误的统一表达，与 wrap 同形）。
func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: "失败: " + msg}},
	}
}

// assertRegistered 断言「实际注册到 server 的工具集合」==「meta 表在该模式下应有的集合」，
// 双向无漏：meta 有而 handler 没注册、handler 注册了 meta 没有的名字都会失败。
func (ts *toolSet) assertRegistered(server *mcp.Server, readOnly bool) error {
	got, err := ListToolNames(context.Background(), server)
	if err != nil {
		return fmt.Errorf("mcp: 注册集合自检失败: %w", err)
	}
	want := NamesForMode(readOnly, ts.groups)
	gotSet := make(map[string]bool, len(got))
	for _, n := range got {
		gotSet[n] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
	}
	var missing, extra []string
	for _, n := range want {
		if !gotSet[n] {
			missing = append(missing, n)
		}
	}
	for _, n := range got {
		if !wantSet[n] {
			extra = append(extra, n)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		return fmt.Errorf("mcp: 注册集合与 meta 表不一致（readOnly=%v），缺少 %v，多出 %v",
			readOnly, missing, extra)
	}
	return nil
}

// toolErrorCodeMiddleware 给 SDK 层错误（参数 schema 校验、未知工具）补稳定错误码。
// handler 已把业务错误包装成 `失败: [CODE] ...`，这里只处理「文本里没有 [CODE]」的情况。
//
// 顺带做一件评审提的事：SDK 的 schema 校验文案是英文（"validating ... missing properties"），
// 与其余中文错误混排，且看不出是哪个参数。这里统一翻成中文并尽量带上参数名与工具名。
func toolErrorCodeMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if method != "tools/call" || err != nil || res == nil {
				return res, err
			}
			call, ok := res.(*mcp.CallToolResult)
			if !ok || !call.IsError || len(call.Content) == 0 {
				return res, err
			}
			text, ok := call.Content[0].(*mcp.TextContent)
			if !ok || strings.Contains(text.Text, "] ") {
				return res, err
			}
			raw := strings.TrimPrefix(text.Text, "失败: ")
			code := CodeInternal
			msg := raw
			switch {
			case strings.Contains(raw, "unknown tool"), strings.Contains(raw, "not found"):
				code = CodeNotFound
			case strings.Contains(raw, "validating"), strings.Contains(raw, "missing properties"),
				strings.Contains(raw, "additional properties"):
				code = CodeInvalid
				msg = humanizeSchemaError(toolNameOf(req), raw)
			}
			call.Content = []mcp.Content{&mcp.TextContent{Text: "失败: " + coded(code, errors.New(msg)).Error()}}
			return res, err
		}
	}
}

// toolNameOf 取本次调用的工具名（拿不到时返回空串，报错里退化为「本工具」）。
func toolNameOf(req mcp.Request) string {
	call, ok := req.(*mcp.CallToolRequest)
	if !ok || call.Params == nil {
		return ""
	}
	return call.Params.Name
}

func (ts *toolSet) registerReadTools(server *mcp.Server) {
	addTool(ts, server, "list_tools", ts.listTools)
	addTool(ts, server, "list_workspaces", ts.listWorkspaces)
	addTool(ts, server, "use_workspace", ts.useWorkspace)
	addTool(ts, server, "list_projects", ts.listProjects)
	addTool(ts, server, "get_project_modules", ts.getProjectModules)
	// 详情：uid 取单个，uids 批量（原 get_request_details 已并入）
	addTool(ts, server, "get_request_detail", ts.getRequestDetail)
	addTool(ts, server, "search_requests", ts.searchRequests)
	addTool(ts, server, "search_docs", ts.searchDocs)
	addTool(ts, server, "list_envs", ts.listEnvs)
	// 文档条目：列表不含正文，取全文走 get_doc（uid/uids 双形态）
	addTool(ts, server, "list_docs", ts.listDocs)
	addTool(ts, server, "get_doc", ts.getDoc)
}

// ---- 工具总览（list_tools）----

// listToolsIn list_tools 的入参。
type listToolsIn struct {
	Filter string `json:"filter,omitempty" jsonschema:"按工具名或描述关键词过滤（大小写无关）；留空 = 全部"`
	Mode   string `json:"mode,omitempty" jsonschema:"all（默认，全部）/ read（只读与会话类）/ write（会落盘改集合的）"`
}

// toolCatalogItem 总览里的一行（结构化输出的最小集）。
type toolCatalogItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Risk        string   `json:"risk"`
	Write       bool     `json:"write"`
	Required    []string `json:"required"`
}

func (ts *toolSet) listTools(_ context.Context, _ *mcp.CallToolRequest, in listToolsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		items, total := ts.catalog(in.Filter, in.Mode)
		data := map[string]any{
			"total":    total,
			"returned": len(items),
			"tools":    nonNilSlice(items),
		}
		text := renderToolCatalog(items, total, in.Filter, in.Mode, ts.readOnly)
		return text + taskCheatSheet(ts.readOnly, ts.groups, in.Filter), data, nil
	})
}

// catalog 按过滤条件产出总览行；total 恒为「当前模式已注册的工具总数」。
// 只读模式下只列本模式可用的工具，避免 AI 挑了未注册的工具却调用不到。
func (ts *toolSet) catalog(filter, mode string) ([]toolCatalogItem, int) {
	total := len(NamesForMode(ts.readOnly, ts.groups))
	filter = strings.ToLower(strings.TrimSpace(filter))
	wantWrite := -1 // -1=不限，0=只读，1=写
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "read":
		wantWrite = 0
	case "write":
		wantWrite = 1
	}
	out := make([]toolCatalogItem, 0, total)
	for _, m := range toolMetas {
		if ts.readOnly && !m.InReadOnly {
			continue
		}
		if !groupEnabled(m.Group, ts.groups) {
			continue
		}
		if wantWrite == 0 && m.Write {
			continue
		}
		if wantWrite == 1 && !m.Write {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(m.Name), filter) &&
			!strings.Contains(strings.ToLower(m.Description), filter) &&
			!strings.Contains(strings.ToLower(m.Brief), filter) {
			continue
		}
		out = append(out, toolCatalogItem{
			Name:        m.Name,
			Description: m.Brief, // 只回一句话摘要：完整描述留给该工具的 schema
			Risk:        string(m.Risk),
			Write:       m.Write,
			Required:    nonNilSlice(ts.requiredOf(m.Name)),
		})
	}
	return out, total
}

// ---- 工作目录（白名单 + 默认目标）----

// listWorkspacesIn list_workspaces 无入参。
type listWorkspacesIn struct{}

func (ts *toolSet) listWorkspaces(_ context.Context, req *mcp.CallToolRequest, _ listWorkspacesIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		sel := ts.workspace(req) // 默认目标是会话级的：只标本会话选的那个
		return renderWorkspaces(ts.svc.ListWorkspaces(sel), sel), nil
	})
}

// useWorkspaceIn use_workspace 的入参。
type useWorkspaceIn struct {
	Workspace string `json:"workspace" jsonschema:"工作目录 path（list_workspaces）或项目名/路径/uid；须在授权白名单内"`
}

func (ts *toolSet) useWorkspace(_ context.Context, req *mcp.CallToolRequest, in useWorkspaceIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		it, key, err := ts.svc.UseWorkspace(in.Workspace)
		if err != nil {
			return "", err
		}
		ts.rememberWorkspace(req, key) // 只改本会话的默认目标
		return renderWorkspaceSelected(it, key), nil
	})
}

func (ts *toolSet) registerWriteTools(server *mcp.Server) {
	addTool(ts, server, "create_request", ts.createRequest)
	addTool(ts, server, "update_requests", ts.updateRequests)
	addTool(ts, server, "import_collection", ts.importCollection)
	addTool(ts, server, "create_module", ts.createModule)
	addTool(ts, server, "create_project", ts.createProject)
	addTool(ts, server, "update_request", ts.updateRequest)
	addTool(ts, server, "delete_request", ts.deleteRequest)
	addTool(ts, server, "delete_requests", ts.deleteRequests)
	// 文档条目的写操作：新建 / 局部更新（正文·改名·移动）/ 删除（进 .trash）
	addTool(ts, server, "create_doc", ts.createDoc)
	addTool(ts, server, "update_doc", ts.updateDoc)
	addTool(ts, server, "delete_doc", ts.deleteDoc)
	// 图片：存到项目 assets/，返回可写进正文的 URL（Markdown 编辑器同款）
	addTool(ts, server, "save_doc_image", ts.saveDocImage)
	// 容器级：模块/项目的改名与删除（删除进 .trash）；空模块清理并入 delete_module
	addTool(ts, server, "rename_module", ts.renameModule)
	addTool(ts, server, "delete_module", ts.deleteModule)
	addTool(ts, server, "rename_project", ts.renameProject)
	addTool(ts, server, "delete_project", ts.deleteProject)
	// 响应示例的写操作：手工新增 / 局部更新（改名·状态码·响应体）/ 删除（进 .trash）
	addTool(ts, server, "save_response_example", ts.saveResponseExample)
	addTool(ts, server, "update_response_example", ts.updateResponseExample)
	addTool(ts, server, "delete_response_example", ts.deleteResponseExample)
	ts.registerEnvWriteTools(server)
}

// registerEnvWriteTools 环境变量的写操作（5 个环境/变量级写工具）。
// 单列一个函数：环境变量是一组有共同约定的工具（敏感值掩码、名字规则），放一起更好维护。
func (ts *toolSet) registerEnvWriteTools(server *mcp.Server) {
	addTool(ts, server, "create_env", ts.createEnv)
	addTool(ts, server, "rename_env", ts.renameEnv)
	addTool(ts, server, "delete_env", ts.deleteEnv)
	addTool(ts, server, "set_env_var", ts.setEnvVar)
	addTool(ts, server, "delete_env_var", ts.deleteEnvVar)
}

func (ts *toolSet) registerSendTool(server *mcp.Server, forceNoSave bool) {
	meta, ok := MetaOf("send_request")
	if !ok {
		panic("mcp: 工具 send_request 缺少 meta.go 元数据")
	}
	if !groupEnabled(meta.Group, ts.groups) {
		return
	}
	ts.noteRequired(meta.Name, &SendRequestInput{})
	desc := meta.Description
	if forceNoSave {
		desc = "发送请求并返回结果（只读模式：不会保存响应示例）。可以给 uid 发已保存的请求，也可以给 method+url 发临时请求。"
	}
	// 描述：只读模式用覆盖文案（addTool 默认取 meta 描述），故这里直接 AddTool，
	// 但仍走同一个 runGuarded 装饰链，保证授权/recover 不缺。
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in SendRequestInput) (*mcp.CallToolResult, any, error) {
		return runGuarded(ts, meta, ctx, req, func() (*mcp.CallToolResult, any, error) {
			if forceNoSave {
				no := false
				in.SaveExample = &no
			}
			return ts.wrapData(func() (string, any, error) {
				in.Project = ts.proj(req, in.Project)
				out, err := ts.svc.SendRequest(in)
				if err != nil {
					return "", nil, err
				}
				return renderSend(out), sendData(out), nil
			})
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: meta.Name, Description: desc, OutputSchema: meta.Output}, handler)
}

// docTemplateURI 接口说明模板的 resource 地址（原 get_doc_template 工具已迁移至此）。
const docTemplateURI = "apidoc://doc-template"

// registerDocTemplateResource 把「接口说明（docs）分节结构」注册为 MCP resource。
//
// 为什么从工具迁到 resource：它是无参数、内容固定的静态文本，作为工具只会占用
// tools/list 的 schema 与一次调用往返；resource 才是协议里"可读取的静态内容"。
func (ts *toolSet) registerDocTemplateResource(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI:      docTemplateURI,
		Name:     "doc_template",
		Title:    "接口说明（docs）模板 · Doc template",
		MIMEType: "text/markdown",
		Description: "接口说明的分节结构契约：与客户端「生成接口文档」的 Markdown 骨架逐节对齐" +
			"（简要描述/请求URI/请求方式/请求头/请求参数表/请求示例/响应字段表/响应示例/备注）。" +
			" Content is a Chinese Markdown document.",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := docTemplateURI
		if req != nil && req.Params != nil && req.Params.URI != "" {
			uri = req.Params.URI
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: uri, MIMEType: "text/markdown", Text: ts.svc.DocTemplate(),
		}}}, nil
	})
}

// errorCodesURI 错误码表的 resource 地址。
const errorCodesURI = "apidoc://error-codes"

// collectionFormatURI 集合结构契约的 resource 地址。
const collectionFormatURI = "apidoc://collection-format"

// registerStaticResources 注册静态说明类 resource：错误码表与集合结构契约。
//
// 这些内容无参数、很少变，放在 resource 里比占一个工具位更合适 ——
// tools/list 的每个工具都会在会话开始时被拉一次，是固定成本。
func (ts *toolSet) registerStaticResources(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI: errorCodesURI, Name: "error_codes", Title: "MCP 错误码表 · Error codes", MIMEType: "text/markdown",
		Description: "工具失败时的 [CODE] 含义与处理建议（NOT_FOUND/CONFLICT/INVALID/READONLY/FORBIDDEN/LIMIT/IO/INTERNAL）。" +
			" Content is a Chinese Markdown document.",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return staticResource(req, errorCodesURI, errorCodeTable()), nil
	})
	server.AddResource(&mcp.Resource{
		URI: collectionFormatURI, Name: "collection_format", Title: "集合结构契约 · Collection format", MIMEType: "text/markdown",
		Description: "集合目录结构、请求文件 YAML 段、body.fields 为唯一真源、.trash 语义。" +
			" Content is a Chinese Markdown document.",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return staticResource(req, collectionFormatURI, collectionFormatDoc()), nil
	})
}

// staticResource 构造 resource 读取结果（请求里没带 URI 时用 fallback）。
func staticResource(req *mcp.ReadResourceRequest, fallback, text string) *mcp.ReadResourceResult {
	uri := fallback
	if req != nil && req.Params != nil && req.Params.URI != "" {
		uri = req.Params.URI
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI: uri, MIMEType: "text/markdown", Text: text,
	}}}
}

// errorCodeTable 错误码表（resource 正文）。
func errorCodeTable() string {
	return `# MCP 错误码

失败统一返回 ` + "`失败: [CODE] 说明`" + `（工具级 isError，不是协议错误）。

| 码 | 含义 | 典型触发 |
|---|---|---|
| NOT_FOUND | 项目/接口/文档/示例/变量不存在 | uid 拼错、path 不匹配 |
| CONFLICT | 同名冲突或 ifMatch 不匹配 | 目标已存在、读后被外部改过 |
| INVALID | 参数缺失或取值非法 | 缺必填、section 越界、method 非法 |
| READONLY | 工作目录是只读授权 | 写工具落在只读目录 |
| FORBIDDEN | 目录不在白名单或无写权限 | 未授权的工作目录 |
| LIMIT | 批量或分页超限 | uids/items 超过 10 |
| IO | 读写磁盘失败 | 权限、磁盘满 |
| INTERNAL | 其它内部错误 | 未分类错误（panic 已 recover） |

处理建议：NOT_FOUND 先看报错附带的候选；CONFLICT 重新读取拿新 hash 再重试；
INVALID 按报错点名的字段改（不要整单重试）。
`
}

// collectionFormatDoc 集合结构契约（resource 正文）。
func collectionFormatDoc() string {
	return `# 集合（项目）结构契约

一个项目 = 一个含 ` + "`opencollection.yml`" + ` 的目录。
请求是 YAML 文件（` + "`<name>.yml`" + `），文档条目是独立的 ` + "`.md`" + `。

## 请求文件关键段

- ` + "`meta`" + `：uid / name / seq（身份；改名只改显示名，不动文件名）
- ` + "`http`" + `：method / url / headers / params / body
- ` + "`body.type`" + `：none | json | text | form | multipart（空串与 none 都表示无请求体）
- ` + "`body.fields`" + `：请求体字段表，**唯一真源**；` + "`body.descs`" + ` 只作首次生成文档的描述种子
- ` + "`docs`" + `：接口说明（Markdown，随请求文件）
- ` + "`respdoc`" + `：响应字段文档（类型/字段表/样例/状态码）
- ` + "`settings`" + ` / ` + "`auth`" + ` / ` + "`grpc`" + `：单请求设置

## 文档条目

独立 ` + "`.md`" + `，带 frontmatter（uid/name），可挂在任意分组目录下；
集合根目录的 README.md 也会作为条目出现（uid 由路径哈希生成）。

## 删除与回收

- 请求/文档/模块/示例：移入集合内 ` + "`.trash/`" + `（可人工找回）
- 项目：移入授权根下 ` + "`.trash/<目录名>-<时间戳>`" + `
`
}

// ---- 各工具的 handler（统一经 wrap 把 panic/错误转成工具结果） ----

// listProjectsIn list_projects 的入参。
type listProjectsIn struct {
	Project string `json:"project,omitempty" jsonschema:"按项目名或路径模糊过滤；留空返回全部"`
	Refresh bool   `json:"refresh,omitempty" jsonschema:"true = 重新扫描项目根目录（新增/删除了项目目录时用）"`
}

func (ts *toolSet) listProjects(ctx context.Context, req *mcp.CallToolRequest, in listProjectsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		items, skipped, err := ts.svc.ListProjects(in.Project, in.Refresh)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{"projects": nonNilSlice(items), "skipped": skipped}
		if len(items) == 0 {
			return "没有匹配的项目。用 refresh=true 重新扫描，或确认 -root 下有含 opencollection.yml 的目录。", data, nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "共 %d 个项目：\n", len(items))
		for _, p := range items {
			if p.Skipped != "" {
				fmt.Fprintf(&sb, "- %s（%s）：%s\n", p.Path, p.Name, p.Skipped)
				continue
			}
			fmt.Fprintf(&sb, "- %s（%s，uid=%s）：%d 个请求 / %d 个模块", p.Path, p.Name, p.UID, p.Requests, p.Folders)
			if len(p.Envs) > 0 {
				fmt.Fprintf(&sb, "；环境：%s", strings.Join(p.Envs, ", "))
			}
			sb.WriteString("\n")
		}
		if skipped > 0 {
			fmt.Fprintf(&sb, "\n（另有 %d 个目录不是可用项目，已跳过）", skipped)
		}
		return sb.String(), data, nil
	})
}

// getModulesIn get_project_modules 的入参。
type getModulesIn struct {
	Project         string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Query           string `json:"query,omitempty" jsonschema:"对模块名与请求名/方法/路径做模糊过滤"`
	IncludeRequests bool   `json:"includeRequests,omitempty" jsonschema:"true = 附带每个模块下的请求清单"`
	OnlyMatched     *bool  `json:"onlyMatched,omitempty" jsonschema:"有 query 时默认只回命中模块；false = 全部（汇总行始终给总数）"`
}

func (ts *toolSet) getProjectModules(ctx context.Context, req *mcp.CallToolRequest, in getModulesIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		// 默认：带 query 就收敛到「有命中的模块」；显式传 onlyMatched=false 才看全量
		onlyMatched := strings.TrimSpace(in.Query) != ""
		if in.OnlyMatched != nil {
			onlyMatched = *in.OnlyMatched
		}
		res, err := ts.svc.GetProjectModules(in.Project, in.Query, in.IncludeRequests, onlyMatched)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "moduleTotal": res.ModuleTotal, "matched": res.Matched,
			"total": res.Total, "modules": nonNilSlice(res.Modules),
		}
		if len(res.Requests) > 0 {
			data["requests"] = res.Requests
		}
		var sb strings.Builder
		if in.Query != "" {
			fmt.Fprintf(&sb, "项目 %s：共 %d 个模块（有命中 %d 个），%d 个请求，其中 %d 个匹配「%s」",
				in.Project, res.ModuleTotal, len(res.Modules), res.Total, res.Matched, in.Query)
		} else {
			fmt.Fprintf(&sb, "项目 %s：%d 个模块，%d 个请求", in.Project, res.ModuleTotal, res.Total)
		}
		sb.WriteString("\n")
		for _, m := range res.Modules {
			fmt.Fprintf(&sb, "- %s：%d 个请求 / %d 个子模块\n", m.Path, m.Requests, m.Dirs)
			for _, r := range res.Requests[m.Path] {
				fmt.Fprintf(&sb, "    · [%s] %s %s（uid=%s）\n", r.Method, r.Name, r.URL, r.UID)
			}
		}
		if len(res.Modules) == 0 {
			sb.WriteString("（该项目还没有模块；用 create_module 建一个）")
		}
		return sb.String(), data, nil
	})
}

// getDetailIn get_request_detail 的入参（uid/path 单个 或 uids 批量，二选一）。
type getDetailIn struct {
	Project      string   `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID          string   `json:"uid,omitempty" jsonschema:"接口 uid（单个查询；与 path/uids 三选一）"`
	Path         string   `json:"path,omitempty" jsonschema:"请求文件路径（如 api/users.yml 或 api/users）；与 uid 二选一，批量用 uids"`
	UIDs         []string `json:"uids,omitempty" jsonschema:"接口 uid 列表，一次 1–10 个（批量；与 uid/path 二选一，超出直接报错）"`
	Query        string   `json:"query,omitempty" jsonschema:"在示例名/文档名里再筛一遍"`
	IncludeBody  bool     `json:"includeBody,omitempty" jsonschema:"true = 返回响应示例的响应体与请求体全文"`
	MaxBodyBytes int      `json:"maxBodyBytes,omitempty" jsonschema:"响应体裁剪上限（字节），默认 8192"`
	Sections     []string `json:"sections,omitempty" jsonschema:"只回指定分段：overview / docs / bodyFields / respDoc / examples；不传 = 全量"`
	// 说明文档可能很长：这三个参数只作用于 docs 段（与 get_doc 的 startLine/maxLines/outline 同口径）
	DocsStartLine int  `json:"docsStartLine,omitempty" jsonschema:"说明文档起始行（1 基）；与 docsMaxLines 分段读（行号仅供阅读）"`
	DocsMaxLines  int  `json:"docsMaxLines,omitempty" jsonschema:"说明文档本次最多行数（默认 100，最大 500）"`
	DocsOutline   bool `json:"docsOutline,omitempty" jsonschema:"true = 只返回说明文档的 Markdown 标题大纲（带行号）"`
}

func (ts *toolSet) getRequestDetail(ctx context.Context, req *mcp.CallToolRequest, in getDetailIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		single := strings.TrimSpace(in.UID) != "" || strings.TrimSpace(in.Path) != ""
		if single && len(in.UIDs) > 0 {
			return "", nil, withInvalid(errors.New("uid/path 与 uids 只能给一组：单个用 uid 或 path，批量用 uids（1–10 个）"))
		}
		if strings.TrimSpace(in.UID) != "" && strings.TrimSpace(in.Path) != "" {
			return "", nil, withInvalid(errors.New("uid 与 path 只能给一个：知道 uid 就用 uid，只有路径才用 path"))
		}
		if len(in.UIDs) > 0 {
			return ts.batchRequestDetails(getDetailsIn{
				Project: in.Project, UIDs: in.UIDs, Sections: in.Sections, Query: in.Query,
				IncludeBody: in.IncludeBody, MaxBodyBytes: in.MaxBodyBytes,
			})
		}
		if !single {
			return "", nil, withInvalid(errors.New("缺少 uid / path / uids：单个查询传 uid 或 path，批量查询传 uids（1–10 个）"))
		}
		sel, err := NormalizeDetailSections(in.Sections)
		if err != nil {
			return "", nil, err
		}
		d, err := ts.svc.GetRequestDetailAt(in.Project, in.UID, in.Path, DetailOptions{
			IncludeBody: in.IncludeBody, MaxBodyBytes: in.MaxBodyBytes, Query: in.Query, Sections: in.Sections,
		})
		if err != nil {
			return "", nil, err
		}
		view := LineView{Start: in.DocsStartLine, Max: in.DocsMaxLines, Outline: in.DocsOutline}
		return renderDetail(d, sel, view), detailData(d), nil
	})
}

// maxBatchDetails get_request_detail（uids 批量）单次 uid 上限。
const maxBatchDetails = 10

// getDetailsIn get_request_detail 批量模式的入参（对调用方不可见，由 getDetailIn 转换）。
type getDetailsIn struct {
	Project      string   `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UIDs         []string `json:"uids" jsonschema:"接口 uid 列表，一次 1–10 个（超出直接报错，请分批）"`
	Sections     []string `json:"sections,omitempty" jsonschema:"同 get_request_detail：overview / docs / bodyFields / respDoc / examples；不传 = 全量"`
	Query        string   `json:"query,omitempty" jsonschema:"在示例名/文档名里再筛一遍"`
	IncludeBody  bool     `json:"includeBody,omitempty" jsonschema:"true = 返回响应示例的响应体与请求体全文"`
	MaxBodyBytes int      `json:"maxBodyBytes,omitempty" jsonschema:"响应体裁剪上限（字节），默认 8192"`
}

// batchRequestDetails 批量取详情：一次最多 10 个 uid，结果按 uid 分组；单个失败不影响其它。
// 单条与批量共用 get_request_detail 的入参，这里只做批处理（Project 已由调用方解析）。
func (ts *toolSet) batchRequestDetails(in getDetailsIn) (string, any, error) {
	uids := make([]string, 0, len(in.UIDs))
	for _, u := range in.UIDs {
		if u = strings.TrimSpace(u); u != "" {
			uids = append(uids, u)
		}
	}
	if len(uids) == 0 {
		return "", nil, withInvalid(errors.New("uids 不能为空：一次传 1–10 个 uid（uid 从 search_requests / get_project_modules 取）"))
	}
	if len(uids) > maxBatchDetails {
		return "", nil, withLimit(fmt.Errorf("uids 一次最多 %d 个（本次 %d 个）：请分批调用", maxBatchDetails, len(uids)))
	}
	sel, err := NormalizeDetailSections(in.Sections)
	if err != nil {
		return "", nil, err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "批量详情（%d 个请求，项目 %s）：", len(uids), in.Project)
	ok := 0
	items := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		d, derr := ts.svc.GetRequestDetail(in.Project, uid, DetailOptions{
			IncludeBody: in.IncludeBody, MaxBodyBytes: in.MaxBodyBytes, Query: in.Query, Sections: in.Sections,
		})
		sb.WriteString("\n")
		if derr != nil {
			fmt.Fprintf(&sb, "===== uid %s 读取失败：%v =====\n", uid, derr)
			items = append(items, map[string]any{"uid": uid, "error": derr.Error()})
			continue
		}
		ok++
		fmt.Fprintf(&sb, "===== %s =====\n", uid)
		sb.WriteString(renderDetail(d, sel, LineView{}))
		items = append(items, detailData(d))
	}
	fmt.Fprintf(&sb, "\n（成功 %d / %d）", ok, len(uids))
	data := map[string]any{
		"project": in.Project, "total": len(uids), "returned": ok, "items": items,
	}
	return sb.String(), data, nil
}

// searchDocsIn search_docs 的入参。
type searchDocsIn struct {
	Project     string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略则在所有已加载项目里搜"`
	Pattern     string `json:"pattern" jsonschema:"要搜索的文本（按行匹配，大小写不敏感）"`
	ExactMatch  bool   `json:"exactMatch,omitempty" jsonschema:"true = 整词匹配（左右必须是词边界），默认子串匹配"`
	Limit       int    `json:"limit,omitempty" jsonschema:"返回文档数上限，默认 50，最大 200"`
	IncludeHash bool   `json:"includeHash,omitempty" jsonschema:"true = 每条附文件 hash（可回传 update_doc.ifMatch）；默认省读盘"`
}

// searchDocs 在文档条目正文里搜索：返回 uid/路径/命中行号/片段，避免逐篇 get_doc 全文拉回。
func (ts *toolSet) searchDocs(ctx context.Context, req *mcp.CallToolRequest, in searchDocsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		hits, total, err := ts.svc.SearchDocs(in.Project, in.Pattern, in.ExactMatch, in.Limit, in.IncludeHash)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"pattern": in.Pattern, "total": total, "returned": len(hits), "docs": nonNilSlice(hits),
		}
		if len(hits) == 0 {
			return fmt.Sprintf("没有文档包含「%s」。", in.Pattern), data, nil
		}
		var sb strings.Builder
		mode := ""
		if in.ExactMatch {
			mode = "，整词匹配"
		}
		fmt.Fprintf(&sb, "「%s」命中 %d 篇文档（返回 %d 篇，按命中行数排序%s）：\n", in.Pattern, total, len(hits), mode)
		for _, h := range hits {
			fmt.Fprintf(&sb, "\n- %s（路径 %s，uid %s，%d 处命中）\n", h.Name, h.Path, h.UID, h.Count)
			if h.Hash != "" {
				fmt.Fprintf(&sb, "  hash %s（可直接回传 update_doc.ifMatch）\n", h.Hash)
			}
			for _, m := range h.Matches {
				fmt.Fprintf(&sb, "    L%d: %s\n", m.Line, m.Snippet)
			}
			if h.Count > len(h.Matches) {
				fmt.Fprintf(&sb, "    …还有 %d 处未列出（用 get_doc 读全文）\n", h.Count-len(h.Matches))
			}
		}
		return sb.String(), data, nil
	})
}

// searchArgs search_requests 的入参（名字避开 fuzzy.go 的 candidate 类型冲突）。
type searchArgs struct {
	Query       string   `json:"query" jsonschema:"搜索关键词（名称/URL/方法/路径/header/说明/请求体片段）"`
	Project     string   `json:"project,omitempty" jsonschema:"限定项目；省略则在所有已加载项目里搜"`
	Limit       int      `json:"limit,omitempty" jsonschema:"返回条数上限，默认 50，最大 200"`
	ExactMatch  bool     `json:"exactMatch,omitempty" jsonschema:"true = 只认整串出现（不切词），长标识符搜索用它消误报"`
	Fields      []string `json:"fields,omitempty" jsonschema:"搜索范围：name/uri/docs/respDoc/bodyFields/header/body；不传 = 全部"`
	IncludeHash bool     `json:"includeHash,omitempty" jsonschema:"true = 每条附完整 hash（可回传 update_request.ifMatch）；默认省体积"`
}

func (ts *toolSet) searchRequests(ctx context.Context, req *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		fields, err := NormalizeSearchFields(in.Fields)
		if err != nil {
			return "", nil, err
		}
		ms, total, err := ts.svc.SearchRequests(in.Query, in.Project, in.Limit, SearchOptions{
			Exact: in.ExactMatch, Fields: fields,
		})
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"query": in.Query, "total": total, "returned": len(ms), "results": nonNilSlice(ms),
		}
		if len(ms) == 0 {
			return fmt.Sprintf("没有匹配「%s」的请求。", in.Query), data, nil
		}
		var sb strings.Builder
		mode := ""
		if in.ExactMatch {
			mode = "，整串匹配"
		}
		fmt.Fprintf(&sb, "「%s」命中 %d 条（返回 %d 条，按相关度排序%s）：\n", in.Query, total, len(ms), mode)
		for _, m := range ms {
			fmt.Fprintf(&sb, "- [%s] %s %s\n", m.Method, m.Name, m.URL)
			fmt.Fprintf(&sb, "    项目 %s · 路径 %s · uid %s · 分数 %d · 命中 %s",
				m.Project, m.Path, m.UID, m.Score, strings.Join(m.Matched, "/"))
			if in.IncludeHash && m.Hash != "" {
				fmt.Fprintf(&sb, " · hash %s", m.Hash)
			}
			sb.WriteString("\n")
			if m.Snippet != "" {
				for _, line := range strings.Split(m.Snippet, "\n") {
					fmt.Fprintf(&sb, "    ↳ %s\n", line)
				}
			}
		}
		return sb.String(), data, nil
	})
}

// ---- 文档条目（集合里的独立 .md；与 request.docs 区分）----

// listDocsIn list_docs 的入参。
type listDocsIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Query   string `json:"query,omitempty" jsonschema:"按文档名 / 路径模糊过滤；留空返回全部"`
	Limit   int    `json:"limit,omitempty" jsonschema:"返回条数上限（默认 200，最大 1000）"`
	Offset  int    `json:"offset,omitempty" jsonschema:"跳过前 N 条（分页；返回首行会给出总数）"`
	// IncludeHash 默认 true：列表直接带 hash，省掉「列表 → 每篇 get_doc」的 N 次往返。
	IncludeHash *bool `json:"includeHash,omitempty" jsonschema:"默认 true = 每条带 hash（可回传 ifMatch）；大项目可传 false 省读盘"`
}

func (ts *toolSet) listDocs(_ context.Context, req *mcp.CallToolRequest, in listDocsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		includeHash := true
		if in.IncludeHash != nil {
			includeHash = *in.IncludeHash
		}
		docs, total, err := ts.svc.ListDocs(in.Project, in.Query, in.Limit, in.Offset, includeHash)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "total": total, "returned": len(docs), "offset": in.Offset, "docs": nonNilSlice(docs),
		}
		return renderDocs(in.Project, docs, total, in.Offset), data, nil
	})
}

// getDocIn get_doc 的入参（uid/path 单篇 或 uids 批量，二选一）。
type getDocIn struct {
	Project string   `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID     string   `json:"uid,omitempty" jsonschema:"文档 uid（list_docs 返回；与 path/uids/paths 三选一）"`
	Path    string   `json:"path,omitempty" jsonschema:"文档路径（如 docs/guide.md）；与 uid 二选一，批量用 paths"`
	UIDs    []string `json:"uids,omitempty" jsonschema:"文档 uid 列表，一次 1–10（与 uid/path/paths 二选一）"`
	Paths   []string `json:"paths,omitempty" jsonschema:"文档路径列表，一次 1–10 个（批量；与 uids 二选一）"`
	// 长文档分段读取：不传 startLine/maxLines/outline = 全文（旧行为）
	StartLine int  `json:"startLine,omitempty" jsonschema:"起始行（1 基）；与 maxLines 分段读（行号仅供阅读）"`
	MaxLines  int  `json:"maxLines,omitempty" jsonschema:"本次最多返回行数（默认 100，最大 500）"`
	Outline   bool `json:"outline,omitempty" jsonschema:"true = 只返回 Markdown 标题大纲（带行号），先看结构再分段读取"`
}

func (ts *toolSet) getDoc(_ context.Context, req *mcp.CallToolRequest, in getDocIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		single := strings.TrimSpace(in.UID) != "" || strings.TrimSpace(in.Path) != ""
		batch := len(in.UIDs) > 0 || len(in.Paths) > 0
		if single && batch {
			return "", nil, withInvalid(errors.New("单篇（uid/path）与批量（uids/paths）只能给一组"))
		}
		if strings.TrimSpace(in.UID) != "" && strings.TrimSpace(in.Path) != "" {
			return "", nil, withInvalid(errors.New("uid 与 path 只能给一个：知道 uid 就用 uid，只有路径才用 path"))
		}
		if len(in.UIDs) > 0 && len(in.Paths) > 0 {
			return "", nil, withInvalid(errors.New("uids 与 paths 只能给一组：批量按 uid 或按路径"))
		}
		if batch {
			return ts.batchDocs(getDocsIn{
				Project: in.Project, UIDs: in.UIDs, Paths: in.Paths,
				StartLine: in.StartLine, MaxLines: in.MaxLines, Outline: in.Outline,
			})
		}
		if !single {
			return "", nil, withInvalid(errors.New("缺少 uid / path / uids / paths：单篇传 uid 或 path，批量传 uids 或 paths（1–10 篇）"))
		}
		d, err := ts.svc.GetDocAt(in.Project, in.UID, in.Path)
		if err != nil {
			return "", nil, err
		}
		view := LineView{Start: in.StartLine, Max: in.MaxLines, Outline: in.Outline, Suggest: true}
		return renderDocDetail(d, view), docDetailData(in.Project, d, true), nil
	})
}

// saveImageIn save_doc_image 的入参。
type saveImageIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Data    string `json:"data" jsonschema:"图片 base64（可带 data: 前缀）；PNG/JPEG/GIF/WebP，单张 ≤5MB"`
	Name    string `json:"name,omitempty" jsonschema:"图片说明（写进 Markdown 的 alt 文本），默认「图片」"`
}

func (ts *toolSet) saveDocImage(ctx context.Context, req *mcp.CallToolRequest, in saveImageIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		res, err := ts.svc.SaveImage(in.Project, in.Data)
		if err != nil {
			return "", nil, err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = "图片"
		}
		markdown := fmt.Sprintf("![%s](%s)", name, res.URL)
		data := map[string]any{
			"url": res.URL, "sha256": res.SHA256, "size": res.Size,
			"deduped": res.Deduped, "markdown": markdown, "name": name,
		}
		text := fmt.Sprintf("已保存图片：%s\n可直接写进 Markdown：%s", res.URL, markdown)
		if res.Deduped {
			text += "\n（已存在相同图片，命中内容寻址缓存，未重复写盘）"
		}
		return text, data, nil
	})
}

// exampleData 响应示例写操作的结构化输出（与 exampleWriteOutput 对齐）。
func exampleData(project, requestUID string, ex *collection.ResponseExample) map[string]any {
	return map[string]any{
		"project": project, "requestUid": requestUID, "uid": ex.UID,
		"name": ex.Name, "path": ex.Path, "status": ex.Response.Status,
	}
}

// sendData send_request 的结构化输出（与 sendRequestOutput 对齐）。
func sendData(o *SendOutcome) map[string]any {
	return map[string]any{
		"status": o.Status, "url": o.URL, "timeMs": o.TimeMS, "size": o.Size,
		"contentType": o.ContentType, "body": o.Body, "truncated": o.Truncated,
		"originalBytes": o.OriginalBytes, "headerCount": o.HeaderCount,
		"redirects": o.Redirects, "remoteAddr": o.RemoteAddr,
		"saved": o.Saved, "exampleUid": o.ExampleUID, "examplePath": o.ExamplePath,
	}
}

// detailData get_request_detail 的结构化输出（与 requestDetailFullOutput 对齐）。
func detailData(d *RequestDetail) map[string]any {
	ex := make([]map[string]any, 0, len(d.Examples))
	for _, e := range d.Examples {
		ex = append(ex, map[string]any{"uid": e.UID, "name": e.Name, "path": e.Path, "hash": e.Hash, "status": e.Status})
	}
	return map[string]any{
		"project": d.Project, "uid": d.UID, "name": d.Name, "method": d.Method,
		"url": d.URL, "path": d.Path, "hash": d.Hash, "summary": d.Summary,
		"params": len(d.Params), "headers": len(d.Headers), "bodyFields": len(d.Body.Fields),
		"examples": nonNilSlice(ex),
	}
}

// requestDetailData 接口写操作的结构化输出（与 requestDetailOutput 对齐）。
func requestDetailData(project string, r *collection.Request) map[string]any {
	return map[string]any{
		"project": project, "uid": r.UID, "name": r.Name,
		"path": r.Path, "method": r.Method, "url": r.URL,
	}
}

// docDetailData 文档详情/写操作的结构化输出（与 outputSchema 对齐）。
func docDetailData(project string, d *DocDetail, withContent bool) map[string]any {
	m := map[string]any{
		"project": project, "uid": d.UID, "name": d.Name, "path": d.Path, "size": d.Size,
	}
	if d.Hash != "" {
		m["hash"] = d.Hash
	}
	if d.Planned != "" {
		m["planned"] = d.Planned
	}
	if withContent {
		m["content"] = d.Content
	}
	return m
}

// getDocsIn get_docs（批量）的入参。
type getDocsIn struct {
	Project   string   `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UIDs      []string `json:"uids,omitempty" jsonschema:"文档 uid 列表，一次 1–10 个（与 paths 二选一）"`
	Paths     []string `json:"paths,omitempty" jsonschema:"文档路径列表，一次 1–10 个（与 uids 二选一）"`
	StartLine int      `json:"startLine,omitempty" jsonschema:"起始行（1 基）；与 maxLines 分段读（行号仅供阅读）"`
	MaxLines  int      `json:"maxLines,omitempty" jsonschema:"本次最多返回行数（默认 100，最大 500）"`
	Outline   bool     `json:"outline,omitempty" jsonschema:"true = 只返回 Markdown 标题大纲（带行号）"`
}

// batchDocs 批量取文档详情：一次最多 10 篇，结果按条目分组；单个失败不影响其它。
// 支持按 uid 或 path 批量（两者互斥），单篇与批量共用 get_doc 的入参。
func (ts *toolSet) batchDocs(in getDocsIn) (string, any, error) {
	type ref struct{ uid, path string }
	refs := make([]ref, 0, len(in.UIDs)+len(in.Paths))
	seen := make(map[string]bool, len(in.UIDs)+len(in.Paths))
	for _, u := range in.UIDs {
		u = strings.TrimSpace(u)
		if u == "" || seen["u:"+u] {
			continue
		}
		seen["u:"+u] = true
		refs = append(refs, ref{uid: u})
	}
	for _, p := range in.Paths {
		p = strings.TrimSpace(p)
		if p == "" || seen["p:"+p] {
			continue
		}
		seen["p:"+p] = true
		refs = append(refs, ref{path: p})
	}
	if len(refs) == 0 {
		return "", nil, withInvalid(errors.New("uids / paths 不能为空：一次传 1–10 篇（uid 或 path 二选一，从 list_docs 取）"))
	}
	if len(refs) > maxBatchDocs {
		return "", nil, withLimit(fmt.Errorf("批量一次最多 %d 篇（本次 %d 篇）：请分批调用", maxBatchDocs, len(refs)))
	}
	view := LineView{Start: in.StartLine, Max: in.MaxLines, Outline: in.Outline, Suggest: true}
	var sb strings.Builder
	fmt.Fprintf(&sb, "批量文档（%d 篇，项目 %s）：", len(refs), in.Project)
	ok := 0
	items := make([]map[string]any, 0, len(refs))
	for _, r := range refs {
		label := r.uid
		if label == "" {
			label = r.path
		}
		d, derr := ts.svc.GetDocAt(in.Project, r.uid, r.path)
		sb.WriteString("\n")
		if derr != nil {
			fmt.Fprintf(&sb, "===== %s 读取失败：%v =====\n", label, derr)
			items = append(items, map[string]any{"uid": r.uid, "path": r.path, "error": derr.Error()})
			continue
		}
		ok++
		fmt.Fprintf(&sb, "===== %s =====\n", label)
		sb.WriteString(renderDocDetail(d, view))
		items = append(items, docDetailData(in.Project, d, true))
	}
	fmt.Fprintf(&sb, "\n（成功 %d / %d）", ok, len(refs))
	data := map[string]any{
		"project": in.Project, "total": len(refs), "returned": ok, "docs": items,
	}
	return sb.String(), data, nil
}

// maxBatchDocs get_doc（uids 批量）单次 uid 上限（与 get_request_detail 对齐）。
const maxBatchDocs = 10

// ---- 容器级工具 ----

type renameModuleIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 默认工作目录"`
	Path    string `json:"path" jsonschema:"模块相对路径，如 api/user（get_project_modules 返回的 path）"`
	Name    string `json:"name" jsonschema:"新的模块显示名（只改 folder.yml，目录与 uid 不变）"`
}

func (ts *toolSet) renameModule(_ context.Context, req *mcp.CallToolRequest, in renameModuleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.RenameModule(in.Project, in.Path, in.Name); err != nil {
			return "", nil, err
		}
		data := map[string]any{"project": in.Project, "path": in.Path, "name": in.Name}
		return fmt.Sprintf("已重命名模块 %s → %s（显示名变了，路径/目录仍是 %s）", in.Path, in.Name, in.Path), data, nil
	})
}

type deleteModuleIn struct {
	Project    string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 默认工作目录"`
	Path       string `json:"path,omitempty" jsonschema:"模块相对路径，如 api/user（pruneEmpty=true 时可省略）"`
	Recursive  bool   `json:"recursive,omitempty" jsonschema:"true = 整棵删除（进 .trash）；false 只允许空模块"`
	PruneEmpty bool   `json:"pruneEmpty,omitempty" jsonschema:"true = 清理全部空模块（忽略 path）；返回删除数量"`
	DryRun     bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演，不移入 .trash（pruneEmpty 不支持预演）"`
}

func (ts *toolSet) deleteModule(_ context.Context, req *mcp.CallToolRequest, in deleteModuleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		if in.PruneEmpty {
			if in.DryRun {
				return "", nil, withInvalid(errors.New("pruneEmpty 不支持 dryRun：空模块清理没有预演模式"))
			}
			n, err := ts.svc.PruneEmptyModules(in.Project)
			if err != nil {
				return "", nil, err
			}
			data := map[string]any{"project": in.Project, "count": n, "deleted": true, "pruneEmpty": true}
			return fmt.Sprintf("已清理 %d 个空模块目录（只删只有 folder.yml、没有请求/文档/子模块的目录）", n), data, nil
		}
		if strings.TrimSpace(in.Path) == "" {
			return "", nil, withInvalid(errors.New("缺少 path：删除单个模块要给 path，或传 pruneEmpty=true 清理全部空模块"))
		}
		if err := ts.svc.DeleteModule(in.Project, in.Path, in.Recursive, in.DryRun); err != nil {
			return "", nil, err
		}
		mode := "空模块"
		if in.Recursive {
			mode = "整棵（含子项）"
		}
		data := map[string]any{
			"project": in.Project, "path": in.Path, "recursive": in.Recursive,
			"deleted": !in.DryRun, "trash": true, "dryRun": in.DryRun,
		}
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将删除模块 %s（%s），目录将移入集合 .trash", in.Path, mode), data, nil
		}
		return fmt.Sprintf("已删除模块 %s（%s），目录已移入集合的 .trash，可人工找回", in.Path, mode), data, nil
	})
}

type renameProjectIn struct {
	Project string `json:"project" jsonschema:"必填：项目路径、名称或 uid（不能省略，避免改错）"`
	Name    string `json:"name" jsonschema:"新的项目显示名（改 opencollection.yml 的 info.name，目录不变）"`
}

func (ts *toolSet) renameProject(_ context.Context, req *mcp.CallToolRequest, in renameProjectIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.RenameProject(in.Project, in.Name); err != nil {
			return "", nil, err
		}
		data := map[string]any{"project": in.Project, "name": in.Name}
		return fmt.Sprintf("已重命名项目 %s → %s（目录不变）", in.Project, in.Name), data, nil
	})
}

type deleteProjectIn struct {
	Project string `json:"project" jsonschema:"必填：项目路径、名称或 uid（不能省略，避免删错）"`
	Confirm string `json:"confirm" jsonschema:"必填：填项目显示名或目录名，用于二次确认；不一致直接拒绝"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演（校验 confirm 与可写性），不移动目录"`
}

func (ts *toolSet) deleteProject(_ context.Context, req *mcp.CallToolRequest, in deleteProjectIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		dest, err := ts.svc.DeleteProject(in.Project, in.Confirm, in.DryRun)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "trashPath": dest, "deleted": !in.DryRun, "dryRun": in.DryRun,
		}
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将删除项目 %s（confirm=%s），目标回收位 %s", in.Project, in.Confirm, dest), data, nil
		}
		return fmt.Sprintf("已删除项目 %s：目录已移入 %s（可手动移回恢复）。list_projects 已刷新。", in.Project, dest), data, nil
	})
}

// createModuleIn create_module 的入参。
type createModuleIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Name    string `json:"name" jsonschema:"模块名；可写完整路径（api/user，自动拆成 parent + 末段）"`
	Parent  string `json:"parent,omitempty" jsonschema:"父模块路径（须已存在）；留空 = 项目根"`
}

func (ts *toolSet) createModule(ctx context.Context, req *mcp.CallToolRequest, in createModuleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		path, err := ts.svc.CreateModule(in.Project, in.Parent, in.Name)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{"project": in.Project, "path": path, "name": in.Name}
		return fmt.Sprintf("已创建模块 %s（项目 %s）", path, in.Project), data, nil
	})
}

// createProjectIn create_project 的入参。
type createProjectIn struct {
	Name    string `json:"name" jsonschema:"项目名（写进 manifest 的 info.name）"`
	DirName string `json:"dirName,omitempty" jsonschema:"目录名；留空 = 与 name 相同"`
	Root    string `json:"root,omitempty" jsonschema:"项目根目录（必须是授权且可写的工作目录）；留空 = 第一个可写工作目录"`
}

func (ts *toolSet) createProject(ctx context.Context, req *mcp.CallToolRequest, in createProjectIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		p, err := ts.svc.CreateProject(in.Root, in.Name, in.DirName)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{"project": p.Path, "name": p.Name, "path": p.Path, "uid": p.UID}
		return fmt.Sprintf("已创建项目 %s（%s，uid=%s）", p.Path, p.Name, p.UID), data, nil
	})
}

// createRequestArg create_request 的入参（直接复用 service 的结构，json tag 已写好）。
type createRequestArg = CreateRequestInput

func (ts *toolSet) createRequest(ctx context.Context, req *mcp.CallToolRequest, in createRequestArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		r, err := ts.svc.CreateRequest(in)
		if err != nil {
			return "", nil, err
		}
		data := requestDetailData(in.Project, r)
		if in.DryRun {
			data["planned"] = "create"
			return fmt.Sprintf("（dryRun：未落盘）将创建请求 [%s] %s（uid=%s，路径 %s，项目 %s）",
				r.Method, r.Name, r.UID, r.Path, in.Project), data, nil
		}
		return fmt.Sprintf("已创建请求 [%s] %s（uid=%s，路径 %s，项目 %s）", r.Method, r.Name, r.UID, r.Path, in.Project) + fieldHint(r), data, nil
	})
}

// updateRequestsArg update_requests 的入参。
type updateRequestsArg = UpdateRequestsInput

// updateRequests 批量局部更新：单条失败不影响其它条目，失败明细逐条回报。
func (ts *toolSet) updateRequests(_ context.Context, req *mcp.CallToolRequest, in updateRequestsArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		updated, failed, err := ts.svc.UpdateRequests(in)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "requested": len(in.Items), "dryRun": in.DryRun,
			"updated": nonNilSlice(updated), "failed": nonNilSlice(failed),
		}
		var sb strings.Builder
		action := "已更新"
		if in.DryRun {
			action = "（dryRun：未落盘）将更新"
		}
		fmt.Fprintf(&sb, "%s %d/%d 条（项目 %s）", action, len(updated), len(in.Items), in.Project)
		if n := len(in.Items) - len(updated); n > 0 {
			fmt.Fprintf(&sb, "，其中 %d 条未通过（见 failed[]）", n)
		}
		for _, f := range failed {
			fmt.Fprintf(&sb, "\n- %s：%s", f.UID, f.Error)
		}
		return sb.String(), data, nil
	})
}

// importCollectionArg import_collection 的入参。
type importCollectionArg = ImportCollectionInput

func (ts *toolSet) importCollection(ctx context.Context, req *mcp.CallToolRequest, in importCollectionArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		sum, err := ts.svc.ImportCollection(in)
		if err != nil {
			return "", nil, err
		}
		format := strings.ToLower(strings.TrimSpace(in.Format))
		data := map[string]any{
			"project":  in.Project,
			"format":   format,
			"parent":   strings.TrimSpace(in.Parent),
			"imported": sum.Imported,
			"skipped":  sum.Skipped,
			"failures": nonNilSlice(sum.Failures),
			"dryRun":   in.DryRun,
		}
		return renderImportSummary(in.Project, format, in.Parent, sum, in.DryRun), data, nil
	})
}

// updateArg update_request 的入参。
type updateArg = UpdateRequestInput

func (ts *toolSet) updateRequest(ctx context.Context, req *mcp.CallToolRequest, in updateArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		// 动说明文档前先留旧文本：成功响应要给「改动行 ±1」的落盘确认（取不到旧文本则回落前 5 行）
		oldDocs := ""
		touched := in.Docs != nil || len(in.Replaces) > 0
		if touched {
			if s, derr := ts.svc.DocsOf(in.Project, in.UID); derr == nil {
				oldDocs = s
			}
		}
		r, err := ts.svc.UpdateRequest(in)
		if err != nil {
			return "", nil, err
		}
		data := requestDetailData(in.Project, r)
		out := fmt.Sprintf("已更新 [%s] %s（uid=%s，路径 %s）", r.Method, r.Name, r.UID, r.Path)
		if in.DryRun {
			data["planned"] = "update"
			out = fmt.Sprintf("（dryRun：未落盘）将更新 [%s] %s（uid=%s，路径 %s）", r.Method, r.Name, r.UID, r.Path)
		}
		if touched {
			if kind, body := changePreview(oldDocs, r.Docs); body != "" {
				out += "\n落盘确认（说明文档" + kind + "）：\n" + body
			}
		}
		if !in.DryRun {
			out += fieldHint(r)
		}
		return out, data, nil
	})
}

// deleteIn delete_request 的入参。
type deleteIn struct {
	Project     string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID         string `json:"uid,omitempty" jsonschema:"要移除的接口 uid；与 path 二选一"`
	Path        string `json:"path,omitempty" jsonschema:"请求文件路径（相对集合根，如 api/users.yml）；与 uid 二选一"`
	DryRun      bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演（返回将进 .trash 的路径），不落盘"`
	IfMatch     string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 search_requests 里该请求的 hash；不传 = 最后写者赢"`
	ConfirmName string `json:"confirmName,omitempty" jsonschema:"二次确认：填请求名称；与磁盘上的 name 不一致直接拒绝（防误删）"`
}

func (ts *toolSet) deleteRequest(ctx context.Context, req *mcp.CallToolRequest, in deleteIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		// 删除后文件已进 .trash：先读一次拿身份信息，结构化输出才拿得到 path/name
		r, err := ts.svc.ReadRequestAt(in.Project, in.UID, in.Path)
		if err != nil {
			return "", nil, err
		}
		if in.ConfirmName != "" && strings.TrimSpace(in.ConfirmName) != r.Name {
			return "", nil, coded(CodeConflict, fmt.Errorf("confirmName 与请求名不一致（当前 name=%q），未删除", r.Name))
		}
		if !in.DryRun {
			if err := ts.svc.DeleteRequestAt(in.Project, in.UID, in.Path, in.IfMatch); err != nil {
				return "", nil, err
			}
		}
		data := requestDetailData(in.Project, r)
		data["deleted"] = !in.DryRun
		data["trash"] = true
		data["dryRun"] = in.DryRun
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将把请求 %s（%s）移入 .trash", in.UID, r.Path), data, nil
		}
		return fmt.Sprintf("已移除请求 %s（文件已移入 .trash，可人工找回）", in.UID), data, nil
	})
}

// createDocArg create_doc 的入参（复用 service 结构，json/描述已写好）。
type deleteRequestsItem struct {
	UID     string `json:"uid,omitempty" jsonschema:"要删除的请求 uid（search_requests 的 results[].uid）；与 path 二选一"`
	Path    string `json:"path,omitempty" jsonschema:"请求文件路径（相对集合根，如 api/users.yml）；与 uid 二选一"`
	IfMatch string `json:"ifMatch" jsonschema:"必填：该请求的 hash（search_requests 的 results[].hash，可传 12 位前缀）"`
	// 逐条二次确认：同名请求较多时用它防「删错那一个」。
	ConfirmName string `json:"confirmName,omitempty" jsonschema:"可选：填该条请求的显示名，不一致则这一条拒绝（不影响其它条）"`
}

type deleteRequestsIn struct {
	Project string               `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 默认工作目录"`
	Confirm string               `json:"confirm,omitempty" jsonschema:"二次确认：填项目显示名/目录名；不一致整批拒绝"`
	Items   []deleteRequestsItem `json:"items" jsonschema:"1–10 条；每条必须给 uid 或 path + ifMatch"`
	DryRun  bool                 `json:"dryRun,omitempty" jsonschema:"true = 只校验并列出将删除的条目，不落盘"`
}

func (ts *toolSet) deleteRequests(ctx context.Context, req *mcp.CallToolRequest, in deleteRequestsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		items := make([]DeleteRequestItem, 0, len(in.Items))
		for _, it := range in.Items {
			items = append(items, DeleteRequestItem{UID: it.UID, Path: it.Path, IfMatch: it.IfMatch, ConfirmName: it.ConfirmName})
		}
		deleted, failed, err := ts.svc.DeleteRequests(in.Project, in.Confirm, items, in.DryRun)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "requested": len(items), "dryRun": in.DryRun,
			"deleted": nonNilSlice(deleted), "failed": nonNilSlice(failed),
		}
		var sb strings.Builder
		action := "已删除"
		if in.DryRun {
			action = "（dryRun：未落盘）将删除"
		}
		fmt.Fprintf(&sb, "%s %d/%d 条（项目 %s）", action, len(deleted), len(items), in.Project)
		if n := len(items) - len(deleted); n > 0 {
			fmt.Fprintf(&sb, "，其中 %d 条未通过（见 failed[]）", n)
		}
		for _, f := range failed {
			fmt.Fprintf(&sb, "\n- %s：%s", f.UID, f.Error)
		}
		return sb.String(), data, nil
	})
}

type createDocArg = CreateDocInput

func (ts *toolSet) createDoc(_ context.Context, req *mcp.CallToolRequest, in createDocArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		d, err := ts.svc.CreateDoc(in)
		if err != nil {
			return "", nil, err
		}
		data := docDetailData(in.Project, d, false)
		if in.DryRun {
			action := "创建"
			if d.Planned == "overwrite" {
				action = "覆盖（复用已有 uid/path）"
			}
			out := fmt.Sprintf("（dryRun：未落盘）将%s %s（uid=%s，项目 %s）", action, d.Path, d.UID, in.Project)
			if prev := previewFirstLines(in.Content, 3); prev != "" {
				out += "\n正文预览（前 3 行）：\n" + prev
			}
			return out, data, nil
		}
		out := fmt.Sprintf("已创建文档 %s（uid=%s，路径 %s，项目 %s，%d 字节）",
			d.Name, d.UID, d.Path, in.Project, d.Size)
		if strings.EqualFold(strings.TrimSpace(in.OnConflict), "overwrite") {
			out = "已复用同路径文档并覆盖正文（onConflict=overwrite）\n" + out
		}
		return out, data, nil
	})
}

// updateDocArg update_doc 的入参。
type updateDocArg = UpdateDocInput

func (ts *toolSet) updateDoc(_ context.Context, req *mcp.CallToolRequest, in updateDocArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		// 动正文前先留旧文本：成功响应要给「改动行 ±1」的落盘确认
		oldContent := ""
		touched := in.Content != nil || len(in.Replaces) > 0 || in.Append != nil || in.InsertAfter != nil
		if touched {
			if d0, derr := ts.svc.GetDocAt(in.Project, in.UID, in.Path); derr == nil {
				oldContent = d0.Content
			}
		}
		d, err := ts.svc.UpdateDoc(in)
		if err != nil {
			return "", nil, err
		}
		data := docDetailData(in.Project, d, false)
		if in.DryRun {
			out := fmt.Sprintf("（dryRun：未落盘）将更新文档 %s（uid=%s，路径 %s）", d.Name, d.UID, d.Path)
			if kind, body := changePreview(d.PrevContent, d.Content); body != "" {
				out += "\n改动预览（正文" + kind + "）：\n" + body
			}
			return out, data, nil
		}
		out := fmt.Sprintf("已更新文档 %s（uid=%s，路径 %s，%d 字节）", d.Name, d.UID, d.Path, d.Size)
		if touched {
			if kind, body := changePreview(oldContent, d.Content); body != "" {
				out += "\n落盘确认（正文" + kind + "）：\n" + body
			}
		}
		return out, data, nil
	})
}

// deleteDocIn delete_doc 的入参（比 deleteIn 多一句说明：这条是文档不是接口）。
type deleteDocIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID     string `json:"uid,omitempty" jsonschema:"要移除的文档 uid（list_docs 返回）；与 path 二选一"`
	Path    string `json:"path,omitempty" jsonschema:"文档路径（相对集合根，如 docs/guide.md）；与 uid 二选一"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只返回将移入 .trash 的路径，不实际删除"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_docs/get_doc 里该文档的 hash；不传 = 最后写者赢"`
}

func (ts *toolSet) deleteDoc(_ context.Context, req *mcp.CallToolRequest, in deleteDocIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		// 先读一次拿 path（删除后文件已进 .trash，拿不到原路径）
		d, err := ts.svc.GetDocAt(in.Project, in.UID, in.Path)
		if err != nil {
			return "", nil, err
		}
		if !hashPrefixMatch(d.Hash, in.IfMatch) {
			return "", nil, coded(CodeConflict, fmt.Errorf("文档 %s 已被他人修改（当前 hash %s），请重新读取后再删", d.UID, d.Hash))
		}
		data := map[string]any{
			"uid": d.UID, "path": d.Path, "deleted": !in.DryRun, "trash": true, "dryRun": in.DryRun,
		}
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将把 %s（uid=%s）移入集合 .trash，可人工找回", d.Path, d.UID), data, nil
		}
		if err := ts.svc.DeleteDocAt(in.Project, in.UID, in.Path); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("已移除文档 %s（文件已移入 .trash，可人工找回）", d.UID), data, nil
	})
}

// ---- 响应示例工具的 handler ----

// saveResponseExampleArg save_response_example 的入参（复用 service 结构）。
type saveResponseExampleArg = SaveResponseExampleInput

func (ts *toolSet) saveResponseExample(_ context.Context, req *mcp.CallToolRequest, in saveResponseExampleArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		ex, err := ts.svc.SaveResponseExample(in)
		if err != nil {
			return "", nil, err
		}
		data := exampleData(in.Project, in.UID, ex)
		if in.DryRun {
			data["planned"] = "create"
			return fmt.Sprintf("（dryRun：未落盘）将保存响应示例 %s（路径 %s，状态码 %d，%d 字节）",
				ex.Name, ex.Path, ex.Response.Status, len(ex.Response.Body)), data, nil
		}
		return fmt.Sprintf("已保存响应示例 %s（uid=%s，路径 %s，状态码 %d，%d 字节，项目 %s）",
			ex.Name, ex.UID, ex.Path, ex.Response.Status, len(ex.Response.Body), in.Project), data, nil
	})
}

// updateResponseExampleArg update_response_example 的入参。
type updateResponseExampleArg = UpdateResponseExampleInput

func (ts *toolSet) updateResponseExample(_ context.Context, req *mcp.CallToolRequest, in updateResponseExampleArg) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		ex, err := ts.svc.UpdateResponseExample(in)
		if err != nil {
			return "", nil, err
		}
		data := exampleData(in.Project, in.UID, ex)
		if in.DryRun {
			data["planned"] = "update"
			return fmt.Sprintf("（dryRun：未落盘）将更新响应示例 %s（路径 %s，状态码 %d，%d 字节）",
				ex.Name, ex.Path, ex.Response.Status, len(ex.Response.Body)), data, nil
		}
		return fmt.Sprintf("已更新响应示例 %s（uid=%s，路径 %s，状态码 %d，%d 字节）",
			ex.Name, ex.UID, ex.Path, ex.Response.Status, len(ex.Response.Body)), data, nil
	})
}

// deleteResponseExampleIn delete_response_example 的入参。
type deleteResponseExampleIn struct {
	Project    string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID        string `json:"uid" jsonschema:"请求 uid（示例所属请求）"`
	ExampleUID string `json:"exampleUid" jsonschema:"要删除的响应示例 uid（get_request_detail 返回的 examples[].uid）"`
	DryRun     bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演，不移入 .trash"`
	IfMatch    string `json:"ifMatch,omitempty" jsonschema:"并发保护：传示例文件当前 hash（可选）；不传 = 最后写者赢"`
}

func (ts *toolSet) deleteResponseExample(_ context.Context, req *mcp.CallToolRequest, in deleteResponseExampleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		if !in.DryRun {
			if err := ts.svc.DeleteResponseExample(in.Project, in.UID, in.ExampleUID, in.IfMatch); err != nil {
				return "", nil, err
			}
		}
		data := map[string]any{
			"project": in.Project, "requestUid": in.UID, "uid": in.ExampleUID,
			"deleted": !in.DryRun, "trash": true, "dryRun": in.DryRun,
		}
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将删除响应示例 %s（请求 %s）", in.ExampleUID, in.UID), data, nil
		}
		return fmt.Sprintf("已删除响应示例 %s（文件已移入 .trash，可人工找回）", in.ExampleUID), data, nil
	})
}

// ---- 环境变量工具的 handler ----

// listEnvsIn list_envs 的入参。
type listEnvsIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Query   string `json:"query,omitempty" jsonschema:"按环境名 / 变量名模糊过滤；留空返回全部"`
	Limit   int    `json:"limit,omitempty" jsonschema:"返回环境条数上限（默认 200，最大 1000）"`
	Offset  int    `json:"offset,omitempty" jsonschema:"跳过前 N 个环境（分页；首行给总数）"`
}

func (ts *toolSet) listEnvs(ctx context.Context, req *mcp.CallToolRequest, in listEnvsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		envs, total, err := ts.svc.ListEnvsPage(in.Project, in.Query, in.Limit, in.Offset)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "total": total, "returned": len(envs), "offset": in.Offset, "envs": nonNilSlice(envs),
		}
		return renderEnvs(in.Project, envs, total, in.Offset), data, nil
	})
}

// renameEnvIn rename_env 的入参。
type renameEnvIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Name    string `json:"name" jsonschema:"当前环境名"`
	NewName string `json:"newName" jsonschema:"新环境名（字母/数字/-/_；不能重名或只改大小写）"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_envs 里该环境的 hash；不传 = 最后写者赢"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演，不落盘"`
}

func (ts *toolSet) renameEnv(ctx context.Context, req *mcp.CallToolRequest, in renameEnvIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		env, err := ts.svc.RenameEnv(in.Project, in.Name, in.NewName, in.IfMatch)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{"project": in.Project, "oldName": in.Name, "name": env.Name, "hash": env.Hash, "varsCount": len(env.Vars)}
		return fmt.Sprintf("已把环境 %s 改名为 %s（%d 个变量已搬过去，旧文件在 .trash 可找回，hash %s…）",
			in.Name, env.Name, len(env.Vars), env.Hash), data, nil
	})
}

// deleteEnvIn delete_env 的入参。
type deleteEnvIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Name    string `json:"name" jsonschema:"要删除的环境名"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_envs 里该环境的 hash；不传 = 最后写者赢"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演，不落盘"`
}

func (ts *toolSet) deleteEnv(ctx context.Context, req *mcp.CallToolRequest, in deleteEnvIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.DeleteEnv(in.Project, in.Name, in.IfMatch, in.DryRun); err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "name": in.Name,
			"deleted": !in.DryRun, "trash": true, "dryRun": in.DryRun,
		}
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将删除环境 %s", in.Name), data, nil
		}
		return fmt.Sprintf("已删除环境 %s（文件已移入 .trash，可人工找回）", in.Name), data, nil
	})
}

func (ts *toolSet) createEnv(ctx context.Context, req *mcp.CallToolRequest, in CreateEnvInput) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		env, err := ts.svc.CreateEnv(in)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{"project": in.Project, "name": env.Name, "hash": env.Hash, "varsCount": len(env.Vars)}
		return fmt.Sprintf("已创建环境 %s（项目 %s，初始变量 %d 个）%s",
			env.Name, in.Project, len(env.Vars), envTip(len(env.Vars))), data, nil
	})
}

func (ts *toolSet) setEnvVar(ctx context.Context, req *mcp.CallToolRequest, in SetEnvVarInput) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		v, err := ts.svc.SetEnvVar(in)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"project": in.Project, "env": in.Env, "name": v.Name, "value": v.Value,
			"secret": v.Secret, "enabled": v.Enabled, "keptSecret": v.KeptSecret, "hash": v.Hash,
		}
		extra := ""
		switch {
		case v.KeptSecret:
			extra = "（传回的是掩码，密钥保持原样未改动）"
		case v.Secret:
			extra = "（敏感值已落盘进 .secrets.yml，对外只回掩码）"
		}
		if !v.Enabled {
			extra += "（已停用：不参与 {{name}} 解析）"
		}
		return fmt.Sprintf("已写入变量 %s.%s = %s（secret=%v，enabled=%v）%s（环境 hash %s…）",
			in.Env, v.Name, v.Value, v.Secret, v.Enabled, extra, v.Hash), data, nil
	})

}

// deleteEnvVarIn delete_env_var 的入参。
type deleteEnvVarIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Env     string `json:"env" jsonschema:"所属环境名"`
	Name    string `json:"name" jsonschema:"要删除的变量名"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_envs 里该环境的 hash；不传 = 最后写者赢"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只预演，不落盘"`
}

func (ts *toolSet) deleteEnvVar(ctx context.Context, req *mcp.CallToolRequest, in deleteEnvVarIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		if !in.DryRun {
			if err := ts.svc.DeleteEnvVar(in.Project, in.Env, in.Name, in.IfMatch); err != nil {
				return "", nil, err
			}
		}
		data := map[string]any{
			"project": in.Project, "env": in.Env, "name": in.Name,
			"deleted": !in.DryRun, "dryRun": in.DryRun,
		}
		if in.DryRun {
			return fmt.Sprintf("（dryRun：未落盘）将从环境 %s 删除变量 %s", in.Env, in.Name), data, nil
		}
		return fmt.Sprintf("已从环境 %s 删除变量 %s", in.Env, in.Name), data, nil
	})
}

// envTip 变量为空时提醒 AI 接下来该做什么（空环境在客户端里就是个空壳）。
func envTip(n int) string {
	if n > 0 {
		return ""
	}
	return "；该环境目前没有任何变量，用 set_env_var 添加（请求里以 {{变量名}} 引用）"
}

// wrap 把 handler 的结果与错误转成 MCP 工具结果：
// 业务错误用 IsError=true 返回（AI 能读到并据此纠正），而不是协议级 error。
func (ts *toolSet) wrap(fn func() (string, error)) (*mcp.CallToolResult, any, error) {
	text, err := fn()
	if err != nil {
		// 未分类错误统一补 [INTERNAL]：调用方可按 [CODE] 前缀稳定分流，不必匹配中文散文
		err = coded(CodeOf(err), err)
		return &mcp.CallToolResult{
			IsError:           true,
			Content:           []mcp.Content{&mcp.TextContent{Text: "失败: " + err.Error()}},
			StructuredContent: errorData(err),
		}, nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

// nonNilSlice 让空结果序列化为 [] 而不是 null（outputSchema 声明的是 array）。
func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// errorData 错误态的结构化体：{error:{code,message}}，让调用方不必解析文本前缀。
func errorData(err error) map[string]any {
	code := CodeOf(err)
	msg := strings.TrimPrefix(err.Error(), "["+code+"] ")
	return map[string]any{"error": map[string]any{"code": code, "message": msg}}
}

// wrapData 与 wrap 相同，但额外返回 structuredContent（仅列表类工具使用）。
// 文本输出保持原样：旧客户端 / 只读人看文本，新客户端可直接解析结构。
func (ts *toolSet) wrapData(fn func() (string, any, error)) (*mcp.CallToolResult, any, error) {
	text, data, err := fn()
	if err != nil {
		err = coded(CodeOf(err), err)
		return &mcp.CallToolResult{
			IsError:           true,
			Content:           []mcp.Content{&mcp.TextContent{Text: "失败: " + err.Error()}},
			StructuredContent: errorData(err),
		}, nil, nil
	}
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	if data != nil {
		res.StructuredContent = data
	}
	return res, nil, nil
}
