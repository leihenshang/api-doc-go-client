package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gRPC 请求文件 round-trip：落盘 → 扫描读回，字段不丢；http 段整体省略。
func TestGRPCRequestFileRoundTrip(t *testing.T) {
	c := newTestCollection(t)
	src := &Request{
		GRPC: &GrpcBlock{
			Target:   "{{host}}",
			Service:  "demo.Greeter",
			Method:   "SayHello",
			Proto:    "protos/greeter.proto",
			Imports:  []string{"protos"},
			Metadata: []KV{{Name: "authorization", Value: "Bearer {{token}}", Enabled: true}},
			Message:  "{\n  \"name\": \"world\"\n}",
			Stream:   "unary",
			TLS:      &GrpcTLS{Mode: "tls", CA: "certs/ca.pem"},
		},
		Docs: "打招呼",
	}
	created, err := c.CreateRequestFromDraft("", "SayHello", src)
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	if created.Method != MethodGRPC {
		t.Errorf("Method = %q，期望 %q", created.Method, MethodGRPC)
	}
	if created.URL != "grpc://{{host}}/demo.Greeter/SayHello" {
		t.Errorf("URL = %q，期望派生的 grpc:// 地址", created.URL)
	}

	raw, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(created.Path)))
	if err != nil {
		t.Fatalf("读文件失败: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "type: grpc") {
		t.Errorf("磁盘应写 type: grpc:\n%s", text)
	}
	if strings.Contains(text, "http:") {
		t.Errorf("gRPC 文件不应写出空 http 段:\n%s", text)
	}
	for _, want := range []string{"service: demo.Greeter", "method: SayHello", "protos/greeter.proto", "mode: tls"} {
		if !strings.Contains(text, want) {
			t.Errorf("磁盘缺少 %q:\n%s", want, text)
		}
	}

	res, err := c.scan()
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	back := res.reqs[created.UID]
	if back == nil {
		t.Fatalf("扫描结果里没有该 gRPC 请求: %+v", res.reqs)
	}
	if !back.IsGRPC() || back.GRPC == nil {
		t.Fatalf("读回后不是 gRPC 请求: %+v", back)
	}
	if back.Method != MethodGRPC || back.URL != created.URL {
		t.Errorf("派生字段不一致: method=%q url=%q", back.Method, back.URL)
	}
	g := back.GRPC
	if g.Target != "{{host}}" || g.Service != "demo.Greeter" || g.Method != "SayHello" {
		t.Errorf("grpc 段丢失: %+v", g)
	}
	if g.Proto != "protos/greeter.proto" || len(g.Imports) != 1 || g.Imports[0] != "protos" {
		t.Errorf("定义路径/import 路径丢失: %+v", g)
	}
	if len(g.Metadata) != 1 || g.Metadata[0].Value != "Bearer {{token}}" {
		t.Errorf("元数据丢失: %+v", g.Metadata)
	}
	if g.Stream != "unary" || g.TLS == nil || g.TLS.Mode != "tls" || g.TLS.CA != "certs/ca.pem" {
		t.Errorf("流式/TLS 设置丢失: %+v", g)
	}
	if back.Docs != "打招呼" {
		t.Errorf("docs 丢失: %q", back.Docs)
	}
}

// HTTP 请求文件的 http 段必须照旧写出（omitempty 不能误伤）。
func TestHTTPRequestFileKeepsHTTPBlock(t *testing.T) {
	c := newTestCollection(t)
	created, err := c.CreateRequestFromDraft("", "ping", &Request{Method: "GET", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(created.Path)))
	if err != nil {
		t.Fatalf("读文件失败: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "type: http") || !strings.Contains(text, "url: https://example.com") {
		t.Errorf("HTTP 文件内容异常:\n%s", text)
	}
	if strings.Contains(text, "grpc:") {
		t.Errorf("HTTP 文件不应写出 grpc 段:\n%s", text)
	}
}

func TestGrpcURL(t *testing.T) {
	if got := GrpcURL("127.0.0.1:50051", "demo.Greeter", "SayHello"); got != "grpc://127.0.0.1:50051/demo.Greeter/SayHello" {
		t.Errorf("GrpcURL = %q", got)
	}
	if got := GrpcURL("", "demo.Greeter", "SayHello"); got != "" {
		t.Errorf("目标为空时应返回空: %q", got)
	}
}

// P8：集合级默认定义 —— 请求不写 proto 时回落；写盘时与默认一致的定义被省略。
func TestGrpcCollectionDefault(t *testing.T) {
	c := newTestCollection(t)
	if err := c.SetGrpcDefault("protos/greeter.proto", []string{"vendor/proto", "vendor/proto"}); err != nil {
		t.Fatalf("写默认定义失败: %v", err)
	}
	got := c.GrpcDefault()
	if got == nil || got.Proto != "protos/greeter.proto" || len(got.Imports) != 1 {
		t.Fatalf("默认定义不符（应去重）: %+v", got)
	}
	// 清单里应该看得见
	data, err := os.ReadFile(filepath.Join(c.Dir, "opencollection.yml"))
	if err != nil {
		t.Fatalf("读清单失败: %v", err)
	}
	if !strings.Contains(string(data), "protos/greeter.proto") {
		t.Fatalf("清单未写出默认定义:\n%s", data)
	}

	// 请求不写 proto：读回时应回落成默认定义
	created, err := c.CreateRequestFromDraft("", "SayHello", &Request{
		GRPC: &GrpcBlock{Target: "localhost:50051", Service: "demo.Greeter", Method: "SayHello"},
	})
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	file, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(created.Path)))
	if err != nil {
		t.Fatalf("读请求文件失败: %v", err)
	}
	if strings.Contains(string(file), "proto:") {
		t.Errorf("与默认一致的定义不应写进请求文件:\n%s", file)
	}
	res, err := c.scan()
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	back := res.reqs[created.UID]
	if back == nil || back.GRPC == nil {
		t.Fatalf("扫描结果里没有该请求: %+v", back)
	}
	if back.GRPC.Proto != "protos/greeter.proto" || len(back.GRPC.Imports) != 1 {
		t.Fatalf("未回落到集合默认定义: %+v", back.GRPC)
	}

	// 清除默认定义后：请求文件里的 proto 重新出现（这里只是确认清理后不再回落）
	if err := c.SetGrpcDefault("", nil); err != nil {
		t.Fatalf("清除默认定义失败: %v", err)
	}
	if c.GrpcDefault() != nil {
		t.Errorf("清除后应为 nil: %+v", c.GrpcDefault())
	}
}
