package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

const greeterProto = `syntax = "proto3";

package demo;

// 打招呼服务
service Greeter {
  // 一元
  rpc SayHello(HelloRequest) returns (HelloReply);
  rpc Watch(HelloRequest) returns (stream HelloReply);
}

message HelloRequest {
  string name = 1;
  int32 age = 2;
}

message HelloReply {
  string message = 1;
}
`

// 打开一个临时集合（devserver 模式：PickDirectory 直接返回该目录）。
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	core := NewApp()
	core.SetHeadlessDir(dir)
	if _, err := core.OpenCollection(dir); err != nil {
		t.Fatalf("打开集合失败: %v", err)
	}
	return core, dir
}

func writeProto(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写 %s 失败: %v", name, err)
	}
	return p
}

// 导入原子化（D4）：解析失败 → 报错且不落盘。
func TestGrpcImportAtomicOnParseError(t *testing.T) {
	core, dir := newTestApp(t)
	broken := writeProto(t, "broken.proto", "syntax = \"proto3\";\npackage demo;\nmessage Broken {\n  string name =\n}\n")

	if _, err := core.ImportGrpcProtos([]string{broken}, nil); err == nil {
		t.Fatal("解析失败应报错")
	} else if !strings.Contains(err.Error(), "broken.proto") {
		t.Errorf("错误应指出文件: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "protos"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("读 protos 目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("解析失败不应落盘，实际有 %d 个文件", len(entries))
	}
}

func TestGrpcImportListSampleRemove(t *testing.T) {
	core, dir := newTestApp(t)
	info, err := core.ImportGrpcProtos([]string{writeProto(t, "greeter.proto", greeterProto)}, nil)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if info.Proto != "protos/greeter.proto" {
		t.Errorf("入口定义 = %q，期望 protos/greeter.proto", info.Proto)
	}
	if len(info.Protos) != 1 || info.Protos[0].Rel != "protos/greeter.proto" {
		t.Errorf("定义清单异常: %+v", info.Protos)
	}
	if len(info.Services) != 1 {
		t.Fatalf("服务数 = %d，期望 1", len(info.Services))
	}
	svc := info.Services[0]
	if svc.Name != "demo.Greeter" || len(svc.Methods) != 2 {
		t.Fatalf("服务/方法清单异常: %+v", svc)
	}
	if svc.Methods[1].Stream != "server" {
		t.Errorf("Watch 应识别为服务端流: %+v", svc.Methods[1])
	}
	if !strings.Contains(svc.Comment, "打招呼服务") {
		t.Errorf("服务注释未取到: %q", svc.Comment)
	}
	if _, err := os.Stat(filepath.Join(dir, "protos", "greeter.proto")); err != nil {
		t.Errorf("定义未复制进集合: %v", err)
	}

	list, err := core.ListGrpcProtos()
	if err != nil || len(list) != 1 {
		t.Fatalf("列举定义异常: %v %+v", err, list)
	}

	sample, err := core.GrpcSampleMessage("protos/greeter.proto", nil, "demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("生成样例失败: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("样例不是合法 JSON: %v\n%s", err, sample)
	}
	if _, ok := got["name"]; !ok {
		t.Errorf("样例缺少 name: %s", sample)
	}

	// 换了集合定义后再解析：应命中缓存（同样的入参 → 结果一致）
	schema, err := core.LoadGrpcSchema("protos/greeter.proto", nil)
	if err != nil || len(schema.Services) != 1 {
		t.Fatalf("重新解析失败: %v %+v", err, schema)
	}

	if err := core.RemoveGrpcProto("protos/greeter.proto"); err != nil {
		t.Fatalf("删除定义失败: %v", err)
	}
	if list, _ := core.ListGrpcProtos(); len(list) != 0 {
		t.Errorf("删除后应为空: %+v", list)
	}
}

// 发送分派：gRPC 请求走 gRPC 通路（错误来自描述符解析，而不是 HTTP 拨号）。
func TestGrpcSendDispatch(t *testing.T) {
	core, _ := newTestApp(t)
	if _, err := core.ImportGrpcProtos([]string{writeProto(t, "greeter.proto", greeterProto)}, nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	req := &collection.Request{
		Name: "SayHello",
		GRPC: &collection.GrpcBlock{
			Target:  "127.0.0.1:1",
			Proto:   "protos/greeter.proto",
			Service: "demo.Nope",
			Method:  "SayHello",
		},
	}
	_, err := core.SendRequest(req, "")
	if err == nil {
		t.Fatal("不存在的服务应报错")
	}
	if !strings.Contains(err.Error(), "没有找到服务") {
		t.Errorf("应走 gRPC 解析通路，实际错误: %v", err)
	}
}

// 定义缺失/未导入：给出可定位的提示。
func TestGrpcLoadMissingProto(t *testing.T) {
	core, _ := newTestApp(t)
	if _, err := core.LoadGrpcSchema("protos/absent.proto", nil); err == nil {
		t.Fatal("定义不存在应报错")
	} else if !strings.Contains(err.Error(), "absent.proto") {
		t.Errorf("错误应指出路径: %v", err)
	}
	if _, err := core.LoadGrpcSchema("", nil); err == nil {
		t.Error("空路径应报错")
	}
}

// 发送时定义未导入：同样给出可定位的提示。
func TestGrpcSendWithoutProto(t *testing.T) {
	core, _ := newTestApp(t)
	req := &collection.Request{GRPC: &collection.GrpcBlock{Target: "127.0.0.1:1", Service: "demo.Greeter", Method: "SayHello"}}
	if _, err := core.SendRequest(req, ""); err == nil {
		t.Fatal("未导入定义应报错")
	} else if !strings.Contains(err.Error(), "定义") {
		t.Errorf("错误应提示先导入定义: %v", err)
	}
}

// Message 分段的后端支撑（G5.3 校验 / G5.4 字段提示）。
func TestGrpcMessageFieldsAndValidate(t *testing.T) {
	core, _ := newTestApp(t)
	if _, err := core.ImportGrpcProtos([]string{writeProto(t, "greeter.proto", greeterProto)}, nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	fields, err := core.GrpcMessageFields("protos/greeter.proto", nil, "demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("取字段失败: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("字段数 = %d，期望 2: %+v", len(fields), fields)
	}
	if fields[0].Name != "name" || fields[0].JSONName != "name" || fields[0].Type != "string" {
		t.Errorf("name 字段不符: %+v", fields[0])
	}
	if fields[1].Type != "int32" {
		t.Errorf("age 类型不符: %+v", fields[1])
	}

	// 校验：空消息通过 / 合法通过 / 类型错误带回字段路径
	if msg, err := core.GrpcValidateMessage("protos/greeter.proto", nil, "demo.Greeter", "SayHello", "  "); err != nil || msg != "" {
		t.Errorf("空消息应通过: msg=%q err=%v", msg, err)
	}
	if msg, err := core.GrpcValidateMessage("protos/greeter.proto", nil, "demo.Greeter", "SayHello", `{"name":"alice","age":3}`); err != nil || msg != "" {
		t.Errorf("合法消息应通过: msg=%q err=%v", msg, err)
	}
	if msg, err := core.GrpcValidateMessage("protos/greeter.proto", nil, "demo.Greeter", "SayHello", `{"age":"x"}`); err != nil {
		t.Fatalf("校验不应把定义问题当错误: %v", err)
	} else if !strings.Contains(msg, "age") {
		t.Errorf("校验错误应带字段路径: %q", msg)
	}

	// 方法不存在：属于「定义问题」，走 error 而不是校验文案
	if _, err := core.GrpcMessageFields("protos/greeter.proto", nil, "demo.Greeter", "Nope"); err == nil {
		t.Error("方法不存在应报错")
	}
	if _, err := core.GrpcValidateMessage("protos/greeter.proto", nil, "demo.Greeter", "Nope", "{}"); err == nil {
		t.Error("方法不存在时校验应报错")
	}
}

// P8 + G3.5 + G11.5：集合默认定义、保存门禁、grpcurl 代码生成。
func TestGrpcDefaultSaveGuardAndCodegen(t *testing.T) {
	core, _ := newTestApp(t)
	if _, err := core.ImportGrpcProtos([]string{writeProto(t, "greeter.proto", greeterProto)}, nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	// P8：写默认定义 → 读回；请求不写定义也能通过保存校验
	if err := core.SetGrpcDefault("protos/greeter.proto", []string{"vendor/x"}); err != nil {
		t.Fatalf("写默认定义失败: %v", err)
	}
	def, err := core.GetGrpcDefault()
	if err != nil || def == nil || def.Proto != "protos/greeter.proto" {
		t.Fatalf("默认定义不符: %+v err=%v", def, err)
	}

	// G3.5：写了定义但解析不了 → 拒绝保存；空定义 / 可解析定义 → 允许
	bad := &collection.Request{GRPC: &collection.GrpcBlock{Target: "127.0.0.1:1", Service: "demo.Greeter", Method: "SayHello", Proto: "protos/absent.proto"}}
	if err := core.SaveRequest(bad); err == nil {
		t.Error("定义缺失时应拒绝保存")
	} else if !strings.Contains(err.Error(), "无法保存") {
		t.Errorf("错误应说明无法保存: %v", err)
	}
	empty := &collection.Request{GRPC: &collection.GrpcBlock{Target: "127.0.0.1:1", Service: "demo.Greeter", Method: "SayHello"}}
	if err := core.SaveRequest(empty); err == nil {
		t.Error("空定义应允许保存（稍后补定义）")
	}
	ok := &collection.Request{UID: "u-1", Name: "SayHello", Path: "SayHello.yml",
		GRPC: &collection.GrpcBlock{Target: "{{host}}", Service: "demo.Greeter", Method: "SayHello", Proto: "protos/greeter.proto", Message: `{"name":"{{who}}"}`}}
	if err := core.SaveRequest(ok); err != nil {
		t.Errorf("可解析定义应允许保存: %v", err)
	}

	// G11.5：gRPC 只生成 grpcurl（含变量渲染、明文标记）
	code, err := core.GenerateCode("grpcurl", "", ok)
	if err != nil {
		t.Fatalf("生成 grpcurl 失败: %v", err)
	}
	for _, want := range []string{"grpcurl ", "-plaintext", "-import-path 'protos'", "-proto 'greeter.proto'", `'{{host}}'`, "'demo.Greeter/SayHello'"} {
		if !strings.Contains(code, want) {
			t.Errorf("grpcurl 片段缺少 %q:\n%s", want, code)
		}
	}
	if _, err := core.GenerateCode("curl", "", ok); err == nil {
		t.Error("gRPC 请求生成 curl 应报错")
	}

	// 清除默认定义后读到 nil
	if err := core.SetGrpcDefault("", nil); err != nil {
		t.Fatalf("清除失败: %v", err)
	}
	if def, err := core.GetGrpcDefault(); err != nil || def != nil {
		t.Errorf("清除后应为 nil: %+v err=%v", def, err)
	}
}
