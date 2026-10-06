package mcp_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "api-doc-go-client/internal/mcp" //nolint:revive // 与同目录测试文件保持一致的调用写法

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain 把本目录测试的配置目录挪到临时目录。
//
// 这些用例会真的打开集合（写索引文件、必要时建默认环境），不隔离就会写进用户真实的配置目录
// （%AppData%\api-doc-client）—— 之前没隔离，跑一次测试就在用户配置目录里留下几个索引文件。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "api-doc-mcp-test-")
	if err != nil {
		panic(err)
	}
	// 唯一在 Windows 与类 Unix 都生效的隔离开关（见 config.EnvDirOverride）
	os.Setenv("API_DOC_CONFIG_DIR", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// silentLogger 测试用静默 logger。
func silentLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// sessionProjectDir 造一个最小可打开的集合目录（含 manifest）。
func sessionProjectDir(t *testing.T, root, name, uid string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "opencollection: 1.0.0\ninfo:\n    name: " + name + "\nmeta:\n    uid: " + uid + "\n"
	if err := os.WriteFile(filepath.Join(dir, "opencollection.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// connectHTTP 连一条**真实 HTTP 会话**：会话 id 由 SDK 随机分配（与生产 HTTP 传输一致）。
// 内存传输拿不到会话 id（ServerSession.ID() 返回空串），所以「按会话隔离」只能用 HTTP 验证。
//
// DisableStandaloneSSE：工具是被动调用、不需要服务端主动推送，而那条常驻 SSE 流会让
// httptest.Server.Close 一直等它结束（实测把整包测试拖到 10 分钟超时）。
func connectHTTP(t *testing.T, ts *httptest.Server) *mcp.ClientSession {
	t.Helper()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{
			Endpoint:             ts.URL,
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatalf("建立 HTTP 会话: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// defaultLine 取 list_workspaces 输出里「当前默认目标：…」那一行（没有则空串）。
func defaultLine(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, "当前默认目标：") {
			return ln
		}
	}
	return ""
}

// callTool 调一个工具，返回纯文本输出与「是否业务错误」。
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("调用 %s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

// 默认工作目录是**会话级**的：同一个进程服务多个客户端（各自带 Mcp-Session-Id）时互不影响。
//
// 以前存在 Service 里（进程级单例），HTTP 下 A 选定后 B 的「省略 project」会跟着落到 A 的目录。
func TestWorkspaceSelectionIsPerSession(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	projA := sessionProjectDir(t, rootA, "alpha", "uid-alpha")
	projB := sessionProjectDir(t, rootB, "beta", "uid-beta")
	server, _, err := NewServer(Config{
		Allow:  []AllowDir{{Path: rootA, Writable: true}, {Path: rootB, Writable: true}},
		NewApp: testNewApp,
		Logger: silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer ts.Close()

	sessA := connectHTTP(t, ts)
	sessB := connectHTTP(t, ts)
	sessC := connectHTTP(t, ts) // 第三个会话全程不选：不该沾到前两个的选择

	// 谁都没选：省略 project 应被拒并提示 use_workspace
	if text, isErr := callTool(t, sessA, "get_project_modules", map[string]any{}); !isErr || !strings.Contains(text, "use_workspace") {
		t.Fatalf("未选定会话应报错提示 use_workspace：isErr=%v text=%s", isErr, text)
	}

	// 两个会话各选自己的目录
	if text, isErr := callTool(t, sessA, "use_workspace", map[string]any{"workspace": projA}); isErr {
		t.Fatalf("会话 A 选定失败: %s", text)
	}
	if text, isErr := callTool(t, sessB, "use_workspace", map[string]any{"workspace": projB}); isErr {
		t.Fatalf("会话 B 选定失败: %s", text)
	}

	// 省略 project：各自落到自己选的目录（既不是对方的，也不是「最后选的那个人」）
	aText, aErr := callTool(t, sessA, "get_project_modules", map[string]any{})
	if aErr || !strings.Contains(aText, "alpha") || strings.Contains(aText, "beta") {
		t.Fatalf("会话 A 应落到 alpha：isErr=%v text=%s", aErr, aText)
	}
	bText, bErr := callTool(t, sessB, "get_project_modules", map[string]any{})
	if bErr || !strings.Contains(bText, "beta") || strings.Contains(bText, "alpha") {
		t.Fatalf("会话 B 应落到 beta：isErr=%v text=%s", bErr, bText)
	}

	// list_workspaces 的「当前默认」也只标自己那个；没选过的会话要显示「尚未选定」
	if ws, _ := callTool(t, sessA, "list_workspaces", nil); defaultLine(ws) == "" || !strings.Contains(defaultLine(ws), "alpha") {
		t.Fatalf("会话 A 的 list_workspaces 应把自己选的标为默认：%s", ws)
	}
	if ws, _ := callTool(t, sessC, "list_workspaces", nil); !strings.Contains(ws, "尚未选定默认工作目录") {
		t.Fatalf("没选过的会话不该有默认目标：%s", ws)
	}
	if text, isErr := callTool(t, sessC, "get_project_modules", map[string]any{}); !isErr {
		t.Fatalf("没选过的会话不该继承别人的默认目标：%s", text)
	}

	// 显式传 project 仍然优先，且不该改掉本会话的默认目标
	if text, isErr := callTool(t, sessA, "get_project_modules", map[string]any{"project": projB}); isErr || !strings.Contains(text, projB) {
		t.Fatalf("显式 project 应生效：isErr=%v text=%s", isErr, text)
	}
	if text, _ := callTool(t, sessA, "get_project_modules", map[string]any{}); !strings.Contains(text, "alpha") {
		t.Fatalf("显式传 project 不该改掉本会话默认：%s", text)
	}
}
