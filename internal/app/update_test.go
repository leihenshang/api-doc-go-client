package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.1.9", 1},
		{"0.1.9", "0.2.0", -1},
		{"0.1.9", "0.1.9", 0},
		{"1.0.0", "0.9.9", 1},
		{"0.1.10", "0.1.9", 1},     // 字符串比较会错，这里必须按数字
		{"v0.2.0", "0.2", 0},       // 缺位按 0
		{"0.2.0-beta", "0.2.0", 0}, // 后缀不参与
	}
	for _, c := range cases {
		if got := compareVer(c.a, c.b); got != c.want {
			t.Fatalf("compareVer(%q,%q)=%d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckUpdateAgainstFakeGitHub(t *testing.T) {
	old := updateAPIBase
	t.Cleanup(func() { updateAPIBase = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "missing UA", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.2.0",
			"html_url": "https://github.com/x/y/releases/tag/v0.2.0",
			"body":     "### 新增\n- 自动更新\n- 修复若干",
			"assets": []map[string]string{
				{"name": "api-doc-client-dev_0.2.0_amd64.deb", "browser_download_url": "https://x/dev.deb"},
				{"name": "api-doc-client_0.2.0_amd64.deb", "browser_download_url": "https://x/prod.deb"},
			},
		})
	}))
	defer srv.Close()
	updateAPIBase = srv.URL

	core, _ := newTestApp(t)
	info, err := core.CheckUpdate("v0.1.9")
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if !info.Available || info.Latest != "0.2.0" || info.Current != "0.1.9" {
		t.Fatalf("版本判断不符: %+v", info)
	}
	// 必须挑正式版 deb，不能把 dev 包给用户
	if info.AssetURL != "https://x/prod.deb" {
		t.Fatalf("应选正式版 deb，实际 %q (%s)", info.AssetURL, info.AssetName)
	}
	if info.Notes == "" {
		t.Fatal("更新说明未带回")
	}

	// 已是最新：available=false
	info2, err := core.CheckUpdate("0.2.0")
	if err != nil {
		t.Fatalf("CheckUpdate 同版本: %v", err)
	}
	if info2.Available {
		t.Fatalf("同版本不应提示更新: %+v", info2)
	}
}

func TestCheckUpdateNoReleaseYet(t *testing.T) {
	old := updateAPIBase
	t.Cleanup(func() { updateAPIBase = old })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	updateAPIBase = srv.URL

	core, _ := newTestApp(t)
	info, err := core.CheckUpdate("0.1.9")
	if err != nil {
		t.Fatalf("没有 Release 时不应报错: %v", err)
	}
	if info.Available {
		t.Fatal("没有 Release 不应提示更新")
	}
}
