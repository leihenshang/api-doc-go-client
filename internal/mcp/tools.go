package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

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
	readOnly bool // 只读模式：不注册写工具，send_request 强制不落盘

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
	if err := registerToolsWithOpts(server, svc, readOnly, toolGuardOpts{}); err != nil {
		panic(err)
	}
}

func registerToolsWithOpts(server *mcp.Server, svc *Service, readOnly bool, opts toolGuardOpts) error {
	if err := ValidateMetas(); err != nil {
		return err
	}
	// SDK 在进入 handler 前会按 inputSchema 校验参数；那类错误没有我们的错误码，
	// 这里统一补 [INVALID]（其余未分类补 [INTERNAL]），保证「失败: [CODE] ...」恒成立。
	server.AddReceivingMiddleware(toolErrorCodeMiddleware())
	ts := &toolSet{
		svc: svc, readOnly: readOnly,
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
	return ts.assertRegistered(server, readOnly)
}

// addTool 按 meta.go 中的名称取描述注册一个工具；名字不在元数据表里即装配缺陷。
// guard 在这一层包住所有 handler：panic recover + 风险工具交互授权，24 个业务 handler 零改动。
// 必须在内部构造**字面签名**的闭包交给 mcp.AddTool —— 若用具名 coreHandler[In] 类型，
// SDK 的 ToolHandlerFor[In,Out] 泛型推断无法把具名类型与类型参数合一。
func addTool[In any](ts *toolSet, server *mcp.Server, name string,
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error)) {
	meta, ok := MetaOf(name)
	if !ok {
		panic(fmt.Sprintf("mcp: 工具 %q 缺少 meta.go 元数据", name))
	}
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
		Project:     project,
		ArgsSummary: argsSummary,
		ResultOK:    kind == mcpaudit.KindOK,
		ResultKind:  string(kind),
		DurationMs:  time.Since(start).Milliseconds(),
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
	want := NamesForMode(readOnly)
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
			code := CodeInternal
			if strings.Contains(text.Text, "validating") || strings.Contains(text.Text, "missing properties") {
				code = CodeInvalid
			} else if strings.Contains(text.Text, "unknown tool") || strings.Contains(text.Text, "not found") {
				code = CodeNotFound
			}
			out := strings.TrimPrefix(text.Text, "失败: ")
			call.Content = []mcp.Content{&mcp.TextContent{Text: "失败: " + coded(code, errors.New(out)).Error()}}
			return res, err
		}
	}
}

