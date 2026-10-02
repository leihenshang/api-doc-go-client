// Command grpcfixture 是 gRPC 功能的本地联调服务端（设计文档 P9）：
// 按给定 .proto 动态收发（`UnknownServiceHandler`，不依赖生成代码），覆盖 4 种方法形态、
// 非 OK 状态码、metadata 回显与尾元数据，可选 TLS。用法：
//
//	go run ./cmd/grpcfixture -proto protos/greeter.proto -imports protos -addr 127.0.0.1:50051
//	go run ./cmd/grpcfixture -proto protos/greeter.proto -tls-cert cert.pem -tls-key key.pem
//
// 约定（写请求消息时用）：
//
//	name = "boom"  → NOT_FOUND(5)（验证错误码展示）
//	name = "slow"  → 服务端睡 3 秒（验证请求级超时 → DEADLINE_EXCEEDED(4)）
//	name = 其它    → 回显：把请求里同名字段原样塞回响应 + message = "hello <name>"
//
// 响应固定带初始元数据 x-server: grpcfixture（并把请求里的 x-token 回显成 x-echo-token）
// 与尾元数据 x-trailer: t1 / x-echo-name: <name>。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	grpcproto "api-doc-go-client/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip" // 注册 gzip 解压器：验证请求压缩（G7.5）
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// serverStreamCount server-stream / bidi 场景下服务端回几条。
const serverStreamCount = 3

func main() {
	protoFile := flag.String("proto", "", "入口 .proto 路径（必填）")
	imports := flag.String("imports", "", "import 搜索路径（逗号分隔）")
	addr := flag.String("addr", "127.0.0.1:50051", "监听地址")
	certFile := flag.String("tls-cert", "", "服务端证书（PEM；与 -tls-key 同时给出则启用 TLS）")
	keyFile := flag.String("tls-key", "", "服务端私钥（PEM）")
	flag.Parse()

	if strings.TrimSpace(*protoFile) == "" {
		log.Fatal("缺少 -proto（入口 .proto 路径）")
	}
	paths := []string{}
	for _, p := range strings.Split(*imports, ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	res, err := grpcproto.Compile(context.Background(), []string{*protoFile}, paths)
	if err != nil {
		log.Fatalf("编译 %s 失败: %v", *protoFile, err)
	}
	srv, err := newServer(res, *certFile, *keyFile)
	if err != nil {
		log.Fatalf("创建服务端失败: %v", err)
	}
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("监听 %s 失败: %v", *addr, err)
	}
	svcs := make([]string, 0, len(res.Services))
	for _, s := range res.Services {
		for _, m := range s.Methods {
			svcs = append(svcs, fmt.Sprintf("%s/%s(%s)", s.Name, m.Name, m.Stream))
		}
	}
	log.Printf("grpcfixture 监听 %s（定义 %s，TLS=%v）", lis.Addr(), *protoFile, *certFile != "")
	log.Printf("可用方法: %s", strings.Join(svcs, ", "))
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("Serve 失败: %v", err)
	}
	_ = os.Stdout.Sync()
}

// newServer 按定义创建服务端；cert/key 都给出时启用 TLS。
func newServer(res *grpcproto.Result, certFile, keyFile string) (*grpc.Server, error) {
	opts := []grpc.ServerOption{grpc.UnknownServiceHandler(handler(res))}
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("TLS 需要同时提供 -tls-cert 与 -tls-key")
		}
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.Creds(creds))
	}
	return grpc.NewServer(opts...), nil
}

// handler 按方法形态动态收发：一元 / 服务端流 / 客户端流 / 双向流。
func handler(res *grpcproto.Result) grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		full, ok := grpc.MethodFromServerStream(stream)
		if !ok {
			return status.Error(codes.Internal, "取不到方法名")
		}
		svc, method, ok := strings.Cut(strings.TrimPrefix(full, "/"), "/")
		if !ok {
			return status.Errorf(codes.Unimplemented, "方法名异常: %q", full)
		}
		md, err := res.MethodDescriptor(svc, method)
		if err != nil {
			return status.Errorf(codes.Unimplemented, "%v", err)
		}
		if err := sendPrelude(stream, md); err != nil {
			return err
		}
		switch {
		case md.IsStreamingClient() && md.IsStreamingServer(): // bidi：逐条回显
			return echoBidi(stream, md)
		case md.IsStreamingClient(): // 客户端流：收完再回一条
			return collectAndReply(stream, md)
		case md.IsStreamingServer(): // 服务端流：回 serverStreamCount 条
			return streamReplies(stream, md)
		default:
			return unary(stream, md)
		}
	}
}

