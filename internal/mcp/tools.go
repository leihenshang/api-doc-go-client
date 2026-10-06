package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 工具集：把 Service 的方法注册成 MCP 工具。Out 一律用 any（不声明输出 schema），
// 结果以「人类/AI 可读文本」放进 Content —— AI 工具链里文本比结构化 JSON 更省 token、也更好读。
type toolSet struct {
	svc      *Service
	readOnly bool // 只读模式：不注册写工具，send_request 强制不落盘

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

// RegisterTools 把全部工具注册到 server。
func RegisterTools(server *mcp.Server, svc *Service, readOnly bool) {
	ts := &toolSet{svc: svc, readOnly: readOnly}
	ts.registerReadTools(server)
	if readOnly {
		// 只读模式下仍给 send_request，但明确「不落盘」——AI 仍可试发，只是不污染集合
		ts.registerSendTool(server, true)
		return
	}
	ts.registerWriteTools(server)
	ts.registerSendTool(server, false)
}

func (ts *toolSet) registerReadTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "列出 MCP 可访问的工作目录（客户端授权的白名单）与其中的项目、当前默认目标。白名单为空时 AI 无法读写任何集合 —— 需要时请让用户在客户端「设置 → MCP 服务 → 可访问的工作目录」里添加。",
	}, ts.listWorkspaces)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "use_workspace",
		Description: "选定默认工作目录：选定后其余工具的 project 参数即可省略。workspace 传 list_workspaces 返回的工作目录 path，或一个项目标识（名称/路径/uid）；不在白名单内会直接拒绝。",
	}, ts.useWorkspace)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_projects",
		Description: "列出所有项目（集合）。project = 包含 opencollection.yml 的目录，一个项目一个集合。范围仅限客户端授权的 MCP 工作目录（见 list_workspaces）。返回名称、路径、uid、请求数/模块数/环境列表。",
	}, ts.listProjects)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_modules",
		Description: "获取某个项目的目录（模块）结构与每个模块下的请求数。query 可对模块名与请求名/方法/路径做模糊过滤；include_requests=true 时附带请求清单。",
	}, ts.getProjectModules)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_request_detail",
		Description: "获取某个接口的详情：请求（method/url/params/body/auth/settings/grpc）、headers、说明文档（docs）以及已保存的响应示例。query 可在示例名/文档名里再筛一遍；include_body=true 时返回示例的响应体（按 maxBodyBytes 裁剪）。返回的 hash 可传给 update_request.if_match 做冲突保护。",
	}, ts.getRequestDetail)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_requests",
		Description: "模糊搜索接口：按名称/URL/方法/路径/header/说明/body 打分排序（分数越高越相关，matched 列出命中字段）。project 省略时在所有已加载项目里搜。适合「记得大概内容但记不清名字」的场景。",
	}, ts.searchRequests)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_envs",
		Description: "列出项目的全部环境与其中的变量（敏感变量的值只回掩码 ••••••，拿不到真实密钥）。query 可对环境名与变量名做模糊过滤。环境即 environments/<env>.yml，请求里用 {{变量名}} 引用这些变量。每个环境带一个 hash，写操作时作为 if_match 传回即可避免覆盖期间的其它改动。",
	}, ts.listEnvs)
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
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_request",
		Description: "创建接口请求。支持三种来源：直接给 method/url/headers/body；给 curl（cURL 命令，自动解析）；给 copyFromUid（复制已有请求）。同名会在分组内自动加序号。",
	}, ts.createRequest)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_module",
		Description: "创建模块（目录）。parent 为空表示在项目根下建；支持嵌套（如 parent=api 建 api/user）。",
	}, ts.createModule)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_project",
		Description: "在**授权且可写**的工作目录下新建一个项目（集合目录 + manifest），建完即可用 list_projects 看到。dir 必须是白名单内的可写目录，留空时用第一个可写目录；白名单外一律拒绝。",
	}, ts.createProject)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_request",
		Description: "修改已有接口（只改传入的字段）。folder 可移动模块；ifMatch 传详情里拿到的 hash 可检测外部改动并拒绝覆盖（不传则最后写者赢）。改名会自动同步文件名。",
	}, ts.updateRequest)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_request",
		Description: "移除接口请求。文件会移入集合的 .trash 目录（可人工找回），不会真正删除。",
	}, ts.deleteRequest)
	ts.registerEnvWriteTools(server)
}

// registerEnvWriteTools 环境变量的写操作（4 个环境级 + 2 个变量级）。
// 单列一个函数：环境变量是一组有共同约定的工具（敏感值掩码、名字规则），放一起更好维护。
func (ts *toolSet) registerEnvWriteTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_env",
		Description: "新建环境（environments/<name>.yml），可同时写入初始变量。重名会报错，不覆盖已有环境。环境名只允许字母、数字、- 与 _。",
	}, ts.createEnv)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rename_env",
		Description: "给环境改名：按新名写一份（敏感值一起搬过去），再把旧名移入 .trash。目标名已被占用会报错；只改大小写（如 dev→DEV）也会被拒绝——在 Windows 上那是同一个文件，改名会让环境消失。ifMatch 传 list_envs 里的 hash 可检测期间的外部改动并拒绝覆盖。",
	}, ts.renameEnv)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_env",
		Description: "删除环境（两个文件都移入集合的 .trash，可人工找回）。删之前建议先 list_envs 确认里面没有还要用的变量。",
	}, ts.deleteEnv)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_env_var",
		Description: "新增或更新某环境里的一个变量（按变量名 upsert，只改这一条，其它变量与顺序都不动）。强烈建议先 list_envs 拿到该环境的 hash 再带 ifMatch 写入（环境是整份重写，不带校验会覆盖期间的其它改动）。secret=true 时值落盘进 .secrets.yml，之后对外只回掩码；若把掩码原样传回表示「密钥不变」，不会覆盖真值（掩码被改写成 •••• / ????? 这类占位也认）；对已存在的敏感变量传空值会被拒绝，避免误清空密钥。enabled=false 可停用某变量（不参与 {{name}} 解析）。",
	}, ts.setEnvVar)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_env_var",
		Description: "从某环境里删掉一个变量。变量不存在会报错（避免「以为删掉了」）。ifMatch 传 list_envs 里该环境的 hash 可获得并发保护。",
	}, ts.deleteEnvVar)
}

func (ts *toolSet) registerSendTool(server *mcp.Server, forceNoSave bool) {
	desc := "发送请求并返回状态码/耗时/大小/响应体（响应体按 maxBodyBytes 裁剪）。saveExample 默认 true：把响应存成该请求的响应示例（examples/*.yml，客户端可回看/删除）。可以给 uid 发已保存的请求，也可以给 method+url 发临时请求（临时请求不落盘、不存示例）。"
	if forceNoSave {
		desc = "发送请求并返回结果（只读模式：不会保存响应示例）。可以给 uid 发已保存的请求，也可以给 method+url 发临时请求。"
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_request",
		Description: desc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in SendRequestInput) (*mcp.CallToolResult, any, error) {
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