func (ts *toolSet) registerReadTools(server *mcp.Server) {
	addTool(ts, server, "list_workspaces", ts.listWorkspaces)
	addTool(ts, server, "use_workspace", ts.useWorkspace)
	addTool(ts, server, "list_projects", ts.listProjects)
	addTool(ts, server, "get_project_modules", ts.getProjectModules)
	addTool(ts, server, "get_request_detail", ts.getRequestDetail)
	addTool(ts, server, "get_request_details", ts.getRequestDetails)
	addTool(ts, server, "search_requests", ts.searchRequests)
	addTool(ts, server, "search_docs", ts.searchDocs)
	addTool(ts, server, "list_envs", ts.listEnvs)
	addTool(ts, server, "get_doc_template", ts.getDocTemplate)
	// 文档条目：只读两个（列表不含正文，取全文走 get_doc）
	addTool(ts, server, "list_docs", ts.listDocs)
	addTool(ts, server, "get_doc", ts.getDoc)
	// 批量读取：1–10 个 uid，按 uid 分组（文档侧对齐 get_request_details）
	addTool(ts, server, "get_docs", ts.getDocs)
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
	Workspace string `json:"workspace" jsonschema:"工作目录 path（list_workspaces 返回）或项目标识（名称/路径/uid）；必须在客户端授权的白名单内"`
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
	addTool(ts, server, "create_module", ts.createModule)
	addTool(ts, server, "create_project", ts.createProject)
	addTool(ts, server, "update_request", ts.updateRequest)
	addTool(ts, server, "delete_request", ts.deleteRequest)
	// 文档条目的写操作：新建 / 局部更新（正文·改名·移动）/ 删除（进 .trash）
	addTool(ts, server, "create_doc", ts.createDoc)
	addTool(ts, server, "update_doc", ts.updateDoc)
	addTool(ts, server, "delete_doc", ts.deleteDoc)
	// 图片：存到项目 assets/，返回可写进正文的 URL（Markdown 编辑器同款）
	addTool(ts, server, "save_doc_image", ts.saveDocImage)
	// 容器级：模块/项目的改名与删除（删除进 .trash）+ 空模块清理
	addTool(ts, server, "rename_module", ts.renameModule)
	addTool(ts, server, "delete_module", ts.deleteModule)
	addTool(ts, server, "rename_project", ts.renameProject)
	addTool(ts, server, "delete_project", ts.deleteProject)
	addTool(ts, server, "prune_empty_modules", ts.pruneEmptyModules)
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
			return ts.wrap(func() (string, error) {
				in.Project = ts.proj(req, in.Project)
				out, err := ts.svc.SendRequest(in)
				if err != nil {
					return "", err
				}
				return renderSend(out), nil
			})
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: meta.Name, Description: desc}, handler)
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
		data := map[string]any{"projects": items, "skipped": skipped}
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
	Project         string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Query           string `json:"query,omitempty" jsonschema:"对模块名与请求名/方法/路径做模糊过滤"`
	IncludeRequests bool   `json:"includeRequests,omitempty" jsonschema:"true = 附带每个模块下的请求清单"`
	OnlyMatched     *bool  `json:"onlyMatched,omitempty" jsonschema:"有 query 时默认只返回有命中的模块（模块名命中，或其下有命中请求）；传 false 可看全部模块（汇总行始终给出模块总数）"`
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
			"total": res.Total, "modules": res.Modules,
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

// getDetailIn get_request_detail 的入参。
type getDetailIn struct {
	Project      string   `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID          string   `json:"uid" jsonschema:"接口 uid（用 search_requests 或 get_project_modules 获取）"`
	Query        string   `json:"query,omitempty" jsonschema:"在示例名/文档名里再筛一遍"`
	IncludeBody  bool     `json:"includeBody,omitempty" jsonschema:"true = 返回响应示例的响应体与请求体全文"`
	MaxBodyBytes int      `json:"maxBodyBytes,omitempty" jsonschema:"响应体裁剪上限（字节），默认 8192"`
	Sections     []string `json:"sections,omitempty" jsonschema:"只返回指定分段以控制体积：overview（请求本体/概述/参数/头/体）/ docs（说明文档与相关文档）/ bodyFields（请求体字段表）/ respDoc（响应字段与样例）/ examples（已保存响应示例）。不传 = 全量（旧行为）"`
	// 说明文档可能很长：这三个参数只作用于 docs 段（与 get_doc 的 startLine/maxLines/outline 同口径）
	DocsStartLine int  `json:"docsStartLine,omitempty" jsonschema:"说明文档从第几行开始（1 基）；与 docsMaxLines 一起分段读取（行号仅供阅读，写回时不要带）"`
	DocsMaxLines  int  `json:"docsMaxLines,omitempty" jsonschema:"说明文档本次最多行数（默认 100，最大 500）"`
	DocsOutline   bool `json:"docsOutline,omitempty" jsonschema:"true = 只返回说明文档的 Markdown 标题大纲（带行号）"`
}

func (ts *toolSet) getRequestDetail(ctx context.Context, req *mcp.CallToolRequest, in getDetailIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		sel, err := NormalizeDetailSections(in.Sections)
		if err != nil {
			return "", err
		}
		d, err := ts.svc.GetRequestDetail(in.Project, in.UID, DetailOptions{
			IncludeBody: in.IncludeBody, MaxBodyBytes: in.MaxBodyBytes, Query: in.Query, Sections: in.Sections,
		})
		if err != nil {
			return "", err
		}
		view := LineView{Start: in.DocsStartLine, Max: in.DocsMaxLines, Outline: in.DocsOutline}
		return renderDetail(d, sel, view), nil
	})
}

// maxBatchDetails get_request_details 单次 uid 上限。
const maxBatchDetails = 10

// getDetailsIn get_request_details（批量）的入参。
type getDetailsIn struct {
	Project      string   `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UIDs         []string `json:"uids" jsonschema:"接口 uid 列表，一次 1–10 个（超出直接报错，请分批）"`
	Sections     []string `json:"sections,omitempty" jsonschema:"同 get_request_detail：overview / docs / bodyFields / respDoc / examples；不传 = 全量"`
	Query        string   `json:"query,omitempty" jsonschema:"在示例名/文档名里再筛一遍"`
	IncludeBody  bool     `json:"includeBody,omitempty" jsonschema:"true = 返回响应示例的响应体与请求体全文"`
	MaxBodyBytes int      `json:"maxBodyBytes,omitempty" jsonschema:"响应体裁剪上限（字节），默认 8192"`
}

