package runner

import (
	"testing"

	"api-doc-go-client/internal/collection"
)

// boolPtr 造 *bool 便于构造三态 settings。
func boolPtr(b bool) *bool { return &b }

// buildURL 的编码开关：默认（true）百分号编码参数值；false 原样拼接。
func TestBuildURLEncodeSwitch(t *testing.T) {
	base := collection.Request{
		Method: "GET",
		URL:    "http://example.com/p",
		Params: []collection.KV{{Name: "q", Value: "hello world", Enabled: true}},
	}
	// 开（默认）：值里的空格被编码为 +
	uOn, _, err := buildURL(base, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if uOn.RawQuery != "q=hello+world" {
		t.Errorf("encode=true 期望 q=hello+world，得 %q", uOn.RawQuery)
	}
	// 关：原样保留（空格不编码），这正是用户显式关掉编码要的效果
	uOff, _, err := buildURL(base, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if uOff.RawQuery != "q=hello world" {
		t.Errorf("encode=false 期望原样 q=hello world，得 %q", uOff.RawQuery)
	}
}

// encode=false 时保留 URL 里已有的原始 query，只把新参数原样追加（不重新编码既有片段）。
func TestBuildURLEncodeOffPreservesRawQuery(t *testing.T) {
	r := collection.Request{
		Method: "GET",
		URL:    "http://example.com/p?sig=a%2Fb",
		Params: []collection.KV{{Name: "t", Value: "x y", Enabled: true}},
	}
	u, _, err := buildURL(r, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if u.RawQuery != "sig=a%2Fb&t=x y" {
		t.Errorf("期望保留原 query 并原样追加，得 %q", u.RawQuery)
	}
}

// MergeOptions 把请求级 encodeUrl 三态并入 Options：nil 保留默认 true，&false 关闭，&true 打开。
func TestMergeOptionsEncodeURL(t *testing.T) {
	if got := MergeOptions(DefaultOptions(), nil).EncodeURL; !got {
		t.Error("无 settings 时默认应编码")
	}
	off := collection.RequestSettings{EncodeURL: boolPtr(false)}
	if got := MergeOptions(DefaultOptions(), &off).EncodeURL; got {
		t.Error("encodeUrl=false 应关闭编码")
	}
	on := collection.RequestSettings{EncodeURL: boolPtr(true)}
	if got := MergeOptions(DefaultOptions(), &on).EncodeURL; !got {
		t.Error("encodeUrl=true 应开启编码")
	}
	absent := collection.RequestSettings{}
	if got := MergeOptions(DefaultOptions(), &absent).EncodeURL; !got {
		t.Error("encodeUrl 未设置（nil）应沿用默认编码")
	}
}
