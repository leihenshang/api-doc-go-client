package main

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
	grpcproto "api-doc-go-client/internal/proto"
	"api-doc-go-client/internal/runner"

	"google.golang.org/grpc/codes"
)

// 用仓库里的 greeter.proto 夹具（同时是客户端单测与手工联调用的同一份定义）。
func fixtureProto(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "internal", "proto", "testdata", "greeter.proto"))
	if err != nil {
		t.Fatalf("解析夹具路径失败: %v", err)
	}
	return abs
}

func startFixture(t *testing.T) (string, *grpcproto.Result, func()) {
	t.Helper()
	proto := fixtureProto(t)
	res, err := grpcproto.Compile(context.Background(), []string{proto}, []string{filepath.Dir(proto)})
	if err != nil {
		t.Fatalf("编译夹具失败: %v", err)
	}
	srv, err := newServer(res, "", "")
	if err != nil {
		t.Fatalf("创建服务端失败: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	return lis.Addr().String(), res, func() {
		srv.Stop()
		_ = lis.Close()
	}
}

func kv(rows []collection.KV, name string) string {
	for _, row := range rows {
		if row.Name == name {
			return row.Value
		}
	}
	return ""
}

// 一元调用：回显、初始元数据、尾元数据；以及 boom → NOT_FOUND。
func TestFixtureUnaryAndError(t *testing.T) {
	addr, res, stop := startFixture(t)
	defer stop()
	md, err := res.MethodDescriptor("demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("取方法失败: %v", err)
	}

	out, err := runner.SendGRPC(context.Background(), runner.GrpcRequest{
		Target:   addr,
		Method:   md,
		Message:  `{"name":"alice"}`,
		Metadata: []collection.KV{{Name: "x-token", Value: "t-123", Enabled: true}},
		Types:    res.Types,
	}, nil, runner.DefaultOptions())
	if err != nil {
		t.Fatalf("一元调用失败: %v", err)
	}
	if out.Status != int(codes.OK) || !strings.Contains(out.Body, "hello alice") {
		t.Errorf("一元结果异常: status=%d body=%s", out.Status, out.Body)
	}
	if got := kv(out.Headers, "x-echo-token"); got != "t-123" {
		t.Errorf("初始元数据未回显 x-token: %+v", out.Headers)
	}
	if got := kv(out.Trailers, "x-echo-name"); got != "alice" {
		t.Errorf("尾元数据缺失 x-echo-name: %+v", out.Trailers)
	}

	bad, err := runner.SendGRPC(context.Background(), runner.GrpcRequest{
		Target: addr, Method: md, Message: `{"name":"boom"}`, Types: res.Types,
	}, nil, runner.DefaultOptions())
	if err != nil {
		t.Fatalf("非 OK 状态应作为结果返回: %v", err)
	}
	if bad.Status != int(codes.NotFound) || !strings.Contains(bad.Body, "NOT_FOUND") {
		t.Errorf("错误码结果异常: status=%d body=%s", bad.Status, bad.Body)
	}
}

// gzip 请求压缩：服务端注册了解压器，应正常返回（G7.5）。
func TestFixtureGzip(t *testing.T) {
	addr, res, stop := startFixture(t)
	defer stop()
	md, err := res.MethodDescriptor("demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("取方法失败: %v", err)
	}
	out, err := runner.SendGRPC(context.Background(), runner.GrpcRequest{
		Target: addr, Method: md, Message: `{"name":"zip"}`, Compress: "gzip", Types: res.Types,
	}, nil, runner.DefaultOptions())
	if err != nil {
		t.Fatalf("压缩调用失败: %v", err)
	}
	if out.Status != int(codes.OK) || !strings.Contains(out.Body, "hello zip") {
		t.Errorf("压缩调用结果异常: status=%d body=%s", out.Status, out.Body)
	}
}

// 流式方法：执行器当前明确报「不支持流式」（P5 落地前），夹具侧已实现 4 种形态。
func TestFixtureStreamingMethodGuard(t *testing.T) {
	addr, res, stop := startFixture(t)
	defer stop()
	for _, name := range []string{"ListHellos", "CollectHellos", "Chat"} {
		md, err := res.MethodDescriptor("demo.Greeter", name)
		if err != nil {
			t.Fatalf("取方法 %s 失败: %v", name, err)
		}
		_, err = runner.SendGRPC(context.Background(), runner.GrpcRequest{
			Target: addr, Method: md, Message: `{"name":"x"}`, Types: res.Types,
		}, nil, runner.DefaultOptions())
		if err == nil || !strings.Contains(err.Error(), "流式") {
			t.Errorf("%s 应报「暂不支持流式」，实际: %v", name, err)
		}
	}
}

// 定义缺失：给出可定位的提示（不是 panic）。
func TestFixtureMethodMissing(t *testing.T) {
	_, res, stop := startFixture(t)
	defer stop()
	if _, err := res.MethodDescriptor("demo.Greeter", "Nope"); err == nil {
		t.Error("不存在的方法应报错")
	}
	// handler 对未知服务/方法返回 UNIMPLEMENTED（这里直接验证描述符查询失败路径）
	if _, err := res.MethodDescriptor("demo.Absent", "Ping"); err == nil {
		t.Error("不存在的服务应报错")
	}
}
