package runner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/varx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	// gRPC 单条消息上限，对齐 HTTP 侧的 maxBodySize（10MB）
	maxGrpcMsgSize = 10 << 20
	// 响应类型标记：响应面板用它区分 gRPC 与 HTTP
	grpcContentType = "application/grpc+proto"
)

// GrpcTLS gRPC 连接的安全设置；Mode 为空/none 表示明文。
type GrpcTLS struct {
	Mode               string `json:"mode"` // none | tls | mtls
	CA                 string `json:"ca"`   // 服务端 CA（PEM 路径）
	Cert               string `json:"cert"` // 客户端证书（mTLS）
	Key                string `json:"key"`  // 客户端私钥（mTLS）
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
}

// GrpcRequest 一次 gRPC 调用所需的最小输入。
// 方法描述符由调用方（App）从 internal/proto 的编译结果里取 —— runner 不关心定义来源。
type GrpcRequest struct {
	Target   string                        // host:port（支持 {{变量}}）
	Method   protoreflect.MethodDescriptor // 目标方法（含入参/出参类型）
	Metadata []collection.KV               // 元数据（名称与值都支持变量）
	Message  string                        // 请求消息（protojson；支持 {{变量}}）
	TLS      GrpcTLS
	// Settings 请求级覆盖（超时 / 忽略证书校验）：与 HTTP 走同一套 mergeOptions 规则
	Settings *collection.RequestSettings
	// Compress 请求压缩：空 / identity = 不压缩；gzip = 压缩（G7.5）
	Compress string
	Types    *protoregistry.Types // protojson 解析 Any / 扩展（可为 nil）
}

