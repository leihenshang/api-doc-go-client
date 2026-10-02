package runner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"api-doc-go-client/internal/collection"
	grpcproto "api-doc-go-client/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// ---- fixture：编译 echo.proto + 起一个真实 TCP 的 gRPC 服务 ----

func compileEcho(t *testing.T) *grpcproto.Result {
	t.Helper()
	res, err := grpcproto.Compile(context.Background(), []string{filepath.Join("testdata", "echo.proto")}, []string{"testdata"})
	if err != nil {
		t.Fatalf("编译 testdata/echo.proto 失败: %v", err)
	}
	return res
}

func echoMethod(t *testing.T, res *grpcproto.Result, name string) protoreflect.MethodDescriptor {
	t.Helper()
	md, err := res.MethodDescriptor("fixture.Echo", name)
	if err != nil {
		t.Fatalf("查方法 %s 失败: %v", name, err)
	}
	return md
}

// startEchoServer 起服务：未知方法走动态处理器（不依赖生成代码）。
func startEchoServer(t *testing.T, res *grpcproto.Result, creds credentials.TransportCredentials) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	opts := []grpc.ServerOption{grpc.UnknownServiceHandler(echoHandler(res))}
	if creds != nil {
		opts = append(opts, grpc.Creds(creds))
	}
	srv := grpc.NewServer(opts...)
	go func() { _ = srv.Serve(lis) }()
	return lis.Addr().String(), func() {
		srv.Stop()
		_ = lis.Close()
	}
}

// echoHandler 按方法名从描述符取输入/输出类型动态收发。
func echoHandler(res *grpcproto.Result) grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		full, hasMethod := grpc.MethodFromServerStream(stream)
		if !hasMethod {
			return status.Error(codes.Internal, "取不到方法名")
		}
		full = strings.TrimPrefix(full, "/")
		svc, method, ok := strings.Cut(full, "/")
		if !ok {
			return status.Errorf(codes.Unimplemented, "方法名异常: %q", full)
		}
		md, err := res.MethodDescriptor(svc, method)
		if err != nil {
			return status.Errorf(codes.Unimplemented, "%v", err)
		}
		in := dynamicpb.NewMessage(md.Input())
		if err := stream.RecvMsg(in); err != nil {
			return err
		}
		switch fieldString(in, "text") {
		case "boom":
			return status.Error(codes.NotFound, "no such thing")
		case "slow":
			time.Sleep(400 * time.Millisecond)
		}
		header := metadata.Pairs("x-server", "fixture")
		if incoming, ok := metadata.FromIncomingContext(stream.Context()); ok && len(incoming.Get("x-token")) > 0 {
			header.Set("x-echo-token", incoming.Get("x-token")[0])
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		stream.SetTrailer(metadata.Pairs("x-trailer", "t1"))

		out := dynamicpb.NewMessage(md.Output())
		in.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if out.Descriptor().Fields().ByName(fd.Name()) != nil {
				out.Set(fd, v)
			}
			return true
		})
		return stream.SendMsg(out)
	}
}

func fieldString(m protoreflect.Message, name protoreflect.Name) string {
	fd := m.Descriptor().Fields().ByName(name)
	if fd == nil {
		return ""
	}
	return m.Get(fd).String()
}

func kvValue(rows []collection.KV, name string) string {
	for _, kv := range rows {
		if kv.Name == name {
			return kv.Value
		}
	}
	return ""
}

// ---- 用例 ----