// getRequestDetails 批量取详情：一次最多 10 个 uid，结果按 uid 分组；单个失败不影响其它。
func (ts *toolSet) getRequestDetails(ctx context.Context, req *mcp.CallToolRequest, in getDetailsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		uids := make([]string, 0, len(in.UIDs))
		for _, u := range in.UIDs {
			if u = strings.TrimSpace(u); u != "" {
				uids = append(uids, u)
			}
		}
		if len(uids) == 0 {
			return "", withInvalid(errors.New("uids 不能为空：一次传 1–10 个 uid（uid 从 search_requests / get_project_modules 取）"))
		}
		if len(uids) > maxBatchDetails {
			return "", withLimit(fmt.Errorf("uids 一次最多 %d 个（本次 %d 个）：请分批调用", maxBatchDetails, len(uids)))
		}
		sel, err := NormalizeDetailSections(in.Sections)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "批量详情（%d 个请求，项目 %s）：", len(uids), in.Project)
		ok := 0
		for _, uid := range uids {
			d, derr := ts.svc.GetRequestDetail(in.Project, uid, DetailOptions{
				IncludeBody: in.IncludeBody, MaxBodyBytes: in.MaxBodyBytes, Query: in.Query, Sections: in.Sections,
			})
			sb.WriteString("\n")
			if derr != nil {
				fmt.Fprintf(&sb, "===== uid %s 读取失败：%v =====\n", uid, derr)
				continue
			}
			ok++
			fmt.Fprintf(&sb, "===== %s =====\n", uid)
			sb.WriteString(renderDetail(d, sel, LineView{}))
		}
		fmt.Fprintf(&sb, "\n（成功 %d / %d）", ok, len(uids))
		return sb.String(), nil
	})
}

// searchDocsIn search_docs 的入参。
type searchDocsIn struct {
	Project    string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略则在所有已加载项目里搜"`
	Pattern    string `json:"pattern" jsonschema:"要搜索的文本（按行匹配，大小写不敏感）"`
	ExactMatch bool   `json:"exactMatch,omitempty" jsonschema:"true = 整词匹配（左右必须是词边界），默认子串匹配"`
	Limit      int    `json:"limit,omitempty" jsonschema:"返回文档数上限，默认 50，最大 200"`
}