// sendPrelude 发送初始元数据 + 设置尾元数据（所有形态一致，便于断言）。
func sendPrelude(stream grpc.ServerStream, md protoreflect.MethodDescriptor) error {
	header := metadata.Pairs("x-server", "grpcfixture", "x-method-stream", streamKind(md))
	if incoming, ok := metadata.FromIncomingContext(stream.Context()); ok {
		if values := incoming.Get("x-token"); len(values) > 0 {
			header.Set("x-echo-token", values[0])
		}
	}
	if err := stream.SendHeader(header); err != nil {
		return err
	}
	stream.SetTrailer(metadata.Pairs("x-trailer", "t1"))
	return nil
}

// recvRequest 收一条请求消息，并处理 boom / slow 约定。
func recvRequest(stream grpc.ServerStream, md protoreflect.MethodDescriptor) (*dynamicpb.Message, error) {
	in := dynamicpb.NewMessage(md.Input())
	if err := stream.RecvMsg(in); err != nil {
		return nil, err
	}
	switch fieldString(in, "name") {
	case "boom":
		stream.SetTrailer(metadata.Pairs("x-trailer", "t1", "x-error", "boom"))
		return nil, status.Error(codes.NotFound, "no such thing")
	case "slow":
		time.Sleep(3 * time.Second)
	}
	return in, nil
}

func unary(stream grpc.ServerStream, md protoreflect.MethodDescriptor) error {
	in, err := recvRequest(stream, md)
	if err != nil {
		return err
	}
	out := replyFor(md, in)
	setTrailers(stream, fieldString(in, "name"))
	return stream.SendMsg(out)
}

func streamReplies(stream grpc.ServerStream, md protoreflect.MethodDescriptor) error {
	in, err := recvRequest(stream, md)
	if err != nil {
		return err
	}
	name := fieldString(in, "name")
	for i := 1; i <= serverStreamCount; i++ {
		out := replyFor(md, in)
		setString(out, "message", fmt.Sprintf("hello %s #%d", name, i))
		if err := stream.SendMsg(out); err != nil {
			return err
		}
	}
	setTrailers(stream, name)
	return nil
}

func collectAndReply(stream grpc.ServerStream, md protoreflect.MethodDescriptor) error {
	count, lastName := 0, ""
	for {
		in := dynamicpb.NewMessage(md.Input())
		err := stream.RecvMsg(in)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		count++
		lastName = fieldString(in, "name")
	}
	out := dynamicpb.NewMessage(md.Output())
	setString(out, "message", fmt.Sprintf("collected %d message(s) from %s", count, lastName))
	setTrailers(stream, lastName)
	return stream.SendMsg(out)
}

func echoBidi(stream grpc.ServerStream, md protoreflect.MethodDescriptor) error {
	lastName := ""
	for {
		in := dynamicpb.NewMessage(md.Input())
		err := stream.RecvMsg(in)
		if errors.Is(err, io.EOF) {
			setTrailers(stream, lastName)
			return nil
		}
		if err != nil {
			return err
		}
		lastName = fieldString(in, "name")
		if err := stream.SendMsg(replyFor(md, in)); err != nil {
			return err
		}
	}
}

// replyFor 构造响应：把请求里同名字段原样回填，再补 message = "hello <name>"。
func replyFor(md protoreflect.MethodDescriptor, in *dynamicpb.Message) *dynamicpb.Message {
	out := dynamicpb.NewMessage(md.Output())
	in.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if out.Descriptor().Fields().ByName(fd.Name()) != nil {
			out.Set(fd, v)
		}
		return true
	})
	setString(out, "message", fmt.Sprintf("hello %s", fieldString(in, "name")))
	return out
}

func setTrailers(stream grpc.ServerStream, name string) {
	stream.SetTrailer(metadata.Pairs("x-trailer", "t1", "x-echo-name", name))
}

func streamKind(md protoreflect.MethodDescriptor) string {
	switch {
	case md.IsStreamingClient() && md.IsStreamingServer():
		return "bidi"
	case md.IsStreamingClient():
		return "client"
	case md.IsStreamingServer():
		return "server"
	default:
		return "unary"
	}
}

func fieldString(m protoreflect.Message, name string) string {
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return ""
	}
	return m.Get(fd).String()
}

func setString(m *dynamicpb.Message, name, v string) {
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return
	}
	m.Set(fd, protoreflect.ValueOfString(v))
}