func TestSendGRPCUnary(t *testing.T) {
	res := compileEcho(t)
	addr, stop := startEchoServer(t, res, nil)
	defer stop()

	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target:   addr,
		Method:   echoMethod(t, res, "Ping"),
		Metadata: []collection.KV{{Name: "x-token", Value: "{{tok}}", Enabled: true}, {Name: "skip", Value: "x", Enabled: false}},
		Message:  `{"text":"hello","n":7,"tags":["a"]}`,
		Types:    res.Types,
	}, map[string]string{"tok": "abc"}, DefaultOptions())
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if out.Status != int(codes.OK) {
		t.Errorf("状态码 = %d，期望 0(OK)", out.Status)
	}
	if out.Proto != "gRPC" {
		t.Errorf("Proto = %q，期望 gRPC", out.Proto)
	}
	if !strings.Contains(out.URL, addr) || !strings.Contains(out.URL, "fixture.Echo/Ping") {
		t.Errorf("URL = %q，期望形如 grpc://host:port/包.服务/方法", out.URL)
	}
	var echo map[string]any
	if err := json.Unmarshal([]byte(out.Body), &echo); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, out.Body)
	}
	if echo["text"] != "hello" {
		t.Errorf("回显 text = %v，期望 hello", echo["text"])
	}
	if got := kvValue(out.Headers, "x-echo-token"); got != "abc" {
		t.Errorf("元数据未按变量渲染或未回带: %+v", out.Headers)
	}
	if got := kvValue(out.Trailers, "x-trailer"); got != "t1" {
		t.Errorf("尾元数据缺失: %+v", out.Trailers)
	}
	if out.Size <= 0 {
		t.Errorf("Size = %d，期望 > 0", out.Size)
	}
	if out.ContentType != grpcContentType {
		t.Errorf("ContentType = %q", out.ContentType)
	}
}

func TestSendGRPCEmptyMessageAllowed(t *testing.T) {
	res := compileEcho(t)
	addr, stop := startEchoServer(t, res, nil)
	defer stop()

	out, err := SendGRPC(context.Background(), GrpcRequest{Target: addr, Method: echoMethod(t, res, "Ping")}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("空消息应可发送: %v", err)
	}
	if out.Status != int(codes.OK) {
		t.Errorf("状态码 = %d，期望 OK", out.Status)
	}
}

// 非 OK 状态是「结果」而不是 error：状态码进 Status，详情进 Body。
func TestSendGRPCStatusNotFound(t *testing.T) {
	res := compileEcho(t)
	addr, stop := startEchoServer(t, res, nil)
	defer stop()

	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: addr, Method: echoMethod(t, res, "Boom"), Message: `{"text":"boom"}`,
	}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("非 OK 状态不应作为 error 返回: %v", err)
	}
	if out.Status != int(codes.NotFound) {
		t.Errorf("状态码 = %d，期望 %d(NOT_FOUND)", out.Status, codes.NotFound)
	}
	if !strings.Contains(out.Body, "no such thing") || !strings.Contains(out.Body, "NOT_FOUND") {
		t.Errorf("Body 应含状态名与消息: %s", out.Body)
	}
}

func TestSendGRPCDeadline(t *testing.T) {
	res := compileEcho(t)
	addr, stop := startEchoServer(t, res, nil)
	defer stop()

	opts := DefaultOptions()
	opts.Timeout = 150 * time.Millisecond
	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: addr, Method: echoMethod(t, res, "Slow"), Message: `{"text":"slow"}`,
	}, nil, opts)
	if err != nil {
		t.Fatalf("超时应作为状态返回: %v", err)
	}
	if out.Status != int(codes.DeadlineExceeded) {
		t.Errorf("状态码 = %d，期望 %d(DEADLINE_EXCEEDED)：%s", out.Status, codes.DeadlineExceeded, out.Body)
	}
}

// 连不上：也是状态（UNAVAILABLE），不是 error —— 响应面板照常显示状态徽章与原因。
func TestSendGRPCUnavailable(t *testing.T) {
	res := compileEcho(t)
	opts := DefaultOptions()
	opts.Timeout = 500 * time.Millisecond
	// 带 scheme 与路径的地址应被归一化成 host:port（否则会因「缺少端口」直接报错）
	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: "grpc://127.0.0.1:1/fixture.Echo/Ping", Method: echoMethod(t, res, "Ping"), Message: `{"text":"x"}`,
	}, nil, opts)
	if err != nil {
		t.Fatalf("连接失败应作为状态返回，实际 error: %v", err)
	}
	if out.Status != int(codes.Unavailable) {
		t.Errorf("状态码 = %d，期望 %d(UNAVAILABLE)", out.Status, codes.Unavailable)
	}
	if !strings.Contains(out.URL, "127.0.0.1:1") {
		t.Errorf("URL 应含归一化后的地址: %q", out.URL)
	}
}

