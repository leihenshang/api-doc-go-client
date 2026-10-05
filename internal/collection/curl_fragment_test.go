package collection

import "testing"

// URL 里的 fragment 必须整体丢弃：`#frag` 出现在 `?` 之后时，
// 旧实现只对 `?` 之前做 Cut，fragment 会残留在最后一个参数值里。
func TestParseCurlDropsFragment(t *testing.T) {
	r, err := ParseCurl(`curl 'http://x.test/y?a=1&b=2#section'`)
	if err != nil {
		t.Fatalf("ParseCurl: %v", err)
	}
	if r.URL != "http://x.test/y" {
		t.Fatalf("URL 应去掉 query 与 fragment，实际 %q", r.URL)
	}
	// 结尾会追加一行空行供编辑器继续填写，不参与断言
	want := map[string]string{"a": "1", "b": "2"}
	for _, p := range r.Params {
		if p.Name == "" {
			continue
		}
		v, ok := want[p.Name]
		if !ok || v != p.Value {
			t.Fatalf("参数 %s = %q，期望 %q（fragment 可能混进了值）", p.Name, p.Value, v)
		}
		delete(want, p.Name)
	}
	if len(want) != 0 {
		t.Fatalf("缺少参数: %v（实际 %+v）", want, r.Params)
	}
}
