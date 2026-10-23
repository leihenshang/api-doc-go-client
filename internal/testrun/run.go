package testrun

// Collection 运行器（RM-02）：把本地集合按树顺序跑一遍，执行前置/后置脚本与断言，
// 输出 text / JSON / JUnit，供 CI 使用。
//
// 与桌面端 SendRequest 共用同一套底层：collection.MergeDefaults（集合级公共头/认证）→
// script 会话（vars.pre-request + script.pre-request）→ runner.Send / SendGRPC →
// script.post-response + assert。区别只有：不写历史、不发 UI 事件、按顺序批量执行。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/proto"
	"api-doc-go-client/internal/runner"
	"api-doc-go-client/internal/script"
	"api-doc-go-client/internal/varx"
)

// Options 一次集合运行的参数。
type Options struct {
	// EnvName 环境名；空 = 只用内置变量。
	EnvName string
	// Filter 按请求名称/路径做不区分大小写的子串过滤；空 = 全部。
	Filter string
	// Bail 遇到第一个失败即停止。
	Bail bool
	// Timeout 覆盖全局单请求超时；0 = 用 Runner / 请求级设置。
	Timeout time.Duration
	// Runner 发送策略（超时、重定向、Cookie、代理等）。
	Runner runner.Options
}

// caseInput runCase 的入参（避免 5 个位置参数）。
type caseInput struct {
	ctx      context.Context
	coll     *collection.Collection
	req      *collection.Request
	baseVars map[string]string
	opts     runner.Options
	cache    *proto.Cache
}