func TestSendGRPCInvalidMessage(t *testing.T) {
	res := compileEcho(t)
	_, err := SendGRPC(context.Background(), GrpcRequest{
		Target: "127.0.0.1:1", Method: echoMethod(t, res, "Ping"), Message: `{"nope":1}`,
	}, nil, DefaultOptions())
	if err == nil {
		t.Fatal("未知字段应解析失败")
	}
	if !strings.Contains(err.Error(), "fixture.EchoMsg") {
		t.Errorf("错误应指出消息类型: %v", err)
	}
}

func TestSendGRPCBadTarget(t *testing.T) {
	res := compileEcho(t)
	for _, target := range []string{"", "localhost", "{{missing}}"} {
		if _, err := SendGRPC(context.Background(), GrpcRequest{Target: target, Method: echoMethod(t, res, "Ping")}, nil, DefaultOptions()); err == nil {
			t.Errorf("地址 %q 应报错", target)
		}
	}
}

func TestSendGRPCStreamingUnsupported(t *testing.T) {
	res := compileEcho(t)
	_, err := SendGRPC(context.Background(), GrpcRequest{
		Target: "127.0.0.1:1", Method: echoMethod(t, res, "Watch"),
	}, nil, DefaultOptions())
	if err == nil || !strings.Contains(err.Error(), "流式") {
		t.Errorf("流式方法应给出明确提示，实际: %v", err)
	}
}

func TestSendGRPCNilMethod(t *testing.T) {
	if _, err := SendGRPC(context.Background(), GrpcRequest{Target: "127.0.0.1:1"}, nil, DefaultOptions()); err == nil {
		t.Error("缺少方法描述符应报错")
	}
}

func TestSendGRPCTLS(t *testing.T) {
	res := compileEcho(t)
	certFile, keyFile := writeSelfSignedCert(t)
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		t.Fatalf("加载服务端证书失败: %v", err)
	}
	addr, stop := startEchoServer(t, res, creds)
	defer stop()

	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: addr, Method: echoMethod(t, res, "Ping"), Message: `{"text":"tls"}`,
		TLS: GrpcTLS{Mode: "tls", CA: certFile},
	}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("TLS 调用失败: %v", err)
	}
	if out.Status != int(codes.OK) {
		t.Errorf("状态码 = %d，期望 OK：%s", out.Status, out.Body)
	}
}

func TestSendGRPCTLSUntrusted(t *testing.T) {
	res := compileEcho(t)
	certFile, keyFile := writeSelfSignedCert(t)
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		t.Fatalf("加载服务端证书失败: %v", err)
	}
	addr, stop := startEchoServer(t, res, creds)
	defer stop()

	opts := DefaultOptions()
	opts.Timeout = 3 * time.Second
	// 不给 CA（也没跳过校验）→ 握手失败，作为 UNAVAILABLE 返回
	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: addr, Method: echoMethod(t, res, "Ping"), Message: `{"text":"tls"}`, TLS: GrpcTLS{Mode: "tls"},
	}, nil, opts)
	if err != nil {
		t.Fatalf("证书不受信应作为状态返回: %v", err)
	}
	if out.Status != int(codes.Unavailable) {
		t.Errorf("状态码 = %d，期望 UNAVAILABLE：%s", out.Status, out.Body)
	}
}

