package app

// 文档图片门面（app_docimage.go）的单测：
// base64 兼容（纯 / data URL / 换行折叠）、类型与大小校验、落盘去重、
// 只读路由（GET/HEAD/404/405、Content-Type 只认嗅探、不可当文件浏览器用）。

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// 与 collection/docimage_test.go、前端 e2e 共用同一张 1×1 PNG。
const testDocPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII="

var docAssetURLRe = regexp.MustCompile(`^/doc-assets/[0-9a-f]{64}$`)

func TestSaveDocImageAndServe(t *testing.T) {
	core, dir := newTestApp(t)
	url, err := core.SaveDocImage(testDocPNG)
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if !docAssetURLRe.MatchString(url) {
		t.Fatalf("URL 形态不符: %q", url)
	}
	hash := strings.TrimPrefix(url, collection.DocAssetURLPrefix)
	if _, err := os.Stat(filepath.Join(dir, "assets", hash)); err != nil {
		t.Fatalf("图片未落到集合 assets/: %v", err)
	}

	h := core.DocAssetHandler()

	// ① GET：内容与类型正确，并带缓存/防嗅探头
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET 状态码 = %d：%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type 应为嗅探结果 image/png，实际 %q", ct)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("缺少 nosniff")
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("内容寻址应可长期缓存: %q", w.Header().Get("Cache-Control"))
	}
	want, _ := base64.StdEncoding.DecodeString(testDocPNG)
	if w.Body.Len() != len(want) {
		t.Errorf("返回字节数 = %d，期望 %d", w.Body.Len(), len(want))
	}

	// ② HEAD 同样 200
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, url, nil))
	if w.Code != http.StatusOK {
		t.Errorf("HEAD 状态码 = %d", w.Code)
	}

	// ③ 同图去重：再次保存得到同一 URL
	if again, err := core.SaveDocImage(testDocPNG); err != nil || again != url {
		t.Errorf("同图应去重: %q %v", again, err)
	}

	// ④ 不存在的 hash / 非法 hash（穿越）→ 404
	for _, p := range []string{
		collection.DocAssetURLPrefix + strings.Repeat("0", 64),
		collection.DocAssetURLPrefix + "../../etc/passwd",
		"/doc-assets/",
		"/other/path",
	} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s 应 404，实际 %d", p, w.Code)
		}
	}

	// ⑤ 非 GET/HEAD → 405
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, url, strings.NewReader("x")))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST 应 405，实际 %d", w.Code)
	}
}

func TestSaveDocImageAcceptsDataURLAndFoldedBase64(t *testing.T) {
	core, _ := newTestApp(t)
	plain, err := core.SaveDocImage(testDocPNG)
	if err != nil {
		t.Fatalf("纯 base64 失败: %v", err)
	}
	dataURL, err := core.SaveDocImage("data:image/png;base64," + testDocPNG)
	if err != nil || dataURL != plain {
		t.Errorf("data URL 应等价于纯 base64: %q %v", dataURL, err)
	}
	folded := testDocPNG[:20] + "\n" + testDocPNG[20:]
	if got, err := core.SaveDocImage(folded); err != nil || got != plain {
		t.Errorf("折叠换行的 base64 应被接受: %q %v", got, err)
	}
}

func TestSaveDocImageRejects(t *testing.T) {
	core, _ := newTestApp(t)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空", "", "为空"},
		{"非法 base64", "!!!not-base64!!!", "解码失败"},
		{"SVG", base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), "不支持"},
		{"超限", base64.StdEncoding.EncodeToString(make([]byte, collection.MaxDocImageBytes+1)), "过大"},
	}
	for _, c := range cases {
		if _, err := core.SaveDocImage(c.in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 期望错误包含 %q，实际 %v", c.name, c.want, err)
		}
	}
}

// 非活动根里的图片也应能取到：内容寻址，活动集合优先、其余根兜底。
func TestDocAssetServesFromAnyRoot(t *testing.T) {
	core, first := newTestApp(t)
	url, err := core.SaveDocImage(testDocPNG)
	if err != nil {
		t.Fatal(err)
	}
	// 打开第二个集合并切为活动根；第一份仍留在内存表里
	second := t.TempDir()
	if _, err := core.OpenCollection(second); err != nil {
		t.Fatalf("打开第二个集合: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first, "assets", strings.TrimPrefix(url, collection.DocAssetURLPrefix))); err != nil {
		t.Fatalf("图片应在第一个集合: %v", err)
	}
	w := httptest.NewRecorder()
	core.DocAssetHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Errorf("切换活动根后仍应从其它根取到图片，状态码 %d", w.Code)
	}
}
