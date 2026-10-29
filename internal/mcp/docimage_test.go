package mcp_test

// MCP 图片工具 save_doc_image 的端到端回归：
// 保存 base64 → 返回 /doc-assets/<sha256>；同图去重；落盘位置在项目 assets/；
// 非法输入（空 / 非 base64 / SVG / 超限）明确报错；只读模式不注册该工具。

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appmcp "api-doc-go-client/internal/mcp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// 与 Go 其它测试、前端 e2e 共用同一张 1×1 PNG。
const sdTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII="

var sdURLRe = regexp.MustCompile(`^/doc-assets/[0-9a-f]{64}$`)

func TestSaveDocImageTool(t *testing.T) {
	cs := connectSelf(t)

	out := mustCall(t, cs, "save_doc_image", map[string]any{
		"project": "demo", "data": sdTestPNG, "name": "示意图",
	})
	if !strings.Contains(out, "/doc-assets/") || !strings.Contains(out, "![示意图](/doc-assets/") {
		t.Fatalf("返回应包含 URL 与 Markdown 片段：\n%s", out)
	}
	url := fieldAfter(out, "已保存图片：")
	if !sdURLRe.MatchString(url) {
		t.Fatalf("URL 形态不符：%q（输出：%s）", url, out)
	}
	// 同图重复保存：内容寻址，URL 不变
	again := mustCall(t, cs, "save_doc_image", map[string]any{"project": "demo", "data": sdTestPNG})
	if got := fieldAfter(again, "已保存图片："); got != url {
		t.Errorf("同图应去重：%q != %q", got, url)
	}

	// 可直接写进文档（说明该 URL 是正文可用的引用）
	callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "带图文档", "content": "![示意图](" + url + ")",
	})
	docs := mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "带图文档"})
	uid := uidAfter(t, docs)
	read := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(read, url) {
		t.Errorf("文档正文应保留图片 URL：\n%s", read)
	}
}

func TestSaveDocImageToolRejects(t *testing.T) {
	cs := connectSelf(t)
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"空数据", map[string]any{"project": "demo", "data": ""}, "data 不能为空"},
		{"非 base64", map[string]any{"project": "demo", "data": "!!!not-base64!!!"}, "解码失败"},
		{"SVG", map[string]any{
			"project": "demo",
			"data":    base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)),
		}, "不支持"},
		{"超限", map[string]any{
			"project": "demo",
			"data":    base64.StdEncoding.EncodeToString(make([]byte, 5<<20+1)),
		}, "过大"},
	}
	for _, c := range cases {
		out, isErr := callTool(t, cs, "save_doc_image", c.args)
		if !isErr || !strings.Contains(out, c.want) {
			t.Errorf("%s：期望错误包含 %q，实际 isErr=%v 输出=%s", c.name, c.want, isErr, out)
		}
	}
}

// 只读模式不应该暴露任何写工具（含 save_doc_image）。
func TestSaveDocImageToolAbsentInReadOnly(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	reg, err := appmcp.NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "ro", Version: "0"}, nil)
	appmcp.RegisterTools(server, appmcp.NewService(reg), true)
	names, err := appmcp.ListToolNames(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "save_doc_image" {
			t.Fatalf("只读模式不应注册 save_doc_image")
		}
	}
}

// 落盘位置：项目 assets/<sha256>（内容寻址），且同图只存一份。
func TestSaveDocImageWritesProjectAssets(t *testing.T) {
	root := t.TempDir()
	dir := newProjectDir(t, root, "demo", "uid-demo")
	reg, err := appmcp.NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "img", Version: "0"}, nil)
	appmcp.RegisterTools(server, appmcp.NewService(reg), false)

	t1, t2 := sdk.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "img-client", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()

	out := mustCall(t, cs, "save_doc_image", map[string]any{"project": "demo", "data": sdTestPNG})
	url := fieldAfter(out, "已保存图片：")
	hash := strings.TrimPrefix(url, "/doc-assets/")
	path := filepath.Join(dir, "assets", hash)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("图片应落在项目 assets/：%v\n%s", err, out)
	}
	if st.Size() == 0 {
		t.Errorf("图片文件为空")
	}
	// 同图再存一次：仍然只有一份文件（hash 相同）
	mustCall(t, cs, "save_doc_image", map[string]any{"project": "demo", "data": sdTestPNG})
	entries, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("同图应去重，assets/ 里应只有 1 个文件，实际 %d", len(entries))
	}
}