func TestTransportCredsValidation(t *testing.T) {
	if _, err := transportCreds(GrpcTLS{Mode: "mtls", Cert: "only-cert.pem"}, false); err == nil {
		t.Error("mTLS 缺私钥应报错")
	}
	if _, err := transportCreds(GrpcTLS{Mode: "tls", CA: filepath.Join(t.TempDir(), "absent.pem")}, false); err == nil {
		t.Error("CA 文件不存在应报错")
	}
	if _, err := transportCreds(GrpcTLS{Mode: "tls", CA: writeGarbagePEM(t)}, false); err == nil {
		t.Error("CA 无法解析应报错")
	}
	if creds, err := transportCreds(GrpcTLS{}, false); err != nil || creds == nil {
		t.Errorf("默认应为明文: creds=%v err=%v", creds, err)
	}
}

// ---- 证书辅助 ----

func writeSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fixture"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("自签证书失败: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	writePEM(t, certFile, "CERTIFICATE", der)
	writePEM(t, keyFile, "EC PRIVATE KEY", keyDER)
	return certFile, keyFile
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	buf := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("写 %s 失败: %v", path, err)
	}
}

func writeGarbagePEM(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(path, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	return path
}

// G10.1：请求消息里的 {{变量}} 在发送前替换；G8.3：发送字节数有值。
func TestSendGRPCResolvesMessageVars(t *testing.T) {
	res := compileEcho(t)
	addr, stop := startEchoServer(t, res, nil)
	defer stop()

	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target:  addr,
		Method:  echoMethod(t, res, "Ping"),
		Message: `{"text":"{{who}}","n":1}`,
	}, map[string]string{"who": "alice"}, DefaultOptions())
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if out.Status != int(codes.OK) {
		t.Fatalf("状态码 = %d，期望 OK：%s", out.Status, out.Body)
	}
	var echo map[string]any
	if err := json.Unmarshal([]byte(out.Body), &echo); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if echo["text"] != "alice" {
		t.Errorf("消息变量未替换：text = %v（原始消息 %q）", echo["text"], `{"text":"{{who}}","n":1}`)
	}
	if out.SentSize <= 0 {
		t.Errorf("SentSize = %d，期望 > 0", out.SentSize)
	}
}

// G7.5：gzip 压缩请求仍能正常调用（服务端解压）。
func TestSendGRPCGzipCompression(t *testing.T) {
	res := compileEcho(t)
	addr, stop := startEchoServer(t, res, nil)
	defer stop()

	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: addr, Method: echoMethod(t, res, "Ping"),
		Message: `{"text":"compressed"}`, Compress: "gzip",
	}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("压缩调用失败: %v", err)
	}
	if out.Status != int(codes.OK) || !strings.Contains(out.Body, "compressed") {
		t.Errorf("压缩调用结果异常: status=%d body=%s", out.Status, out.Body)
	}
}

// G7.3/G7.4：请求级 settings 覆盖全局（忽略证书校验、超时）。
func TestSendGRPCRequestSettingsOverride(t *testing.T) {
	// 超时合并：请求级 2s 覆盖默认 30s
	if got := MergeOptions(DefaultOptions(), &collection.RequestSettings{TimeoutSec: 2}).Timeout; got != 2*time.Second {
		t.Errorf("请求级超时未生效: %v", got)
	}
	if got := MergeOptions(DefaultOptions(), nil).Timeout; got != defaultTimeout {
		t.Errorf("无请求级设置时应取默认超时: %v", got)
	}

	// 自签 TLS：全局不跳过校验，但请求级 settings.InsecureSSL = true → 应握手成功
	res := compileEcho(t)
	certFile, keyFile := writeSelfSignedCert(t)
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		t.Fatalf("加载服务端证书失败: %v", err)
	}
	addr, stop := startEchoServer(t, res, creds)
	defer stop()

	skip := true
	out, err := SendGRPC(context.Background(), GrpcRequest{
		Target: addr, Method: echoMethod(t, res, "Ping"), Message: `{"text":"tls"}`,
		TLS:      GrpcTLS{Mode: "tls"},
		Settings: &collection.RequestSettings{InsecureSSL: &skip},
	}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if out.Status != int(codes.OK) {
		t.Errorf("请求级忽略证书校验未生效: status=%d body=%s", out.Status, out.Body)
	}
}
