package cookiejar

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析 URL: %v", err)
	}
	return u
}

func TestSetCookiesAndHostScoping(t *testing.T) {
	isolateConfigDir(t)
	jar, err := New(true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	api := mustURL(t, "https://api.example.com/v1/users")
	jar.SetCookies(api, []*http.Cookie{{Name: "sid", Value: "abc", Path: "/"}})

	got := jar.Cookies(api)
	if len(got) != 1 || got[0].Value != "abc" {
		t.Fatalf("Cookies: %+v", got)
	}
	other := mustURL(t, "https://other.com/")
	if len(jar.Cookies(other)) != 0 {
		t.Fatalf("不应把 Cookie 发给其他主机")
	}
	// 未下发 Domain = host-only：不回发子域（浏览器同款语义）
	deeper := mustURL(t, "https://sub.api.example.com/v1")
	if len(jar.Cookies(deeper)) != 0 {
		t.Fatalf("host-only Cookie 不应发给子域: %+v", jar.Cookies(deeper))
	}
	// 显式 Domain 为父域时子域才匹配
	jar.SetCookies(api, []*http.Cookie{{Name: "wide", Value: "w", Path: "/", Domain: ".example.com"}})
	if got := jar.Cookies(deeper); len(got) != 1 || got[0].Name != "wide" {
		t.Fatalf("Domain 父域 Cookie 应下发给子域: %+v", got)
	}
}

// 跨站 Domain 必须被拒绝：否则任意站点都能往别人的域里塞 Cookie（会话固定/超级 Cookie）。
func TestRejectForeignDomainAndSecureOverHTTP(t *testing.T) {
	isolateConfigDir(t)
	jar, err := New(false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	evil := mustURL(t, "https://evil.com/")
	jar.SetCookies(evil, []*http.Cookie{{Name: "sid", Value: "injected", Path: "/", Domain: "victim.com"}})
	victim := mustURL(t, "https://victim.com/")
	if got := jar.Cookies(victim); len(got) != 0 {
		t.Fatalf("跨站 Domain 应被拒绝: %+v", got)
	}
	// 公共后缀（com）同样拒绝
	jar.SetCookies(evil, []*http.Cookie{{Name: "su", Value: "1", Path: "/", Domain: "com"}})
	if got := jar.Cookies(mustURL(t, "https://any-other.com/")); len(got) != 0 {
		t.Fatalf("顶级域 Domain 应被拒绝: %+v", got)
	}
	// Secure Cookie 不得走明文
	jar.SetCookies(mustURL(t, "https://api.example.com/"), []*http.Cookie{{Name: "s", Value: "1", Path: "/", Secure: true}})
	if got := jar.Cookies(mustURL(t, "http://api.example.com/")); len(got) != 0 {
		t.Fatalf("Secure Cookie 不应走 http: %+v", got)
	}
	if got := jar.Cookies(mustURL(t, "https://api.example.com/")); len(got) != 1 {
		t.Fatalf("Secure Cookie 应走 https: %+v", got)
	}
}

func TestPersistAndReload(t *testing.T) {
	isolateConfigDir(t)
	u := mustURL(t, "https://api.example.com/")
	jar, err := New(true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "token", Value: "t1", Path: "/"}})

	reloaded, err := New(true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := reloaded.Cookies(u)
	if len(got) != 1 || got[0].Value != "t1" {
		t.Fatalf("重启后未恢复 Cookie: %+v", got)
	}
}

func TestDeleteAndExpire(t *testing.T) {
	isolateConfigDir(t)
	u := mustURL(t, "https://api.example.com/")
	jar, err := New(false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "1", Path: "/"}})
	jar.SetCookies(u, []*http.Cookie{{Name: "gone", Value: "x", Path: "/", Expires: time.Now().Add(-time.Hour)}})
	if got := jar.Cookies(u); len(got) != 1 || got[0].Name != "sid" {
		t.Fatalf("过期 Cookie 应被丢弃: %+v", got)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "", Path: "/", MaxAge: -1}})
	if got := jar.Cookies(u); len(got) != 0 {
		t.Fatalf("MaxAge<0 应删除: %+v", got)
	}
}

func TestClearAndList(t *testing.T) {
	isolateConfigDir(t)
	u := mustURL(t, "https://api.example.com/")
	jar, err := New(true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1", Path: "/"}})
	if list := jar.List(); len(list) != 1 || list[0].Domain != "api.example.com" {
		t.Fatalf("List: %+v", list)
	}
	if err := jar.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if list := jar.List(); len(list) != 0 {
		t.Fatalf("清空后仍有余留: %+v", list)
	}
}

// isolateConfigDir 把「用户配置目录」指向临时目录，避免读到/写到真实配置。
// Windows 上 os.UserConfigDir() 只看 %AppData%（忽略 XDG_CONFIG_HOME），
// Linux/macOS 反而看 XDG_CONFIG_HOME —— 两个都设，保证跨平台隔离一致。
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
}