// searchDocs 在文档条目正文里搜索：返回 uid/路径/命中行号/片段，避免逐篇 get_doc 全文拉回。
func (ts *toolSet) searchDocs(ctx context.Context, req *mcp.CallToolRequest, in searchDocsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrapData(func() (string, any, error) {
		in.Project = ts.proj(req, in.Project)
		hits, total, err := ts.svc.SearchDocs(in.Project, in.Pattern, in.ExactMatch, in.Limit)
		if err != nil {
			return "", nil, err
		}
		data := map[string]any{
			"pattern": in.Pattern, "total": total, "returned": len(hits), "docs": hits,
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
	ExactMatch  bool     `json:"exactMatch,omitempty" jsonschema:"true = 只认整串出现（不切词、不做子序列兜底），长标识符搜索用它消掉误报"`
	Fields      []string `json:"fields,omitempty" jsonschema:"限定搜索范围：name / uri（方法+URL+路径）/ docs / respDoc / bodyFields / header / body；不传 = 全部"`
	IncludeHash bool     `json:"includeHash,omitempty" jsonschema:"true = 每条结果附完整 hash（可直接回传 update_request.ifMatch）；默认 false 省体积"`
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
			"query": in.Query, "total": total, "returned": len(ms), "results": ms,
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

// getDocTemplateIn get_doc_template 无入参。
type getDocTemplateIn struct{}

func (ts *toolSet) getDocTemplate(_ context.Context, _ *mcp.CallToolRequest, _ getDocTemplateIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		return ts.svc.DocTemplate(), nil
	})
}

// ---- 文档条目（集合里的独立 .md；与 request.docs 区分）----

// listDocsIn list_docs 的入参。
type listDocsIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Query   string `json:"query,omitempty" jsonschema:"按文档名 / 路径模糊过滤；留空返回全部"`
	Limit   int    `json:"limit,omitempty" jsonschema:"返回条数上限（默认 200，最大 1000）"`
	Offset  int    `json:"offset,omitempty" jsonschema:"跳过前 N 条（分页；返回首行会给出总数）"`
	// IncludeHash 默认 true：列表直接带 hash，省掉「列表 → 每篇 get_doc」的 N 次往返。
	IncludeHash *bool `json:"includeHash,omitempty" jsonschema:"默认 true：每条带文件 hash（可直接回传 update_doc.ifMatch）；大项目可传 false 省一次读盘"`
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
			"project": in.Project, "total": total, "returned": len(docs), "offset": in.Offset, "docs": docs,
		}
		return renderDocs(in.Project, docs, total, in.Offset), data, nil
	})
}

// getDocIn get_doc 的入参。
type getDocIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID     string `json:"uid" jsonschema:"文档 uid（list_docs 返回）"`
	// 长文档分段读取：不传 startLine/maxLines/outline = 全文（旧行为）
	StartLine int  `json:"startLine,omitempty" jsonschema:"从第几行开始（1 基）；与 maxLines 一起分段读取（行号仅供阅读，写回时不要带）"`
	MaxLines  int  `json:"maxLines,omitempty" jsonschema:"本次最多返回行数（默认 100，最大 500）"`
	Outline   bool `json:"outline,omitempty" jsonschema:"true = 只返回 Markdown 标题大纲（带行号），先看结构再分段读取"`
}

func (ts *toolSet) getDoc(_ context.Context, req *mcp.CallToolRequest, in getDocIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		d, err := ts.svc.GetDoc(in.Project, in.UID)
		if err != nil {
			return "", err
		}
		view := LineView{Start: in.StartLine, Max: in.MaxLines, Outline: in.Outline, Suggest: true}
		return renderDocDetail(d, view), nil
	})
}

// saveImageIn save_doc_image 的入参。
type saveImageIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Data    string `json:"data" jsonschema:"图片的 base64（可带 data:image/...;base64, 前缀）；只接受 PNG / JPEG / GIF / WebP，单张 ≤5MB"`
	Name    string `json:"name,omitempty" jsonschema:"图片说明（写进 Markdown 的 alt 文本），默认「图片」"`
}

func (ts *toolSet) saveDocImage(ctx context.Context, req *mcp.CallToolRequest, in saveImageIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		url, err := ts.svc.SaveImage(in.Project, in.Data)
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = "图片"
		}
		return fmt.Sprintf("已保存图片：%s\n可直接写进 Markdown：![%s](%s)", url, name, url), nil
	})
}

