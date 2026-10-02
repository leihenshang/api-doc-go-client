package proto

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func testdata(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("解析测试文件路径失败: %v", err)
	}
	return abs
}

func testdataDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("解析 testdata 目录失败: %v", err)
	}
	return abs
}

func mustCompileGreeter(t *testing.T) *Result {
	t.Helper()
	res, err := Compile(context.Background(), []string{testdata(t, "greeter.proto")}, []string{testdataDir(t)})
	if err != nil {
		t.Fatalf("编译 greeter.proto 失败: %v", err)
	}
	return res
}

func TestCompileListsServicesAndMethods(t *testing.T) {
	res := mustCompileGreeter(t)
	if len(res.Services) != 1 {
		t.Fatalf("服务数 = %d，期望 1", len(res.Services))
	}
	svc := res.Services[0]
	if svc.Name != "demo.Greeter" {
		t.Errorf("服务名 = %q，期望 demo.Greeter", svc.Name)
	}
	if !strings.Contains(svc.Comment, "打招呼服务") {
		t.Errorf("服务注释未取到: %q", svc.Comment)
	}
	if len(svc.Methods) != 4 {
		t.Fatalf("方法数 = %d，期望 4", len(svc.Methods))
	}
	wantStream := map[string]string{
		"SayHello":      "unary",
		"ListHellos":    "server",
		"CollectHellos": "client",
		"Chat":          "bidi",
	}
	for _, m := range svc.Methods {
		if got := wantStream[m.Name]; got != m.Stream {
			t.Errorf("%s 形态 = %q，期望 %q", m.Name, m.Stream, got)
		}
		if m.FullName != "demo.Greeter."+m.Name {
			t.Errorf("%s 完整名 = %q", m.Name, m.FullName)
		}
	}
	say := svc.Methods[0]
	if say.Name != "SayHello" {
		t.Fatalf("首个方法应为 SayHello，实际 %q", say.Name)
	}
	if !strings.Contains(say.Comment, "一元") {
		t.Errorf("方法注释未取到: %q", say.Comment)
	}
	if say.Input != "demo.HelloRequest" || say.Output != "demo.HelloReply" {
		t.Errorf("入参/出参类型 = %q / %q", say.Input, say.Output)
	}
	if say.ClientStreaming || say.ServerStreaming {
		t.Errorf("SayHello 不应是流式")
	}
}

// 只给文件、不给 import 路径：文件所在目录应被临时加入（用户直接点选单个文件即可编译）
func TestCompileWithoutImportPath(t *testing.T) {
	res, err := Compile(context.Background(), []string{testdata(t, "greeter.proto")}, nil)
	if err != nil {
		t.Fatalf("无 import 路径时编译失败: %v", err)
	}
	if len(res.Services) != 1 || res.Services[0].Name != "demo.Greeter" {
		t.Errorf("服务清单异常: %+v", res.Services)
	}
}

func TestCompileMissingImport(t *testing.T) {
	_, err := Compile(context.Background(), []string{testdata(t, "missing_import.proto")}, []string{testdataDir(t)})
	if err == nil {
		t.Fatal("import 缺失时应报错")
	}
	if !strings.Contains(err.Error(), "absent.proto") {
		t.Errorf("错误信息应指出缺失文件，实际: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "解析 .proto 失败") {
		t.Errorf("错误应带统一前缀，实际: %v", err)
	}
}

func TestCompileSyntaxError(t *testing.T) {
	_, err := Compile(context.Background(), []string{testdata(t, "broken.proto")}, []string{testdataDir(t)})
	if err == nil {
		t.Fatal("语法错误时应报错")
	}
	if !strings.Contains(err.Error(), "broken.proto") {
		t.Errorf("错误信息应带文件名，实际: %v", err)
	}
	if !strings.Contains(err.Error(), ":") {
		t.Errorf("错误信息应带行列号，实际: %v", err)
	}
}

func TestCompileWithoutFiles(t *testing.T) {
	if _, err := Compile(context.Background(), nil, nil); err == nil {
		t.Error("未选择文件时应报错")
	}
}

func TestDescriptorLookup(t *testing.T) {
	res := mustCompileGreeter(t)

	md, err := res.MethodDescriptor("demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("按完整服务名查方法失败: %v", err)
	}
	if string(md.Name()) != "SayHello" {
		t.Errorf("方法名 = %q", md.Name())
	}
	// 短名（末段）也应可查，UI 里选择器给出的是完整名，但用户手填可能只写短名
	if _, err := res.MethodDescriptor("Greeter", "SayHello"); err != nil {
		t.Errorf("按短服务名查方法失败: %v", err)
	}
	if _, err := res.MethodDescriptor("demo.Greeter", "Nope"); err == nil {
		t.Error("不存在的方法应报错")
	}
	if _, err := res.MethodDescriptor("demo.Nope", "SayHello"); err == nil {
		t.Error("不存在的服务应报错")
	}

	msg, err := res.MessageDescriptor("demo.HelloRequest")
	if err != nil {
		t.Fatalf("查消息失败: %v", err)
	}
	if msg.Fields().ByName("name") == nil {
		t.Error("HelloRequest 应有 name 字段")
	}
	if _, err := res.MessageDescriptor("demo.Nope"); err == nil {
		t.Error("不存在的消息应报错")
	}
}

func TestServiceByNameShort(t *testing.T) {
	res := mustCompileGreeter(t)
	svc, ok := res.ServiceByName("Greeter")
	if !ok || svc.Name != "demo.Greeter" {
		t.Errorf("短名查询失败: ok=%v svc=%+v", ok, svc)
	}
	if _, ok := res.ServiceByName(""); ok {
		t.Error("空名字不应命中")
	}
}
