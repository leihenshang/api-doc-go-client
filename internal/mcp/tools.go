package mcp

import (
	"context"
	"encoding/json"
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
// guard 在这一层包住所有 handler：panic recover + 风险工具交互授权，18 个业务 handler 零改动。
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
	mcp.AddTool(server, &mcp.Tool{Name: meta.Name, Description: meta.Description}, wrapped)
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

func (ts *toolSet) registerReadTools(server *mcp.Server) {
	addTool(ts, server, "list_workspaces", ts.listWorkspaces)
	addTool(ts, server, "use_workspace", ts.useWorkspace)
	addTool(ts, server, "list_projects", ts.listProjects)
	addTool(ts, server, "get_project_modules", ts.getProjectModules)
	addTool(ts, server, "get_request_detail", ts.getRequestDetail)
	addTool(ts, server, "search_requests", ts.searchRequests)
	addTool(ts, server, "list_envs", ts.listEnvs)
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
	return ts.wrap(func() (string, error) {
		items, skipped, err := ts.svc.ListProjects(in.Project, in.Refresh)
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "没有匹配的项目。用 refresh=true 重新扫描，或确认 -root 下有含 opencollection.yml 的目录。", nil
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
		return sb.String(), nil
	})
}

// getModulesIn get_project_modules 的入参。
type getModulesIn struct {
	Project         string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Query           string `json:"query,omitempty" jsonschema:"对模块名与请求名/方法/路径做模糊过滤"`
	IncludeRequests bool   `json:"includeRequests,omitempty" jsonschema:"true = 附带每个模块下的请求清单"`
}

func (ts *toolSet) getProjectModules(ctx context.Context, req *mcp.CallToolRequest, in getModulesIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		res, err := ts.svc.GetProjectModules(in.Project, in.Query, in.IncludeRequests)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "项目 %s：%d 个模块，%d 个请求", in.Project, len(res.Modules), res.Total)
		if in.Query != "" {
			fmt.Fprintf(&sb, "，其中 %d 个匹配「%s」", res.Matched, in.Query)
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
		return sb.String(), nil
	})
}

// getDetailIn get_request_detail 的入参。
type getDetailIn struct {
	Project      string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID          string `json:"uid" jsonschema:"接口 uid（用 search_requests 或 get_project_modules 获取）"`
	Query        string `json:"query,omitempty" jsonschema:"在示例名/文档名里再筛一遍"`
	IncludeBody  bool   `json:"includeBody,omitempty" jsonschema:"true = 返回响应示例的响应体与请求体全文"`
	MaxBodyBytes int    `json:"maxBodyBytes,omitempty" jsonschema:"响应体裁剪上限（字节），默认 8192"`
}

func (ts *toolSet) getRequestDetail(ctx context.Context, req *mcp.CallToolRequest, in getDetailIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		d, err := ts.svc.GetRequestDetail(in.Project, in.UID, DetailOptions{
			IncludeBody: in.IncludeBody, MaxBodyBytes: in.MaxBodyBytes, Query: in.Query,
		})
		if err != nil {
			return "", err
		}
		return renderDetail(d), nil
	})
}

// searchIn_ search_requests 的入参（名字避开 fuzzy.go 的 candidate 类型冲突）。
type searchArgs struct {
	Query   string `json:"query" jsonschema:"搜索关键词（名称/URL/方法/路径/header/说明/请求体片段）"`
	Project string `json:"project,omitempty" jsonschema:"限定项目；省略则在所有已加载项目里搜"`
	Limit   int    `json:"limit,omitempty" jsonschema:"返回条数上限，默认 50，最大 200"`
}

func (ts *toolSet) searchRequests(ctx context.Context, req *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		ms, total, err := ts.svc.SearchRequests(in.Query, in.Project, in.Limit)
		if err != nil {
			return "", err
		}
		if len(ms) == 0 {
			return fmt.Sprintf("没有匹配「%s」的请求。", in.Query), nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "「%s」命中 %d 条（返回 %d 条，按相关度排序）：\n", in.Query, total, len(ms))
		for _, m := range ms {
			fmt.Fprintf(&sb, "- [%s] %s %s\n    项目 %s · 路径 %s · uid %s · 分数 %d · 命中 %s\n",
				m.Method, m.Name, m.URL, m.Project, m.Path, m.UID, m.Score, strings.Join(m.Matched, "/"))
		}
		return sb.String(), nil
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
		r, err := ts.svc.UpdateRequest(in)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已更新 [%s] %s（uid=%s，路径 %s）", r.Method, r.Name, r.UID, r.Path), nil
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

// ---- 环境变量工具的 handler ----

// listEnvsIn list_envs 的入参。
type listEnvsIn struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Query   string `json:"query,omitempty" jsonschema:"按环境名 / 变量名模糊过滤；留空返回全部"`
}

func (ts *toolSet) listEnvs(ctx context.Context, req *mcp.CallToolRequest, in listEnvsIn) (*mcp.CallToolResult, any, error) {
	return ts.wrap(func() (string, error) {
		in.Project = ts.proj(req, in.Project)
		envs, err := ts.svc.ListEnvs(in.Project, in.Query)
		if err != nil {
			return "", err
		}
		return renderEnvs(in.Project, envs), nil
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
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "失败: " + err.Error()}},
		}, nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}