// getDocsIn get_docs（批量）的入参。
type getDocsIn struct {
	Project   string   `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UIDs      []string `json:"uids" jsonschema:"文档 uid 列表，一次 1–10 个（超出直接报错，请分批）"`
	StartLine int      `json:"startLine,omitempty" jsonschema:"从第几行开始（1 基）；与 maxLines 一起分段读取（行号仅供阅读，写回时不要带）"`
	MaxLines  int      `json:"maxLines,omitempty" jsonschema:"本次最多返回行数（默认 100，最大 500）"`
	Outline   bool     `json:"outline,omitempty" jsonschema:"true = 只返回 Markdown 标题大纲（带行号）"`
}

// getDocs 批量取文档详情：一次最多 10 个 uid，结果按 uid 分组；单个失败不影响其它。
func (ts *toolSet) getDocs(_ context.Context, req *mcp.CallToolRequest, in getDocsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		uids := make([]string, 0, len(in.UIDs))
		seen := make(map[string]bool, len(in.UIDs))
		for _, u := range in.UIDs {
			u = strings.TrimSpace(u)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			uids = append(uids, u)
		}
		if len(uids) == 0 {
			return "", withInvalid(errors.New("uids 不能为空：一次传 1–10 个文档 uid（从 list_docs 取）"))
		}
		if len(uids) > maxBatchDocs {
			return "", withLimit(fmt.Errorf("uids 一次最多 %d 个（本次 %d 个）：请分批调用", maxBatchDocs, len(uids)))
		}
		view := LineView{Start: in.StartLine, Max: in.MaxLines, Outline: in.Outline, Suggest: true}
		var sb strings.Builder
		fmt.Fprintf(&sb, "批量文档（%d 篇，项目 %s）：", len(uids), in.Project)
		ok := 0
		for _, uid := range uids {
			d, derr := ts.svc.GetDoc(in.Project, uid)
			sb.WriteString("\n")
			if derr != nil {
				fmt.Fprintf(&sb, "===== uid %s 读取失败：%v =====\n", uid, derr)
				continue
			}
			ok++
			fmt.Fprintf(&sb, "===== %s =====\n", uid)
			sb.WriteString(renderDocDetail(d, view))
		}
		fmt.Fprintf(&sb, "\n（成功 %d / %d）", ok, len(uids))
		return sb.String(), nil
	})
}

// maxBatchDocs get_docs 单次 uid 上限（与 get_request_details 对齐）。
const maxBatchDocs = 10

// ---- 容器级工具 ----

type renameModuleIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 默认工作目录"`
	Path    string `json:"path" jsonschema:"模块相对路径，如 api/user（get_project_modules 返回的 path）"`
	Name    string `json:"name" jsonschema:"新的模块显示名（只改 folder.yml，目录与 uid 不变）"`
}

func (ts *toolSet) renameModule(_ context.Context, req *mcp.CallToolRequest, in renameModuleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.RenameModule(in.Project, in.Path, in.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("已重命名模块 %s → %s（显示名变了，路径/目录仍是 %s）", in.Path, in.Name, in.Path), nil
	})
}

type deleteModuleIn struct {
	Project   string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 默认工作目录"`
	Path      string `json:"path" jsonschema:"模块相对路径，如 api/user"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"true = 连同子模块/请求/文档整棵删除（进 .trash）；false（默认）只允许空模块"`
}

func (ts *toolSet) deleteModule(_ context.Context, req *mcp.CallToolRequest, in deleteModuleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.DeleteModule(in.Project, in.Path, in.Recursive); err != nil {
			return "", err
		}
		mode := "空模块"
		if in.Recursive {
			mode = "整棵（含子项）"
		}
		return fmt.Sprintf("已删除模块 %s（%s），目录已移入集合的 .trash，可人工找回", in.Path, mode), nil
	})
}

type renameProjectIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid（不能省略，避免改错）"`
	Name    string `json:"name" jsonschema:"新的项目显示名（改 opencollection.yml 的 info.name，目录不变）"`
}

func (ts *toolSet) renameProject(_ context.Context, req *mcp.CallToolRequest, in renameProjectIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.RenameProject(in.Project, in.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("已重命名项目 %s → %s（目录不变）", in.Project, in.Name), nil
	})
}

type deleteProjectIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid（不能省略）"`
	Confirm string `json:"confirm" jsonschema:"必填：填项目显示名或目录名，用于二次确认；不一致直接拒绝"`
}