// Run 执行集合，返回报告。没有任何可运行请求时返回错误（避免 CI 空跑绿）。
func Run(ctx context.Context, c *collection.Collection, opts Options) (*Report, error) {
	if c == nil {
		return nil, fmt.Errorf("集合为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	envVars, err := loadEnvVars(c, opts.EnvName)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	nodes := collectRequests(tree, opts.Filter)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("没有可运行的请求（检查 --filter）")
	}
	runOpts := opts.Runner
	if runOpts.Timeout <= 0 {
		runOpts.Timeout = runner.DefaultOptions().Timeout
	}
	if opts.Timeout > 0 {
		runOpts.Timeout = opts.Timeout
	}

	rep := &Report{Collection: c.Name, Env: opts.EnvName, StartedAt: time.Now()}
	cache := proto.NewCache()
	runtimeVars := map[string]string{}
	for _, n := range nodes {
		req, rerr := c.ReadRequest(n.UID)
		if rerr != nil {
			rep.Cases = append(rep.Cases, Case{
				Name: n.Name, Path: n.Path, UID: n.UID, Method: n.Method,
				Error: "读取请求失败：" + rerr.Error(),
			})
		} else {
			cs := runCase(caseInput{
				ctx: ctx, coll: c, req: req, baseVars: mergeVars(envVars, runtimeVars),
				opts: runOpts, cache: cache,
			})
			rep.Cases = append(rep.Cases, cs)
			for k, v := range cs.Vars {
				runtimeVars[k] = v
			}
		}
		if opts.Bail && !rep.Cases[len(rep.Cases)-1].Passed {
			break
		}
	}
	rep.finish()
	return rep, nil
}

// loadEnvVars 读取指定环境的启用变量 + 内置变量；指定了环境但不存在时报错。
func loadEnvVars(c *collection.Collection, envName string) (map[string]string, error) {
	vars := map[string]string{}
	envs, err := c.ListEnvs()
	if err != nil {
		return nil, err
	}
	if envName != "" {
		found := false
		for _, e := range envs {
			if e.Name != envName {
				continue
			}
			found = true
			for _, v := range e.Vars {
				if v.Enabled && v.Name != "" {
					vars[v.Name] = v.Value
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("环境不存在：%s", envName)
		}
	}
	for k, v := range varx.Builtins() {
		vars[k] = v
	}
	return vars, nil
}

// collectRequests 按树顺序收集请求节点（目录先于内容，组内按 seq/名称）。
func collectRequests(nodes []*collection.Node, filter string) []*collection.Node {
	f := strings.ToLower(strings.TrimSpace(filter))
	out := make([]*collection.Node, 0, len(nodes))
	var walk func([]*collection.Node)
	walk = func(list []*collection.Node) {
		for _, n := range list {
			if n.Type == "request" {
				name, path := strings.ToLower(n.Name), strings.ToLower(n.Path)
				if f == "" || strings.Contains(name, f) || strings.Contains(path, f) {
					out = append(out, n)
				}
			}
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return out
}

// runCase 执行单条请求：默认值合并 → 前置 → 发送 → 后置/断言。
func runCase(in caseInput) Case {
	start := time.Now()
	req := collection.MergeDefaults(in.req, in.coll.Defaults())
	cs := Case{Name: req.Name, Path: req.Path, UID: req.UID, Method: req.Method, URL: req.URL}

	scr := script.Scripts{}
	if req.Script != nil {
		scr = script.Scripts{PreRequest: req.Script.PreRequest, PostResponse: req.Script.PostResponse}
	}
	sess := script.New(in.baseVars, scr)
	preVars := make([]script.Var, 0, len(req.VarsPreRequest))
	for _, v := range req.VarsPreRequest {
		preVars = append(preVars, script.Var{Name: v.Name, Value: v.Value, Enabled: v.Enabled})
	}
	pre := sess.RunPreRequest(preVars)
	sendVars := sess.Vars()
	for k, v := range pre.Vars {
		if resolved, missing := varx.Resolve(v, sendVars); len(missing) == 0 && resolved != v {
			sendVars[k] = resolved
			sess.SetVar(k, resolved)
		}
	}

	res, sendErr := execute(in.ctx, in.coll, req, sendVars, in.opts, in.cache)
	cs.Vars = mergeVars(pre.Vars, nil)
	if sendErr != nil {
		cs.Error = sendErr.Error()
		cs.DurationMS = time.Since(start).Milliseconds()
		return cs
	}
	if res == nil {
		cs.Error = "发送结果为空"
		cs.DurationMS = time.Since(start).Milliseconds()
		return cs
	}

	post := sess.RunPostResponse(scriptResponse(res), toScriptAsserts(req.Asserts))
	cs.Vars = mergeVars(pre.Vars, post.Vars)
	cs.Asserts = post.Asserts
	cs.ScriptError = joinScriptErrors(pre.ScriptError, post.ScriptError)
	// 只保留请求文件里的模板 URL（含占位符），不把渲染后的 URL 写进报告，避免 secret 进 CI 日志。
	cs.Status, cs.Proto = res.Status, res.Proto
	cs.Passed = cs.ScriptError == "" && assertsPassed(cs.Asserts)
	cs.DurationMS = time.Since(start).Milliseconds()
	return cs
}

// execute 按协议分派：HTTP 走 runner.Send，gRPC 走 SendGRPC（见 proto.go）。
func execute(ctx context.Context, c *collection.Collection, r *collection.Request, vars map[string]string, opts runner.Options, cache *proto.Cache) (*runner.Result, error) {
	if r.IsGRPC() {
		return sendGRPC(ctx, c, r, vars, opts, cache)
	}
	return runner.Send(ctx, *r, vars, opts)
}

// scriptResponse 把执行结果转成脚本可见的 res 对象。
func scriptResponse(res *runner.Result) *script.Response {
	return &script.Response{
		Status:       res.Status,
		Headers:      kvMap(res.Headers),
		Body:         script.ParseBody(res.Body),
		BodyText:     res.Body,
		ResponseTime: res.TimeMS,
		ContentType:  res.ContentType,
		IsGRPC:       res.Proto == "gRPC",
		GRPCCode:     grpcCodeOf(res),
		Trailers:     kvMap(res.Trailers),
	}
}

// toScriptAsserts 集合断言 → 脚本断言。
func toScriptAsserts(list []collection.ScriptAssert) []script.Assert {
	if len(list) == 0 {
		return nil
	}
	out := make([]script.Assert, 0, len(list))
	for _, a := range list {
		out = append(out, script.Assert{Name: a.Name, Expr: a.Expr})
	}
	return out
}

// assertsPassed 全部断言通过（没有断言视为通过）。
func assertsPassed(list []script.AssertResult) bool {
	for _, a := range list {
		if !a.Passed {
			return false
		}
	}
	return true
}

// joinScriptErrors 合并前置/后置脚本错误。
func joinScriptErrors(pre, post string) string {
	switch {
	case pre == "":
		return post
	case post == "":
		return pre
	default:
		return pre + "; " + post
	}
}

// kvMap 响应头/尾元数据转脚本 map。
func kvMap(rows []collection.KV) map[string]string {
	if len(rows) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Name] = row.Value
	}
	return out
}

// grpcCodeOf gRPC 状态码（HTTP 响应为 0）。
func grpcCodeOf(res *runner.Result) int {
	if res != nil && res.Proto == "gRPC" {
		return res.Status
	}
	return 0
}

// mergeVars 合并变量副本（后者覆盖前者）。
func mergeVars(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}
