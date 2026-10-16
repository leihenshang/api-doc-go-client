package app

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"api-doc-go-client/internal/collection"
)

// E19 接线核心：文档引用的同源图片镜像进 assets/（内容寻址去重），异源与 404 静默跳过、二次幂等。
func TestSyncDocAssetsMirrorsAndSkips(t *testing.T) {
	const blob = "PNGDATA"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/uploads/a.png":
			_, _ = w.Write([]byte(blob))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open collection: %v", err)
	}
	content := "![a](/uploads/a.png)\n![miss](/uploads/missing.png)\n![ext](https://evil.test/x.png)\n"
	if err := c.SaveDoc(&collection.DocEntry{
		UID: "doc-1", Name: "指南", Path: filepath.ToSlash(filepath.Join("docs", "guide.md")), Content: content,
	}); err != nil {
		t.Fatalf("save doc: %v", err)
	}

	n, err := syncDocAssets(c, srv.URL, "tok")
	if err != nil {
		t.Fatalf("syncDocAssets: %v", err)
	}
	if n != 1 {
		t.Fatalf("应只镜像同源可取得的 1 张，got %d", n)
	}
	sum := sha256.Sum256([]byte(blob))
	hash := hex.EncodeToString(sum[:])
	if !c.HasAsset(hash) {
		t.Fatalf("assets/ 未落内容寻址文件 %s", hash[:8])
	}

	// 幂等：再跑一轮全部命中已有哈希，新计数为 0
	if n2, err := syncDocAssets(c, srv.URL, "tok"); err != nil || n2 != 0 {
		t.Fatalf("已存在应跳过：n=%d err=%v", n2, err)
	}
}

// assetURL 是「文档附件只能从绑定服务端同源下载」这条规则的唯一入口。
// 回归点：曾经任意 http(s) 地址都会带上用户的 Bearer 令牌去下载（凭据外泄 + SSRF）。
func TestAssetURLOnlyAllowsServerOrigin(t *testing.T) {
	const server = "https://doc.example.com"
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"相对路径按服务端补全", "/uploads/a.png", "https://doc.example.com/uploads/a.png", false},
		{"同源绝对地址放行", "https://doc.example.com/uploads/b.png", "https://doc.example.com/uploads/b.png", false},
		{"同源但不同端口视为不同源", "https://doc.example.com:8443/x.png", "", true},
		{"异源必须拒绝", "https://evil.test/steal.png", "", true},
		{"协议不同视为异源", "http://doc.example.com/x.png", "", true},
		{"协议相对地址拒绝", "//evil.test/x.png", "", true},
		{"非 http 协议拒绝", "file:///etc/passwd", "", true},
		{"空地址拒绝", "   ", "", true},
	}
	for _, c := range cases {
		got, err := assetURL(server, c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: 期望报错，实际 %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 不应报错: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// 未配置服务端时，相对路径无从补全：必须报错而不是退化为「原样请求」。
func TestAssetURLRequiresServerForRelativePath(t *testing.T) {
	if _, err := assetURL("", "/uploads/a.png"); err == nil {
		t.Fatal("没有服务端地址时相对路径应报错")
	}
	if _, err := assetURL("not-a-url", "/uploads/a.png"); err == nil {
		t.Fatal("服务端地址非法时相对路径应报错")
	}
}
