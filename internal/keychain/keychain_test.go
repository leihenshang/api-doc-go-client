package keychain

import "testing"

// withFake 用内存实现顶替真实密钥环；通 t.Cleanup 恢复（同包直接捕获原钩子）。
// 注意：不能用 defer——defer 在 withFake 返回时就执行，测试体会跑在真实密钥环上。
func withFake(t *testing.T, store map[string]string, unavailable bool) {
	t.Helper()
	prevSet, prevGet, prevDel := setHook, getHook, delHook
	t.Cleanup(func() { SetHooks(prevSet, prevGet, prevDel) })
	if unavailable {
		setHook = func(string, string) error { return ErrNotAvailable }
		getHook = func(string) (string, error) { return "", ErrNotAvailable }
		delHook = func(string) error { return ErrNotAvailable }
	} else {
		setHook = func(u, v string) error { store[u] = v; return nil }
		getHook = func(u string) (string, error) {
			if v, ok := store[u]; ok {
				return v, nil
			}
			return "", ErrNotFound
		}
		delHook = func(u string) error {
			if _, ok := store[u]; !ok {
				return ErrNotFound
			}
			delete(store, u)
			return nil
		}
	}
}

func TestRoundTrip(t *testing.T) {
	store := map[string]string{}
	withFake(t, store, false)

	if err := Set("u1", "v1"); err != nil {
		t.Fatal(err)
	}
	if v, err := Get("u1"); err != nil || v != "v1" {
		t.Fatalf("get: v=%q err=%v", v, err)
	}
	if _, err := Get("nope"); err != ErrNotFound {
		t.Fatalf("未命中应返回 ErrNotFound，实际 %v", err)
	}
	if err := Delete("u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("u1"); err != ErrNotFound {
		t.Fatalf("删除后应 ErrNotFound，实际 %v", err)
	}
	if err := Delete("u1"); err != ErrNotFound {
		t.Fatalf("重复删除应 ErrNotFound，实际 %v", err)
	}
}

func TestUnavailablePropagates(t *testing.T) {
	withFake(t, nil, true)
	if err := Set("u", "v"); err == nil {
		t.Error("无密钥环时 Set 应报错（调用方据此回退文件）")
	}
	if _, err := Get("u"); err == nil {
		t.Error("无密钥环时 Get 应报错")
	}
}
