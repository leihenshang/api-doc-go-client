// mcpserver 把 api-doc-go 集合（项目）暴露给 MCP 客户端（Claude Desktop / Cursor / Cline 等 AI 工具）。
//
// 用法：
//
//	mcpserver -root D:\collections            # stdio（本地 AI 工具挂载，默认）
//	mcpserver -root D:\collections -http 127.0.0.1:8189   # 追加 streamable HTTP（仅本机）
//	mcpserver -root D:\collections -http 0.0.0.0:8189 -http-token <token>   # 跨主机（WSL / 局域网）
//	mcpserver -root D:\collections -readonly  # 只注册只读工具
//
// 跨主机（WSL2 / 局域网）要点：
//   - 绑 0.0.0.0 才不被回环限制；不给 -http-token 会自动生成并打印一个（不给就裸奔太危险）。
//   - WSL2 两种网络模式的连法不同：
//     mirrored（Windows 11 22H2+ / WSL 2.0+，推荐）：WSL 与 Windows 共用网络命名空间，
//     在 WSL 里直接连 127.0.0.1:<端口>（或 Windows 的局域网 IP）即可。
//     NAT（默认的老模式）：WSL 在 172.x 虚拟网段，要用宿主地址 —— 在 WSL 里执行
//     `ip route show default | awk '{print $3}'`（mirrored 下这条拿到的是路由器，不适用）。
//   - Windows 防火墙首次会弹窗询问，选「专用网络」放行（或手动放行该端口）。
//
// 项目 = -root 下含 opencollection.yml 的子目录，可用 list_projects 查看。
//
// 依赖说明：MCP Go SDK 需从 goproxy.cn 拉取（proxy.golang.org 在部分网络不可达）：
//
//	GOPROXY=https://goproxy.cn,direct go get github.com/modelcontextprotocol/go-sdk@latest
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"api-doc-go-client/internal/mcp"
)

// splitAndTrim 逗号分隔字符串 → 去空去白的切片。
func splitAndTrim(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func main() {
	var (
		root        = flag.String("root", "", "项目根目录（其下含 opencollection.yml 的子目录即一个项目）")
		addr        = flag.String("http", "", "启用 streamable HTTP 的监听地址，如 127.0.0.1:8189；留空 = 只走 stdio")
		readOnly    = flag.Bool("readonly", false, "只读模式：不注册创建/修改/删除类工具")
		httpToken   = flag.String("http-token", "", "HTTP 传输的 Bearer 令牌；绑非回环地址时不给则自动生成并打印")
		allowOrigin = flag.String("http-allow-origin", "", "允许的浏览器来源（Origin），逗号分隔；* = 任意。用于浏览器端客户端与 DNS rebinding 防护")
	)
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "错误：需要 -root 指定项目根目录")
		flag.Usage()
		os.Exit(2)
	}

	// 日志一律走 stderr：stdio 传输下 stdout 属于 JSON-RPC 协议，写日志会破坏会话
	logger := log.New(os.Stderr, "mcpserver ", log.LstdFlags)

	server, svc, err := mcp.NewServer(mcp.Config{Root: *root, ReadOnly: *readOnly, Logger: logger})
	if err != nil {
		logger.Fatalf("启动失败: %v", err)
	}
	projects := len(svc.Projects())
	logger.Printf("已扫描项目根 %s：%d 个项目（%s）", *root, projects, map[bool]string{true: "只读", false: "读写"}[*readOnly])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *addr != "" {
		// 跨主机（绑非回环）必须带令牌：同网段/WSL 侧任何进程都能调工具，没令牌等于把集合交出去。
		opts := mcp.HTTPOptions{
			Addr:         *addr,
			Path:         "/mcp",
			Token:        *httpToken,
			AllowOrigins: splitAndTrim(*allowOrigin),
			Logger:       logger,
		}
		if opts.Token == "" && !mcp.IsLoopbackAddr(*addr) {
			opts.Token = mcp.NewToken()
			logger.Printf("未指定 -http-token，已自动生成令牌（见下）；仅回环监听可省略该参数")
		}
		if opts.Token != "" {
			logger.Printf("HTTP 令牌（客户端需配置 Authorization: Bearer <下面这串>）: %s", opts.Token)
		} else {
			logger.Printf("警告: 未设置令牌，任何能连到该端口的进程都能读写集合（仅建议本机/受信网络这样用）")
		}
		if len(opts.AllowOrigins) > 0 {
			logger.Printf("允许的浏览器来源: %s", strings.Join(opts.AllowOrigins, ", "))
		}

		// 两种传输同时提供。注意：进程存活要等信号，不能等 stdio 返回 ——
		// 只开 HTTP 时 stdin 往往是关的/立即 EOF，stdio 会马上返回，若据此退出就会把 HTTP 带走。
		go func() {
			logger.Printf("HTTP 传输监听 %s/mcp（健康检查 %s/healthz）", opts.Addr, opts.Addr)
			if err := mcp.ServeHTTP(ctx, server, opts); err != nil && ctx.Err() == nil {
				logger.Printf("HTTP 传输退出: %v", err)
			}
		}()
		go func() {
			if err := mcp.ServeStdio(ctx, server); err != nil && ctx.Err() == nil {
				logger.Printf("stdio 传输退出: %v", err)
			}
		}()
		<-ctx.Done()
		logger.Println("已退出")
		return
	}
	if err := mcp.ServeStdio(ctx, server); err != nil && ctx.Err() == nil {
		logger.Fatalf("stdio 传输退出: %v", err)
	}
	logger.Println("已退出")
}
