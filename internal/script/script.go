// Package script 用 goja 执行请求的前置/后置脚本与断言（C6 / E15–E17）。
//
// 文件字段（Bruno 超集，见 request_file_test.go 样例）：
//
//	vars:
//	  pre-request:
//	    - { name: traceId, value: "{{$uuid}}", enabled: true }
//	script:
//	  pre-request: bru.setVar('ts', Date.now())
//	  post-response: |
//	    bru.setVar('id', res.body.id)
//	assert:
//	  - { name: 状态码为 200, expr: "res.status === 200" }
//
// bru 最小 API：setVar / getVar / setEnvVar / getEnvVar。
// 断言 expr 为 JS 布尔表达式，可引用 res（status/headers/body/responseTime）与 bru 变量。
package script

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dop251/goja"
)

// Var 脚本阶段的变量条目（对应 vars.pre-request 的一行）。
type Var struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// Assert 一条断言。
type Assert struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

// Scripts 请求上的脚本块（pre-request / post-response 源码）。
type Scripts struct {
	PreRequest   string `json:"preRequest"`
	PostResponse string `json:"postResponse"`
}

// AssertResult 一条断言的执行结果。
type AssertResult struct {
	Name   string `json:"name"`
	Expr   string `json:"expr"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

// RunResult 脚本/断言阶段的汇总。
type RunResult struct {
	// Vars 脚本阶段写入的变量（并入发送/预览），覆盖同名环境变量。
	Vars map[string]string `json:"vars"`
	// Asserts 断言结果（仅 post-response 阶段产出）。
	Asserts []AssertResult `json:"asserts,omitempty"`
	// ScriptError 脚本运行时错误（非致命：记录后继续发送/展示）。
	ScriptError string `json:"scriptError,omitempty"`
}

// Response 发送给脚本的响应视图（body 已尽量解析为 JSON 对象）。
//
// gRPC 响应（G10.2）：Status 是 gRPC 状态码（0 = OK，不是 HTTP 状态码），
// 另有 IsGRPC / GRPCCode / Trailers 三个字段；断言与脚本里用 res.isGrpc 区分协议，
// 用 res.grpcCode（或 res.status）判状态，用 res.trailers 读尾元数据。
type Response struct {
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers"`
	Body         any               `json:"body"`
	BodyText     string            `json:"bodyText"`
	ResponseTime int64             `json:"responseTime"`
	ContentType  string            `json:"contentType"`
	// IsGRPC 是否 gRPC 响应（HTTP 为 false）
	IsGRPC bool `json:"isGrpc"`
	// GRPCCode gRPC 状态码（HTTP 响应为 0）
	GRPCCode int `json:"grpcCode"`
	// Trailers gRPC 尾元数据（HTTP 响应为空对象）
	Trailers map[string]string `json:"trailers"`
}

// Runner 一次请求生命周期内的脚本会话：变量在 pre → send → post 之间贯穿。
type Runner struct {
	env  map[string]string // 环境变量（只读底座）
	vars map[string]string // 脚本可写的运行时变量（覆盖 env）
	scr  Scripts
}

// New 创建脚本会话。env 为当前环境变量快照（会被复制）。
func New(env map[string]string, scr Scripts) *Runner {
	e := make(map[string]string, len(env))
	for k, v := range env {
		e[k] = v
	}
	return &Runner{env: e, vars: map[string]string{}, scr: scr}
}

// Vars 当前生效变量（env + 脚本写入），供发送与预览使用。
func (r *Runner) Vars() map[string]string {
	out := make(map[string]string, len(r.env)+len(r.vars))
	for k, v := range r.env {
		out[k] = v
	}
	for k, v := range r.vars {
		out[k] = v
	}
	return out
}

// SetVar 直接写入运行时变量（外部解析 {{}} 后回填）。
func (r *Runner) SetVar(name, value string) {
	r.vars[name] = value
}