func (ts *toolSet) deleteProject(_ context.Context, req *mcp.CallToolRequest, in deleteProjectIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		dest, err := ts.svc.DeleteProject(in.Project, in.Confirm)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已删除项目 %s：目录已移入 %s（可手动移回恢复）。list_projects 已刷新。", in.Project, dest), nil
	})
}

type pruneModulesIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 默认工作目录"`
}

func (ts *toolSet) pruneEmptyModules(_ context.Context, req *mcp.CallToolRequest, in pruneModulesIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		n, err := ts.svc.PruneEmptyModules(in.Project)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已清理 %d 个空模块目录（只删只有 folder.yml、没有请求/文档/子模块的目录）", n), nil
	})
}

// createModuleIn create_module 的入参。
type createModuleIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Name    string `json:"name" jsonschema:"模块名"`
	Parent  string `json:"parent,omitempty" jsonschema:"父模块路径；留空 = 项目根"`
}

func (ts *toolSet) createModule(ctx context.Context, req *mcp.CallToolRequest, in createModuleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		path, err := ts.svc.CreateModule(in.Project, in.Parent, in.Name)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已创建模块 %s（项目 %s）", path, in.Project), nil
	})
}

// createProjectIn create_project 的入参。
type createProjectIn struct {
	Name    string `json:"name" jsonschema:"项目名（写进 manifest 的 info.name）"`
	DirName string `json:"dirName,omitempty" jsonschema:"目录名；留空 = 与 name 相同"`
	Root    string `json:"root,omitempty" jsonschema:"项目根目录；留空 = 服务启动参数 -root"`
}

func (ts *toolSet) createProject(ctx context.Context, req *mcp.CallToolRequest, in createProjectIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		p, err := ts.svc.CreateProject(in.Root, in.Name, in.DirName)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已创建项目 %s（%s，uid=%s）", p.Path, p.Name, p.UID), nil
	})
}

// createRequestArg create_request 的入参（直接复用 service 的结构，json tag 已写好）。
type createRequestArg = CreateRequestInput

func (ts *toolSet) createRequest(ctx context.Context, req *mcp.CallToolRequest, in createRequestArg) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		r, err := ts.svc.CreateRequest(in)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已创建请求 [%s] %s（uid=%s，路径 %s，项目 %s）", r.Method, r.Name, r.UID, r.Path, in.Project), nil
	})
}

// updateArg update_request 的入参。
type updateArg = UpdateRequestInput

func (ts *toolSet) updateRequest(ctx context.Context, req *mcp.CallToolRequest, in updateArg) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
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
			return "", err
		}
		out := fmt.Sprintf("已更新 [%s] %s（uid=%s，路径 %s）", r.Method, r.Name, r.UID, r.Path)
		if touched {
			if kind, body := changePreview(oldDocs, r.Docs); body != "" {
				out += "\n落盘确认（说明文档" + kind + "）：\n" + body
			}
		}
		return out, nil
	})
}

// deleteIn delete_request 的入参。
type deleteIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID     string `json:"uid" jsonschema:"要移除的接口 uid"`
}

func (ts *toolSet) deleteRequest(ctx context.Context, req *mcp.CallToolRequest, in deleteIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.DeleteRequest(in.Project, in.UID); err != nil {
			return "", err
		}
		return fmt.Sprintf("已移除请求 %s（文件已移入 .trash，可人工找回）", in.UID), nil
	})
}

// createDocArg create_doc 的入参（复用 service 结构，json/描述已写好）。
type createDocArg = CreateDocInput

