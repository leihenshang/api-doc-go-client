package collection

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// tinyPNG 生成一张 1×1 PNG（内联测试用的最小合法图片）。
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestMDImageSyntax Markdown 图片语法要渲染成 <img>，此前会被整段转义成字面量。
func TestMDImageSyntax(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"基本", "![截图](/doc-assets/abc.png)", `<img src="/doc-assets/abc.png" alt="截图"`},
		{"带 title", `![图](https://x.test/a.png "标题")`, `<img src="https://x.test/a.png" alt="图"`},
		{"相对路径", "![](./img/a.png)", `<img src="./img/a.png" alt=""`},
		{"居中在段内", "前 ![a](b.png) 后", "前 <img src=\"b.png\""},
	}
	for _, tc := range cases {
		got := mdToHTML(tc.in)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s：期望包含 %q，实际 %q", tc.name, tc.want, got)
		}
	}
}

// TestMDImageRejectsUnsafe 非安全 scheme 不渲染成 img（不能被当成图片地址注入）。
func TestMDImageRejectsUnsafe(t *testing.T) {
	for _, src := range []string{"javascript:alert(1)", "vbscript:msgbox(1)", "file:///etc/passwd"} {
		got := mdToHTML("![x](" + src + ")")
		if strings.Contains(got, "<img") {
			t.Fatalf("%s 不该渲染成图片：%q", src, got)
		}
	}
}

// TestMDImageInsideCodeFenceUntouched 代码块里的图片语法保持原样（示例代码不该被执行）。
func TestMDImageInsideCodeFenceUntouched(t *testing.T) {
	got := mdToHTML("```\n![x](/doc-assets/a.png)\n```")
	if strings.Contains(got, "<img") {
		t.Fatalf("代码块里的图片语法不该被渲染：%q", got)
	}
}

// TestInlineLocalImage 集合内图片内联成 data URL，单文件可直接打开。
func TestInlineLocalImage(t *testing.T) {
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopWatch()
	hash, err := c.SaveDocImage(tinyPNG(t))
	if err != nil {
		t.Fatal(err)
	}

	in := `<p><img src="` + DocAssetURLPrefix + hash + `" alt="x"></p>`
	out, st := c.InlineImages(in, false)
	if !strings.Contains(out, "data:image/png;base64,") {
		t.Fatalf("未内联：%q", out)
	}
	if strings.Contains(out, DocAssetURLPrefix) {
		t.Fatalf("原地址应被替换：%q", out)
	}
	if st.Inlined != 1 || st.Skipped != 0 {
		t.Fatalf("统计不对：%+v", st)
	}
}

// TestInlineMissingKeepsSrc 读不到的图片保留原地址并计入 skipped（不静默丢、不报错）。
func TestInlineMissingKeepsSrc(t *testing.T) {
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopWatch()
	missing := strings.Repeat("a", 64)
	in := `<img src="` + DocAssetURLPrefix + missing + `">`
	out, st := c.InlineImages(in, false)
	if out != in {
		t.Fatalf("应保留原地址：%q", out)
	}
	if st.Skipped != 1 {
		t.Fatalf("应计一次 skipped：%+v", st)
	}
}

// TestInlineRemoteImage 勾选网络图片时才下载内联；不勾选则一个请求都不发。
func TestInlineRemoteImage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG(t))
	}))
	defer srv.Close()

	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopWatch()
	in := `<img src="` + srv.URL + `/a.png">`

	// 不勾选：保留原地址，且不发请求
	out, st := c.InlineImages(in, false)
	if out != in || st.Skipped != 1 {
		t.Fatalf("未勾选时不该下载：out=%q st=%+v", out, st)
	}
	if hits != 0 {
		t.Fatalf("未勾选却发出了 %d 次请求", hits)
	}

	// 勾选：内联
	out, st = c.InlineImages(in, true)
	if !strings.Contains(out, "data:image/png;base64,") {
		t.Fatalf("勾选后未内联：%q", out)
	}
	if st.Remote != 1 || st.Inlined != 1 {
		t.Fatalf("统计不对：%+v", st)
	}
}

// TestInlineRemoteNonImageSkipped 网络返回的不是图片：保留原地址，不把 HTML 塞进 data URL。
func TestInlineRemoteNonImageSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer srv.Close()

	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopWatch()
	in := `<img src="` + srv.URL + `/a.png">`
	out, st := c.InlineImages(in, true)
	if out != in {
		t.Fatalf("非图片应保留原地址：%q", out)
	}
	if st.Skipped != 1 || st.Inlined != 0 {
		t.Fatalf("统计不对：%+v", st)
	}
}

// TestExportHTMLHasPrintCSS 打印样式随 HTML 一起导出（PDF 走系统打印依赖它）。
func TestExportHTMLHasPrintCSS(t *testing.T) {
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopWatch()
	out, err := c.ExportHTML()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@media print", "@page", "break-inside: avoid"} {
		if !strings.Contains(out, want) {
			t.Fatalf("导出 HTML 缺少 %q", want)
		}
	}
}
