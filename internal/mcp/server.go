package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 版本号（与 go.mod 的 module 版本无关，单独维护；发布时同步）。
const serverVersion = "0.1.0"

// toolTimeout 单个工具的执行上限：防止「发送一个永远不响应的地址」把会话挂死。
const toolTimeout = 3 * time.Minute

// Config 启动配置。
type Config struct {
	Root     string // 项目根目录
	ReadOnly bool   // 只读模式：不注册写工具，send_request 不落盘
	Logger   *log.Logger
	// NewApp 构造项目级运行时（必填）：*app.App 满足 ProjectApp。
	// 由调用方注入以避免 internal/mcp 与 internal/app 互相依赖。
	NewApp NewAppFunc
}

// NewServer 装配 MCP 服务器（注册全部工具）。
func NewServer(cfg Config) (*mcp.Server, *Service, error) {
	reg, err := NewRegistry(cfg.Root, cfg.NewApp)
	if err != nil {
		return nil, nil, err
	}
	svc := NewService(reg)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "api-doc-go",
		Title:   "api-doc-go 接口集合",
		Version: serverVersion,
	}, nil)
	RegisterTools(server, svc, cfg.ReadOnly)
	return server, svc, nil
}

// ServeStdio 通过标准输入输出服务（本地 AI 工具挂载的默认方式）。
//
// 注意：stdio 传输下 stdout 属于协议，日志必须走 stderr（cmd/mcpserver 用 log 默认输出）。
func ServeStdio(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}

// HTTPOptions HTTP 传输配置。跨主机（WSL / 局域网另一台机器）使用时至少要设 Token。
type HTTPOptions struct {
	Addr string // 监听地址：仅本机 127.0.0.1:8189；跨主机 0.0.0.0:8189
	Path string // 挂载路径，留空 = /mcp
	// Token 非空时每个请求都必须带 `Authorization: Bearer <token>`。
	// 绑非回环地址时 cmd/mcpserver 会自动生成一个（见 main.go），避免裸奔。
	Token string
	// AllowOrigins 允许的浏览器来源（精确匹配或 *）。作用有两个：
	//  ① DNS rebinding 防护 —— 恶意网页能让浏览器对内网地址发请求，Origin 必须显式允许；
	//  ② 浏览器端 MCP 客户端要读响应，需要配套的 CORS 响应头。
	// 非浏览器客户端（Claude Desktop / cursor / 命令行）不带 Origin，不受影响。
	AllowOrigins []string
	Logger       *log.Logger
}

// ServeHTTP 以 streamable HTTP 方式服务（同源多会话由 SDK 管理）。
//
// 安全默认：Token 非空 → 校验 Authorization；请求带 Origin → 必须命中 AllowOrigins（DNS rebinding 防护）。
// 健康检查 /healthz 恒开且只回版本信息，方便 WSL/远端先探通端口再谈鉴权。
func ServeHTTP(ctx context.Context, server *mcp.Server, opts HTTPOptions) error {
	if opts.Path == "" {
		opts.Path = "/mcp"
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	opts.Addr = normalizeAddr(opts.Addr)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.Handle(opts.Path, withHTTPGuard(handler, opts, logger))
	srv := &http.Server{Addr: opts.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	// 不设 ReadTimeout/WriteTimeout：SSE 是长连接，写超时会掐断正常的流式会话。
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return <-errCh
	}
}

// healthz 存活探针：不要求令牌（只回版本，不泄露项目/路径），远端先 curl 它判断网络与端口通不通。
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write([]byte(`{"ok":true,"service":"api-doc-go","version":"` + serverVersion + `"}`))
}

// withHTTPGuard 给 MCP 端点套上「令牌 + 来源」校验；OPTIONS 预检与允许的来源回 CORS 头。
func withHTTPGuard(next http.Handler, opts HTTPOptions, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		originOK := originAllowed(origin, opts.AllowOrigins)
		// 带 Origin 的请求 = 来自浏览器里的脚本，必须显式允许（DNS rebinding 防护）；
		// 不带 Origin 的（非浏览器 MCP 客户端）不受来源限制。
		if origin != "" && !originOK {
			logger.Printf("拒绝来自 %s 的请求（Origin 不在允许列表内）", origin)
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if originOK && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			// CORS 预检：浏览器端 MCP 客户端握手前会先发 OPTIONS
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", strings.Join([]string{
				"Authorization", "Content-Type", "Last-Event-ID",
				"Mcp-Session-Id", "Mcp-Protocol-Version",
			}, ", "))
			w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if opts.Token != "" && !tokenOK(r.Header.Get("Authorization"), opts.Token) {
			logger.Printf("拒绝 %s %s：缺少或错误的 Bearer 令牌", r.Method, r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="api-doc-go"`)
			http.Error(w, "unauthorized: 需要 Authorization: Bearer <token>", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK 校验 `Authorization: Bearer <token>`（前缀大小写不敏感 + 定长比较，避免时序侧信道）。
func tokenOK(header, want string) bool {
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(header[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// originAllowed 来源白名单：空 Origin（非浏览器客户端）放行；allow 为空 → 拒绝一切带 Origin 的请求。
func originAllowed(origin string, allow []string) bool {
	if origin == "" {
		return true
	}
	if len(allow) == 0 {
		return false
	}
	normal := func(s string) string { return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "/") }
	for _, a := range allow {
		a = strings.TrimSpace(a)
		if a == "*" || normal(a) == normal(origin) {
			return true
		}
	}
	return false
}

// normalizeAddr 补全监听地址：空 → :8189；只给端口 → 0.0.0.0:端口（跨主机可达）。
func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "0.0.0.0:8189" // 与只给端口一样是全网卡；写全是为了启动日志里能直接看出监听范围
	}
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}

// ToolCount 返回已注册的工具数量（自检/测试用）。
func ToolCount(ctx context.Context, server *mcp.Server) (int, error) {
	tools, err := ListToolNames(ctx, server)
	if err != nil {
		return 0, err
	}
	return len(tools), nil
}

// ListToolNames 用内存传输自检：返回已注册的工具名列表。
func ListToolNames(ctx context.Context, server *mcp.Server) ([]string, error) {
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "selfcheck", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		return nil, fmt.Errorf("自检会话建立失败: %w", err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, t.Name)
	}
	return out, nil
}
