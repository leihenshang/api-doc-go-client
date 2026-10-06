package history

import (
	"fmt"
	"testing"
)

func TestAppendTruncateAndList(t *testing.T) {
	isolateConfigDir(t)
	for i := 0; i < 5; i++ {
		e := Entry{Time: int64(i), URL: fmt.Sprintf("/u%d", i), Method: "GET", Status: 200}
		if err := Append(e, 3); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	items, err := List(0, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("应裁剪到 3 条，实际 %d", len(items))
	}
	if items[0].URL != "/u4" || items[2].URL != "/u2" {
		t.Fatalf("顺序应为新→旧: %+v", items)
	}
	limited, err := List(2, "")
	if err != nil || len(limited) != 2 {
		t.Fatalf("limit 未生效: %v %d", err, len(limited))
	}
}

// 多工作目录：历史按工作目录标识过滤；升级前「无归属」的旧记录在任一目录下都可见，
// 并随任一次按目录清空一起清掉（否则升级后旧历史会像凭空消失）。
func TestListAndClearFilterByCollection(t *testing.T) {
	isolateConfigDir(t)
	seed := []Entry{
		{Root: "a", URL: "/a1", Method: "GET"},
		{Root: "a", URL: "/a2", Method: "GET"},
		{Root: "b", URL: "/b1", Method: "GET"},
		{Root: "", URL: "/legacy", Method: "GET"},
	}
	for _, e := range seed {
		if err := Append(e, 10); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	forA, err := List(0, "a")
	if err != nil {
		t.Fatalf("List(a): %v", err)
	}
	if len(forA) != 3 {
		t.Fatalf("a 目录应看到 2 条自身 + 1 条无归属，实际 %d", len(forA))
	}
	for _, e := range forA {
		if e.Root == "b" {
			t.Fatalf("a 目录不应看到 b 的记录: %+v", e)
		}
	}
	forB, _ := List(0, "b")
	if len(forB) != 2 {
		t.Fatalf("b 目录应看到 1 条自身 + 1 条无归属，实际 %d", len(forB))
	}

	if err := Clear("a"); err != nil {
		t.Fatalf("Clear(a): %v", err)
	}
	if rest, _ := List(0, "a"); len(rest) != 0 {
		t.Fatalf("a 目录应已清空: %+v", rest)
	}
	left, _ := List(0, "")
	if len(left) != 1 || left[0].Root != "b" {
		t.Fatalf("清 a 不应动 b 的记录: %+v", left)
	}
}

func TestEntriesSurviveFailureAndClear(t *testing.T) {
	isolateConfigDir(t)
	if err := Append(Entry{Method: "GET", URL: "http://x", Error: "连接失败"}, 10); err != nil {
		t.Fatalf("Append: %v", err)
	}
	items, err := List(0, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Error != "连接失败" || items[0].Status != 0 {
		t.Fatalf("失败记录未保留: %+v", items)
	}
	if err := Clear(""); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	items, _ = List(0, "")
	if len(items) != 0 {
		t.Fatalf("清空失败: %+v", items)
	}
}

// isolateConfigDir 把「用户配置目录」指向临时目录，避免读到/写到真实配置与真实历史。
// Windows 上 os.UserConfigDir() 只看 %AppData%（忽略 XDG_CONFIG_HOME），Linux/macOS 反之 —— 两个都设。
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
}