// SendGRPC 发送一次 gRPC 调用（首期仅支持 unary）。
//
// 与 HTTP 的差异：**非 OK 的 gRPC 状态不算 error** —— 它以 Result.Status（gRPC code）+ 尾元数据
// 返回，错误详情写进 Body，让响应面板照常渲染状态徽章与详情；只有「没到服务端」的情况
// （地址非法 / 消息不合法 / 证书读不出来）才返回 error。
func SendGRPC(ctx context.Context, req GrpcRequest, vars map[string]string, opts Options) (*Result, error) {
	if req.Method == nil {
		return nil, errors.New("缺少 gRPC 方法（请先导入 .proto 并选择服务与方法）")
	}
	if req.Method.IsStreamingClient() || req.Method.IsStreamingServer() {
		return nil, fmt.Errorf("暂不支持流式方法 %s（当前版本仅支持一元调用）", req.Method.Name())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 请求级 settings 覆盖全局（超时 / 忽略证书校验）：与 HTTP 同一套规则（G7.3/G7.4）
	opts = MergeOptions(opts, req.Settings)
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	target, err := renderTarget(req.Target, vars)
	if err != nil {
		return nil, err
	}
	creds, err := transportCreds(req.TLS, opts.InsecureSSL)
	if err != nil {
		return nil, err
	}
	callOpts := []grpc.CallOption{}
	if strings.EqualFold(strings.TrimSpace(req.Compress), "gzip") {
		callOpts = append(callOpts, grpc.UseCompressor(gzip.Name)) // G7.5
	}
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGrpcMsgSize), grpc.MaxCallSendMsgSize(maxGrpcMsgSize)),
	)
	if err != nil {
		return nil, fmt.Errorf("建立 gRPC 连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	callCtx := withMetadata(ctx, req.Metadata, vars)
	// 请求消息里的 {{变量}} 先替换（G10.1）：与 HTTP 请求体同一套语义
	msgText, _ := varx.Resolve(req.Message, vars)
	reqMsg := dynamicpb.NewMessage(req.Method.Input())
	if strings.TrimSpace(msgText) != "" {
		if err := unmarshalMessage(req.Method, msgText, reqMsg, req.Types); err != nil {
			return nil, err
		}
	}
	// 发送字节数（G8.3）：序列化后的大小（压缩后实际线长会小于它，界面标注为消息大小）
	sentSize := 0
	if raw, merr := proto.Marshal(reqMsg); merr == nil {
		sentSize = len(raw)
	}

	respMsg := dynamicpb.NewMessage(req.Method.Output())
	var header, trailer metadata.MD
	start := time.Now()
	invokeErr := conn.Invoke(callCtx, grpcCallPath(req.Method), reqMsg, respMsg,
		append(callOpts, grpc.Header(&header), grpc.Trailer(&trailer))...)

	st := status.Convert(invokeErr)
	res := &Result{
		URL:         grpcURL(target, req.Method),
		Status:      int(st.Code()),
		Proto:       "gRPC",
		TimeMS:      time.Since(start).Milliseconds(),
		SentSize:    sentSize,
		ContentType: grpcContentType,
		Headers:     mdToKV(header),
		Trailers:    mdToKV(trailer),
	}
	if invokeErr != nil {
		res.Body = grpcErrorBody(st)
		return res, nil
	}
	body, err := marshalMessage(respMsg, req.Types)
	if err != nil {
		return nil, err
	}
	res.Body = body
	if raw, merr := proto.Marshal(respMsg); merr == nil {
		res.Size = len(raw)
	}
	return res, nil
}

// renderTarget 渲染变量并校验地址形态。
func renderTarget(target string, vars map[string]string) (string, error) {
	rendered, _ := varx.Resolve(target, vars)
	rendered = normalizeTarget(rendered)
	if rendered == "" {
		return "", errors.New("服务地址为空（形如 host:port）")
	}
	if !strings.Contains(rendered, ":") {
		return "", fmt.Errorf("服务地址缺少端口: %s（形如 localhost:50051）", rendered)
	}
	return rendered, nil
}

// normalizeTarget 容忍粘贴进来的 URL 形态：去掉 scheme 与多余的路径段。
//
//	grpc://localhost:50051/demo.Greeter  ->  localhost:50051
//	https://host:443/anything            ->  host:443
//	dns:///localhost:50051               ->  localhost:50051
func normalizeTarget(raw string) string {
	t := strings.TrimSpace(raw)
	lower := strings.ToLower(t)
	for _, scheme := range []string{"grpcs://", "grpc://", "https://", "http://"} {
		if strings.HasPrefix(lower, scheme) {
			t = t[len(scheme):]
			break
		}
	}
	if strings.HasPrefix(t, "dns:///") {
		t = strings.TrimPrefix(t, "dns:///")
	}
	if i := strings.Index(t, "/"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// transportCreds 把请求级 TLS 设置与全局「忽略证书校验」合成传输凭据。
func transportCreds(cfg GrpcTLS, globalInsecure bool) (credentials.TransportCredentials, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", "none", "plaintext":
		return insecure.NewCredentials(), nil
	}
	//nolint:gosec // 跳过校验由用户显式选择（全局设置或请求级设置）
	tlsCfg := &tls.Config{
		InsecureSkipVerify: globalInsecure || cfg.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	if cfg.CA != "" {
		pool, err := loadCertPool(cfg.CA)
		if err != nil {
			return nil, err
		}
		tlsCfg.RootCAs = pool
	}
	if cfg.Cert != "" || cfg.Key != "" {
		if cfg.Cert == "" || cfg.Key == "" {
			return nil, errors.New("mTLS 需要同时提供客户端证书与私钥")
		}
		pair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("读取客户端证书失败: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{pair}
	}
	return credentials.NewTLS(tlsCfg), nil
}

// loadCertPool 读 CA 证书（PEM）。
func loadCertPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 CA 证书失败: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA 证书无法解析（需要 PEM 格式）: %s", path)
	}
	return pool, nil
}

// withMetadata 渲染元数据（名称与值都支持变量）并挂到 context 上。
// grpc 侧会在发送时校验键名合法性，非法键会以状态错误的形式回到界面上。
func withMetadata(ctx context.Context, rows []collection.KV, vars map[string]string) context.Context {
	items := resolveKV(rows, vars)
	if len(items) == 0 {
		return ctx
	}
	pairs := make([]string, 0, len(items)*2)
	for _, kv := range items {
		pairs = append(pairs, kv.Name, kv.Value)
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...))
}

// unmarshalMessage 把 JSON 请求消息解析成动态消息（错误里带字段路径）。
func unmarshalMessage(md protoreflect.MethodDescriptor, text string, dst *dynamicpb.Message, types *protoregistry.Types) error {
	opts := protojson.UnmarshalOptions{DiscardUnknown: false}
	if types != nil {
		opts.Resolver = types
	}
	if err := opts.Unmarshal([]byte(text), dst); err != nil {
		return fmt.Errorf("请求消息不是合法的 %s JSON: %w", md.Input().FullName(), err)
	}
	return nil
}

// marshalMessage 动态消息 → protojson（缩进 2，与响应面板的 JSON 树一致）。
func marshalMessage(msg *dynamicpb.Message, types *protoregistry.Types) (string, error) {
	opts := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: false}
	if types != nil {
		opts.Resolver = types
	}
	out, err := opts.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("响应消息序列化失败: %w", err)
	}
	return string(out), nil
}