func (ts *toolSet) createDoc(_ context.Context, req *mcp.CallToolRequest, in createDocArg) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		d, err := ts.svc.CreateDoc(in)
		if err != nil {
			return "", err
		}
		out := fmt.Sprintf("已创建文档 %s（uid=%s，路径 %s，项目 %s，%d 字节）",
			d.Name, d.UID, d.Path, in.Project, d.Size)
		if in.DryRun {
			action := "创建"
			if d.Planned == "overwrite" {
				action = "覆盖（复用已有 uid/path）"
			}
			out = fmt.Sprintf("（dryRun：未落盘）将%s %s（uid=%s，项目 %s）\n", action, d.Path, d.UID, in.Project) + out
		} else if strings.EqualFold(strings.TrimSpace(in.OnConflict), "overwrite") {
			out = "已复用同路径文档并覆盖正文（onConflict=overwrite）\n" + out
		}
		return out, nil
	})
}

// updateDocArg update_doc 的入参。
type updateDocArg = UpdateDocInput

func (ts *toolSet) updateDoc(_ context.Context, req *mcp.CallToolRequest, in updateDocArg) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		// 动正文前先留旧文本：成功响应要给「改动行 ±1」的落盘确认
		oldContent := ""
		touched := in.Content != nil || len(in.Replaces) > 0
		if touched {
			if d0, derr := ts.svc.GetDoc(in.Project, in.UID); derr == nil {
				oldContent = d0.Content
			}
		}
		d, err := ts.svc.UpdateDoc(in)
		if err != nil {
			return "", err
		}
		if in.DryRun {
			out := fmt.Sprintf("（dryRun：未落盘）将更新文档 %s（uid=%s，路径 %s）", d.Name, d.UID, d.Path)
			if kind, body := changePreview(d.PrevContent, d.Content); body != "" {
				out += "\n改动预览（正文" + kind + "）：\n" + body
			}
			return out, nil
		}
		out := fmt.Sprintf("已更新文档 %s（uid=%s，路径 %s，%d 字节）", d.Name, d.UID, d.Path, d.Size)
		if touched {
			if kind, body := changePreview(oldContent, d.Content); body != "" {
				out += "\n落盘确认（正文" + kind + "）：\n" + body
			}
		}
		return out, nil
	})
}

// deleteDocIn delete_doc 的入参（比 deleteIn 多一句说明：这条是文档不是接口）。
type deleteDocIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID     string `json:"uid" jsonschema:"要移除的文档 uid（list_docs 返回）"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只返回将移入 .trash 的路径，不实际删除"`
}

func (ts *toolSet) deleteDoc(_ context.Context, req *mcp.CallToolRequest, in deleteDocIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if in.DryRun {
			d, err := ts.svc.GetDoc(in.Project, in.UID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("（dryRun：未落盘）将把 %s（uid=%s）移入集合 .trash，可人工找回", d.Path, d.UID), nil
		}
		if err := ts.svc.DeleteDoc(in.Project, in.UID); err != nil {
			return "", err
		}
		return fmt.Sprintf("已移除文档 %s（文件已移入 .trash，可人工找回）", in.UID), nil
	})
}

// ---- 响应示例工具的 handler ----

// saveResponseExampleArg save_response_example 的入参（复用 service 结构）。
type saveResponseExampleArg = SaveResponseExampleInput

func (ts *toolSet) saveResponseExample(_ context.Context, req *mcp.CallToolRequest, in saveResponseExampleArg) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		ex, err := ts.svc.SaveResponseExample(in)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已保存响应示例 %s（uid=%s，路径 %s，状态码 %d，%d 字节，项目 %s）",
			ex.Name, ex.UID, ex.Path, ex.Response.Status, len(ex.Response.Body), in.Project), nil
	})
}

// updateResponseExampleArg update_response_example 的入参。
type updateResponseExampleArg = UpdateResponseExampleInput

func (ts *toolSet) updateResponseExample(_ context.Context, req *mcp.CallToolRequest, in updateResponseExampleArg) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		ex, err := ts.svc.UpdateResponseExample(in)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已更新响应示例 %s（uid=%s，路径 %s，状态码 %d，%d 字节）",
			ex.Name, ex.UID, ex.Path, ex.Response.Status, len(ex.Response.Body)), nil
	})
}

// deleteResponseExampleIn delete_response_example 的入参。
type deleteResponseExampleIn struct {
	Project    string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID        string `json:"uid" jsonschema:"请求 uid（示例所属请求）"`
	ExampleUID string `json:"exampleUid" jsonschema:"要删除的响应示例 uid（get_request_detail 返回的 examples[].uid）"`
}

