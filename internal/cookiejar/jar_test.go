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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	deeper := mustURL(t, "https://sub.api.example.com/v1")
	if len(jar.Cookies(deeper)) != 1 {
		t.Fatalf("子域应匹配（域名 Cookie）")
	}
}

func TestPersistAndReload(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
