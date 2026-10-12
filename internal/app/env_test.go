package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// 环境变量整链：SaveEnv 落盘 → envVars(envName) 读到 → SendRequest 把 {{name}} 解析后真实发出。
// 这条链上任何一环断掉（环境没选中、envVars 拿不到、runner 不解析），用户看到的就是
// 一句 `unsupported protocol scheme ""`（URL 还是 {{name}}/... 形态）。
func TestEnvVarResolvesAndSends(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	core, _ := newTestApp(t)
	if err := core.SaveEnv(&collection.Env{
		Name: "dev",
		Vars: []collection.Var{{Name: "host1", Value: srv.URL, Enabled: true}},
	}); err != nil {
		t.Fatalf("保存环境失败: %v", err)
	}

	vars, err := core.envVars("dev")
	if err != nil {
		t.Fatalf("envVars 失败: %v", err)
	}
	if vars["host1"] != srv.URL {
		t.Fatalf("envVars 未包含 host1: %+v", vars)
	}

	req := &collection.Request{
		Name: "导出模板",
		Method: "GET",
		URL:    "{{host1}}/apis/linkingthing.com/ipam/ipv6-filing/v1/selfownedfilings?action=export_template",
	}
	res, err := core.SendRequest(req, "dev")
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("状态码异常: %d", res.Status)
	}
	if !strings.HasPrefix(res.URL, srv.URL) {
		t.Fatalf("URL 未被变量渲染: %s", res.URL)
	}
}

// 没选环境（envName 为空）时只给内置动态变量：URL 里的 {{name}} 原样保留 → 无协议头，
// 必须在发送前报「变量未定义 + 检查环境」，而不是把底层协议头错误抛给用户。
func TestSendWithoutEnvGivesClearError(t *testing.T) {
	core, _ := newTestApp(t)
	req := &collection.Request{Name: "x", Method: "GET", URL: "{{host1}}/apis/x?action=export_template"}
	_, err := core.SendRequest(req, "")
	if err == nil {
		t.Fatal("未选环境且变量未定义时应报错")
	}
	if !strings.Contains(err.Error(), "host1") || !strings.Contains(err.Error(), "变量未定义") {
		t.Fatalf("错误应点名变量并说明原因: %v", err)
	}
	if strings.Contains(err.Error(), "unsupported protocol scheme") {
		t.Fatalf("不应把底层协议头错误抛给用户: %v", err)
	}
}