func (ts *toolSet) deleteResponseExample(_ context.Context, req *mcp.CallToolRequest, in deleteResponseExampleIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.DeleteResponseExample(in.Project, in.UID, in.ExampleUID); err != nil {
			return "", err
		}
		return fmt.Sprintf("已删除响应示例 %s（文件已移入 .trash，可人工找回）", in.ExampleUID), nil
	})
}

// ---- 环境变量工具的 handler ----

// listEnvsIn list_envs 的入参。
type listEnvsIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
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
			"project": in.Project, "total": total, "returned": len(envs), "offset": in.Offset, "envs": envs,
		}
		return renderEnvs(in.Project, envs, total, in.Offset), data, nil
	})
}

// renameEnvIn rename_env 的入参。
type renameEnvIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Name    string `json:"name" jsonschema:"当前环境名"`
	NewName string `json:"newName" jsonschema:"新环境名（只允许字母、数字、- 与 _；不能与现有名重复，也不允许只改大小写）"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_envs 里该环境的 hash；不传 = 最后写者赢"`
}

func (ts *toolSet) renameEnv(ctx context.Context, req *mcp.CallToolRequest, in renameEnvIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		env, err := ts.svc.RenameEnv(in.Project, in.Name, in.NewName, in.IfMatch)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已把环境 %s 改名为 %s（%d 个变量已搬过去，旧文件在 .trash 可找回，hash %s…）",
			in.Name, env.Name, len(env.Vars), env.Hash), nil
	})
}

// deleteEnvIn delete_env 的入参。
type deleteEnvIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Name    string `json:"name" jsonschema:"要删除的环境名"`
}

func (ts *toolSet) deleteEnv(ctx context.Context, req *mcp.CallToolRequest, in deleteEnvIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.DeleteEnv(in.Project, in.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("已删除环境 %s（文件已移入 .trash，可人工找回）", in.Name), nil
	})
}

func (ts *toolSet) createEnv(ctx context.Context, req *mcp.CallToolRequest, in CreateEnvInput) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		env, err := ts.svc.CreateEnv(in)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已创建环境 %s（项目 %s，初始变量 %d 个）%s",
			env.Name, in.Project, len(env.Vars), envTip(len(env.Vars))), nil
	})
}

func (ts *toolSet) setEnvVar(ctx context.Context, req *mcp.CallToolRequest, in SetEnvVarInput) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		v, err := ts.svc.SetEnvVar(in)
		if err != nil {
			return "", err
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
			in.Env, v.Name, v.Value, v.Secret, v.Enabled, extra, v.Hash), nil
	})
}

// deleteEnvVarIn delete_env_var 的入参。
type deleteEnvVarIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Env     string `json:"env" jsonschema:"所属环境名"`
	Name    string `json:"name" jsonschema:"要删除的变量名"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_envs 里该环境的 hash；不传 = 最后写者赢"`
}

func (ts *toolSet) deleteEnvVar(ctx context.Context, req *mcp.CallToolRequest, in deleteEnvVarIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		if err := ts.svc.DeleteEnvVar(in.Project, in.Env, in.Name, in.IfMatch); err != nil {
			return "", err
		}
		return fmt.Sprintf("已从环境 %s 删除变量 %s", in.Env, in.Name), nil
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
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "失败: " + err.Error()}},
		}, nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

// wrapData 与 wrap 相同，但额外返回 structuredContent（仅列表类工具使用）。
// 文本输出保持原样：旧客户端 / 只读人看文本，新客户端可直接解析结构。
func (ts *toolSet) wrapData(fn func() (string, any, error)) (*mcp.CallToolResult, any, error) {
	text, data, err := fn()
	if err != nil {
		err = coded(CodeOf(err), err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "失败: " + err.Error()}},
		}, nil, nil
	}
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	if data != nil {
		res.StructuredContent = data
	}
	return res, nil, nil
}