// RunPreRequest 执行 vars.pre-request 赋值 + script.pre-request。
// varsList 里 enabled 的行先写入，再跑脚本（脚本可覆盖）。
func (r *Runner) RunPreRequest(varsList []Var) *RunResult {
	res := &RunResult{Vars: map[string]string{}}
	for _, v := range varsList {
		if !v.Enabled || v.Name == "" {
			continue
		}
		r.vars[v.Name] = v.Value
		res.Vars[v.Name] = v.Value
	}
	if strings.TrimSpace(r.scr.PreRequest) == "" {
		return res
	}
	if err := r.exec(r.scr.PreRequest, nil); err != nil {
		res.ScriptError = fmt.Sprintf("pre-request 脚本: %v", err)
	}
	for k, v := range r.vars {
		res.Vars[k] = v
	}
	return res
}

// RunPostResponse 执行 script.post-response 与 assert 列表。
func (r *Runner) RunPostResponse(res *Response, asserts []Assert) *RunResult {
	out := &RunResult{Vars: map[string]string{}}
	if res == nil {
		res = &Response{Status: 0, Headers: map[string]string{}, Body: nil, BodyText: ""}
	}
	if strings.TrimSpace(r.scr.PostResponse) != "" {
		if err := r.exec(r.scr.PostResponse, res); err != nil {
			out.ScriptError = fmt.Sprintf("post-response 脚本: %v", err)
		}
	}
	for k, v := range r.vars {
		out.Vars[k] = v
	}
	out.Asserts = r.evalAsserts(res, asserts)
	return out
}

// evalAsserts 逐条求值断言表达式。
func (r *Runner) evalAsserts(res *Response, asserts []Assert) []AssertResult {
	if len(asserts) == 0 {
		return nil
	}
	out := make([]AssertResult, 0, len(asserts))
	for _, a := range asserts {
		item := AssertResult{Name: a.Name, Expr: a.Expr}
		expr := strings.TrimSpace(a.Expr)
		if expr == "" {
			item.Error = "空断言"
			out = append(out, item)
			continue
		}
		vm := r.newVM(res)
		val, err := vm.RunString(fmt.Sprintf("Boolean(%s)", expr))
		if err != nil {
			item.Error = err.Error()
			out = append(out, item)
			continue
		}
		item.Passed = val.ToBoolean()
		out = append(out, item)
	}
	return out
}

// exec 运行一段脚本；res 为 nil 表示 pre-request 阶段（无 res 对象）。
func (r *Runner) exec(src string, res *Response) error {
	vm := r.newVM(res)
	_, err := vm.RunString(src)
	return err
}

// newVM 每次执行用干净的 goja VM，注入 bru / res。
func (r *Runner) newVM(res *Response) *goja.Runtime {
	vm := goja.New()
	_ = vm.Set("bru", map[string]any{
		"setVar": func(call goja.FunctionCall) goja.Value {
			name := call.Argument(0).String()
			val := call.Argument(1)
			r.vars[name] = val.String()
			return goja.Undefined()
		},
		"getVar": func(call goja.FunctionCall) goja.Value {
			name := call.Argument(0).String()
			if v, ok := r.vars[name]; ok {
				return vm.ToValue(v)
			}
			if v, ok := r.env[name]; ok {
				return vm.ToValue(v)
			}
			return goja.Undefined()
		},
		"setEnvVar": func(call goja.FunctionCall) goja.Value {
			// 客户端当前只有一层环境：写进运行时变量即可生效
			name := call.Argument(0).String()
			r.vars[name] = call.Argument(1).String()
			return goja.Undefined()
		},
		"getEnvVar": func(call goja.FunctionCall) goja.Value {
			name := call.Argument(0).String()
			if v, ok := r.env[name]; ok {
				return vm.ToValue(v)
			}
			return goja.Undefined()
		},
	})
	if res != nil {
		_ = vm.Set("res", map[string]any{
			"status":       res.Status,
			"headers":      res.Headers,
			"body":         res.Body,
			"text":         res.BodyText,
			"responseTime": res.ResponseTime,
			"contentType":  res.ContentType,
			"isGrpc":       res.IsGRPC,
			"grpcCode":     res.GRPCCode,
			"trailers":     res.Trailers,
		})
	}
	return vm
}

// ParseBody 尽量把响应体解析为 JSON 对象（断言里写 res.body.id）；失败则保留字符串。
func ParseBody(text string) any {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		return text
	}
	return v
}
