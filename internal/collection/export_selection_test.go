package collection

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// exportFixture 造一棵「根请求 + 两层目录 + 两篇文档」的集合，覆盖各种选中组合。
type exportFixture struct {
	c       *Collection
	folderA string // 目录 A 的 uid
	reqR1   *Request
	reqR2   *Request
	reqR3   *Request
	docRoot *DocEntry
	docInA  *DocEntry
}

func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	c := newTestCollection(t)
	if err := c.CreateFolder("", "A"); err != nil {
		t.Fatalf("建目录 A: %v", err)
	}
	if err := c.CreateFolder("A", "A1"); err != nil {
		t.Fatalf("建目录 A/A1: %v", err)
	}
	mkReq := func(folder, name string) *Request {
		r, err := c.CreateRequest(folder, name, "GET")
		if err != nil {
			t.Fatalf("建请求 %s: %v", name, err)
		}
		return r
	}
	mkDoc := func(name, folder string) *DocEntry {
		d, err := c.CreateDoc(name, "", folder)
		if err != nil {
			t.Fatalf("建文档 %s: %v", name, err)
		}
		return d
	}
	uidA, ok := c.folderUID("A")
	if !ok || uidA == "" {
		t.Fatalf("目录 A 缺少 uid")
	}
	return &exportFixture{
		c:       c,
		folderA: uidA,
		reqR1:   mkReq("A", "req-one"),
		reqR2:   mkReq("A/A1", "req-two"),
		reqR3:   mkReq("", "req-root"),
		docRoot: mkDoc("doc-root", ""),
		docInA:  mkDoc("doc-in-a", "A"),
	}
}

// full 全量导出的 Markdown，用于对照「全选 = 旧行为」。
func (f *exportFixture) full(t *testing.T) string {
	t.Helper()
	md, err := f.c.ExportMarkdown()
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	return md
}

func TestExportSelectionRequestsOnly(t *testing.T) {
	f := newExportFixture(t)
	md, err := f.c.ExportMarkdownSelected(ExportSelection{Requests: []string{f.reqR1.UID}})
	if err != nil {
		t.Fatalf("ExportMarkdownSelected: %v", err)
	}
	if !strings.Contains(md, "req-one") {
		t.Fatalf("缺少选中的请求 req-one:\n%s", md)
	}
	for _, unwanted := range []string{"req-two", "req-root", "doc-root", "doc-in-a"} {
		if strings.Contains(md, unwanted) {
			t.Fatalf("未选中的 %q 出现在导出里:\n%s", unwanted, md)
		}
	}
	// 祖先目录必须保留：去掉它标题层级就断了（请求会直接从 h2 起，看不出归属）
	if !strings.Contains(md, "## A") {
		t.Fatalf("缺少祖先目录标题 A:\n%s", md)
	}
	// 未包含兄弟目录 A1
	if strings.Contains(md, "### A1") || strings.Contains(md, "A1") {
		t.Fatalf("未选中的子目录 A1 出现在导出里:\n%s", md)
	}
}

func TestExportSelectionFolderIncludesSubtree(t *testing.T) {
	f := newExportFixture(t)
	md, err := f.c.ExportMarkdownSelected(ExportSelection{Groups: []string{f.folderA}})
	if err != nil {
		t.Fatalf("ExportMarkdownSelected: %v", err)
	}
	// 选中目录 = 整棵子树：两层目录里的请求与文档都要在
	for _, want := range []string{"req-one", "req-two", "doc-in-a", "A1"} {
		if !strings.Contains(md, want) {
			t.Fatalf("选中目录 A 后缺少 %q:\n%s", want, md)
		}
	}
	for _, unwanted := range []string{"req-root", "doc-root"} {
		if strings.Contains(md, unwanted) {
			t.Fatalf("目录外的 %q 出现在导出里:\n%s", unwanted, md)
		}
	}
}

func TestExportSelectionDocsOnly(t *testing.T) {
	f := newExportFixture(t)
	md, err := f.c.ExportMarkdownSelected(ExportSelection{Docs: []string{f.docInA.UID}})
	if err != nil {
		t.Fatalf("ExportMarkdownSelected: %v", err)
	}
	if !strings.Contains(md, "doc-in-a") {
		t.Fatalf("缺少选中的文档:\n%s", md)
	}
	for _, unwanted := range []string{"doc-root", "req-one", "req-two", "req-root"} {
		if strings.Contains(md, unwanted) {
			t.Fatalf("未选中的 %q 出现在导出里:\n%s", unwanted, md)
		}
	}
}

func TestExportSelectionAllMatchesFullExport(t *testing.T) {
	f := newExportFixture(t)
	want := f.full(t)
	// All=true 与「不传任何范围」都必须与旧的全量导出一字不差
	for name, sel := range map[string]ExportSelection{
		"all":   {All: true},
		"empty": {},
	} {
		got, err := f.c.ExportMarkdownSelected(sel)
		if err != nil {
			t.Fatalf("%s: ExportMarkdownSelected: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s 与全量导出不一致:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
		}
	}
}

func TestExportSelectionUnknownUid(t *testing.T) {
	f := newExportFixture(t)
	_, err := f.c.ExportMarkdownSelected(ExportSelection{Requests: []string{"no-such-uid"}})
	if !errors.Is(err, ErrNothingSelected) {
		t.Fatalf("期望 ErrNothingSelected，实际 %v", err)
	}
	// HTML 同一条路径，也要给出同样的错误而不是产出空文档
	if _, err := f.c.ExportHTMLSelected(ExportSelection{Docs: []string{"no-such-uid"}}); !errors.Is(err, ErrNothingSelected) {
		t.Fatalf("HTML 期望 ErrNothingSelected，实际 %v", err)
	}
}

func TestExportSelectionHTMLBodyAndTOCAgree(t *testing.T) {
	f := newExportFixture(t)
	out, err := f.c.ExportHTMLSelected(ExportSelection{Groups: []string{f.folderA}})
	if err != nil {
		t.Fatalf("ExportHTMLSelected: %v", err)
	}
	for _, want := range []string{"req-one", "req-two", "doc-in-a", `class="toc"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("HTML 缺少 %q", want)
		}
	}
	// 未选中的条目既不能出现在正文，也不能出现在左侧目录（否则点进去是空的）
	for _, unwanted := range []string{"req-root", "doc-root"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("未选中的 %q 出现在 HTML（正文或目录）里", unwanted)
		}
	}
	// 目录锚点与正文标题 id 必须一一对应（裁剪后两边同源，否则点目录会跳不到）
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="toc-(\d+)"`).FindAllStringSubmatch(out, -1) {
		ids[m[1]] = true
	}
	hrefs := map[string]bool{}
	for _, m := range regexp.MustCompile(`href="#toc-(\d+)"`).FindAllStringSubmatch(out, -1) {
		hrefs[m[1]] = true
	}
	if len(ids) != len(hrefs) {
		t.Fatalf("TOC 锚点 %d 个、链接 %d 个，数量不一致（目录与正文对不上）", len(ids), len(hrefs))
	}
	for k := range hrefs {
		if !ids[k] {
			t.Fatalf("TOC 链接 #toc-%s 在正文里没有对应锚点", k)
		}
	}
}