// grpcErrorBody 把非 OK 状态整理成 JSON：状态码名 + 消息 + 详情类型。
// 写进 Body 而不是返回 error，是为了让响应面板照常展示（状态徽章 + 详情内容）。
func grpcErrorBody(st *status.Status) string {
	payload := map[string]any{
		"code":     int32(st.Code()),
		"codeName": GrpcCodeName(st.Code()),
		"message":  st.Message(),
	}
	if details := st.Proto().GetDetails(); len(details) > 0 {
		urls := make([]string, 0, len(details))
		for _, d := range details {
			urls = append(urls, d.GetTypeUrl())
		}
		payload["details"] = urls
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return st.Message()
	}
	return string(out)
}

// mdToKV 元数据 → 可展示的 KV 行（同名多值用 ", " 连接，按键排序保证稳定）。
func mdToKV(md metadata.MD) []collection.KV {
	if len(md) == 0 {
		return nil
	}
	keys := make([]string, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]collection.KV, 0, len(keys))
	for _, k := range keys {
		out = append(out, collection.KV{Name: k, Value: strings.Join(md[k], ", "), Enabled: true})
	}
	return out
}

// grpcCodeNames gRPC 状态码的官方名（SCREAMING_SNAKE）：界面徽章与错误详情统一用它，
// 与 grpc-go 的 codes.Code.String()（CamelCase，如 NotFound）区分开。
var grpcCodeNames = map[codes.Code]string{
	codes.OK:                 "OK",
	codes.Canceled:           "CANCELLED",
	codes.Unknown:            "UNKNOWN",
	codes.InvalidArgument:    "INVALID_ARGUMENT",
	codes.DeadlineExceeded:   "DEADLINE_EXCEEDED",
	codes.NotFound:           "NOT_FOUND",
	codes.AlreadyExists:      "ALREADY_EXISTS",
	codes.PermissionDenied:   "PERMISSION_DENIED",
	codes.ResourceExhausted:  "RESOURCE_EXHAUSTED",
	codes.FailedPrecondition: "FAILED_PRECONDITION",
	codes.Aborted:            "ABORTED",
	codes.OutOfRange:         "OUT_OF_RANGE",
	codes.Unimplemented:      "UNIMPLEMENTED",
	codes.Internal:           "INTERNAL",
	codes.Unavailable:        "UNAVAILABLE",
	codes.DataLoss:           "DATA_LOSS",
	codes.Unauthenticated:    "UNAUTHENTICATED",
}

// GrpcCodeName 状态码官方名；未知码回落成 codes.Code 的默认字符串。
func GrpcCodeName(code codes.Code) string {
	if name, ok := grpcCodeNames[code]; ok {
		return name
	}
	return code.String()
}

// grpcCallPath 调用路径：/包.服务/方法（注意服务与方法之间是**斜杠**，写点号会被服务端
// 判为 malformed method name → UNIMPLEMENTED）。
func grpcCallPath(md protoreflect.MethodDescriptor) string {
	if svc, ok := md.Parent().(protoreflect.ServiceDescriptor); ok {
		return "/" + string(svc.FullName()) + "/" + string(md.Name())
	}
	full := string(md.FullName())
	if i := strings.LastIndex(full, "."); i >= 0 {
		return "/" + full[:i] + "/" + full[i+1:]
	}
	return "/" + full
}

// grpcURL 合成可搜索、可入历史的地址：grpc://host:port/包.服务/方法
func grpcURL(target string, md protoreflect.MethodDescriptor) string {
	return "grpc://" + target + grpcCallPath(md)
}
