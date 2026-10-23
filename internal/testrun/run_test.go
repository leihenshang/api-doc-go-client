package testrun

// RM-02 回归：顺序执行、断言、失败退出、过滤/bail、环境变量、脚本变量链式传递、报告格式。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/runner"
)

// newRunCollection 建临时集合并把 dev 环境的 host 指向 srv。
func newRunCollection(t *testing.T, srv *httptest.Server) *collection.Collection {
	t.Helper()
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Logf("close collection: %v", err)
		}
	})
	envs, err := c.ListEnvs()
	if err != nil || len(envs) == 0 {
		t.Fatalf("ListEnvs: %v (len=%d)", err, len(envs))
	}
	env := envs[0]
	env.Vars = append(env.Vars, collection.Var{Name: "host", Value: srv.URL, Enabled: true})
	if err := c.SaveEnv(env); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}
	return c
}

// newEchoServer 提供 /ok、/token、/next 三个端点。
func newEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/token":
			_, _ = w.Write([]byte(`{"token":"abc"}`))
		case "/next":
			_ = json.NewEncoder(w).Encode(map[string]string{"query": r.URL.RawQuery})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func addRequest(t *testing.T, c *collection.Collection, name string, src *collection.Request) {
	t.Helper()
	if _, err := c.CreateRequestFromDraft("", name, src); err != nil {
		t.Fatalf("CreateRequestFromDraft(%s): %v", name, err)
	}
}

func TestRunPassesWithEnvAndAsserts(t *testing.T) {
	srv := newEchoServer(t)
	c := newRunCollection(t, srv)
	addRequest(t, c, "01-ok", &collection.Request{
		Method: "GET", URL: "{{host}}/ok",
		Asserts: []collection.ScriptAssert{
			{Name: "status 200", Expr: "res.status === 200"},
			{Name: "ok true", Expr: "res.body.ok === true"},
		},
	})

	rep, err := Run(context.Background(), c, Options{EnvName: "dev", Runner: runner.DefaultOptions()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Total != 1 || rep.Passed != 1 || rep.Failed != 0 {
		t.Fatalf("报告统计不符: %+v", rep)
	}
	if !rep.Cases[0].Passed || rep.Cases[0].Status != 200 {
		t.Fatalf("用例应通过: %+v", rep.Cases[0])
	}
	if !strings.Contains(rep.Text(), "PASS") {
		t.Fatalf("text 报告缺少 PASS: %s", rep.Text())
	}
	data, err := rep.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var parsed Report
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Passed != 1 {
		t.Fatalf("JSON 报告不可解析: %v %s", err, data)
	}
	xmlData, err := rep.JUnit()
	if err != nil {
		t.Fatalf("JUnit: %v", err)
	}
	if !strings.Contains(string(xmlData), "<testsuite") || !strings.Contains(string(xmlData), "01-ok") {
		t.Fatalf("JUnit 报告不符: %s", xmlData)
	}
}

func TestRunFailsOnAssertAndReportsJUnit(t *testing.T) {
	srv := newEchoServer(t)
	c := newRunCollection(t, srv)
	addRequest(t, c, "01-fail", &collection.Request{
		Method: "GET", URL: "{{host}}/ok",
		Asserts: []collection.ScriptAssert{{Name: "status 500", Expr: "res.status === 500"}},
	})

	rep, err := Run(context.Background(), c, Options{EnvName: "dev", Runner: runner.DefaultOptions()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Failed != 1 || rep.Cases[0].Passed {
		t.Fatalf("断言失败应标记用例失败: %+v", rep)
	}
	if !strings.Contains(rep.Text(), "FAIL") || !strings.Contains(rep.Text(), "status 500") {
		t.Fatalf("text 报告缺少失败明细: %s", rep.Text())
	}
	xmlData, err := rep.JUnit()
	if err != nil {
		t.Fatalf("JUnit: %v", err)
	}
	if !strings.Contains(string(xmlData), "<failure") {
		t.Fatalf("JUnit 应包含 failure: %s", xmlData)
	}
}

func TestRunFilterAndBail(t *testing.T) {
	srv := newEchoServer(t)
	c := newRunCollection(t, srv)
	fail := &collection.Request{
		Method: "GET", URL: "{{host}}/ok",
		Asserts: []collection.ScriptAssert{{Name: "fail", Expr: "res.status === 500"}},
	}
	pass := &collection.Request{
		Method: "GET", URL: "{{host}}/ok",
		Asserts: []collection.ScriptAssert{{Name: "pass", Expr: "res.status === 200"}},
	}
	addRequest(t, c, "01-fail", fail)
	addRequest(t, c, "02-pass", pass)
	addRequest(t, c, "03-pass", pass)

	rep, err := Run(context.Background(), c, Options{
		EnvName: "dev", Filter: "pass", Runner: runner.DefaultOptions(), Bail: true,
	})
	if err != nil {
		t.Fatalf("Run(filter): %v", err)
	}
	if rep.Total != 2 || rep.Passed != 2 {
		t.Fatalf("filter 应只跑 02/03: %+v", rep)
	}

	rep, err = Run(context.Background(), c, Options{
		EnvName: "dev", Runner: runner.DefaultOptions(), Bail: true,
	})
	if err != nil {
		t.Fatalf("Run(bail): %v", err)
	}
	if rep.Total != 1 || rep.Failed != 1 {
		t.Fatalf("bail 应在首个失败后停止: %+v", rep)
	}

	if _, err := Run(context.Background(), c, Options{
		EnvName: "dev", Filter: "no-such-request", Runner: runner.DefaultOptions(),
	}); err == nil {
		t.Fatal("filter 无匹配时应报错，避免 CI 空跑绿")
	}
}

func TestRunChainsScriptVars(t *testing.T) {
	srv := newEchoServer(t)
	c := newRunCollection(t, srv)
	addRequest(t, c, "01-token", &collection.Request{
		Method: "GET", URL: "{{host}}/token",
		Script: &collection.ScriptBlock{PostResponse: "bru.setVar('token', res.body.token)"},
	})
	addRequest(t, c, "02-next", &collection.Request{
		Method: "GET", URL: "{{host}}/next?token={{token}}",
		Asserts: []collection.ScriptAssert{
			{Name: "query has token", Expr: "res.body.query.indexOf('token=abc') >= 0"},
		},
	})

	rep, err := Run(context.Background(), c, Options{EnvName: "dev", Runner: runner.DefaultOptions()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Total != 2 || rep.Passed != 2 {
		t.Fatalf("脚本变量应链式传给下一条请求: %+v", rep)
	}
	if !strings.Contains(rep.Cases[1].URL, "{{token}}") {
		t.Fatalf("报告应保留模板 URL（不渲染 secret）: %s", rep.Cases[1].URL)
	}
}

func TestRunTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := newRunCollection(t, srv)
	addRequest(t, c, "01-slow", &collection.Request{Method: "GET", URL: "{{host}}/slow"})

	rep, err := Run(context.Background(), c, Options{
		EnvName: "dev", Runner: runner.DefaultOptions(), Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Failed != 1 || rep.Cases[0].Error == "" {
		t.Fatalf("超时应标记失败: %+v", rep.Cases[0])
	}
}

func TestRunMissingEnv(t *testing.T) {
	srv := newEchoServer(t)
	c := newRunCollection(t, srv)
	addRequest(t, c, "01-ok", &collection.Request{Method: "GET", URL: "{{host}}/ok"})
	if _, err := Run(context.Background(), c, Options{EnvName: "missing", Runner: runner.DefaultOptions()}); err == nil {
		t.Fatal("指定不存在的环境应报错")
	}
}
