package collection

import (
	"errors"
	"strings"
	"testing"
)

// 评审 P0-2：create_request 的冲突策略与 create_doc 对齐（默认 error）。
func TestCreateRequestConflictModes(t *testing.T) {
	c := newTestCollection(t)
	draft := func(url string) *Request {
		return &Request{Method: "GET", URL: url, Params: []KV{{Enabled: true}}}
	}

	first, err := c.CreateRequestFromDraftEx("api", "重名接口", draft("/v1"), "", "")
	if err != nil {
		t.Fatal(err)
	}

	// 默认：同路径 → ErrRequestConflict（带已有 path）
	_, err = c.CreateRequestFromDraftEx("api", "重名接口", draft("/v2"), "", "")
	if !errors.Is(err, ErrRequestConflict) || !strings.Contains(err.Error(), first.Path) {
		t.Fatalf("默认应冲突并带 path：%v", err)
	}

	// suffix：加 -2，且 uid 不同
	second, err := c.CreateRequestFromDraftEx("api", "重名接口", draft("/v2"), "", "suffix")
	if err != nil {
		t.Fatal(err)
	}
	if second.Path == first.Path || !strings.HasSuffix(second.Path, "-2.yml") || second.UID == first.UID {
		t.Errorf("suffix 结果不符：%+v vs %+v", second, first)
	}

	// uid 指定 + 默认：同 uid → 冲突
	if _, err := c.CreateRequestFromDraftEx("api", "固定身份", draft("/a"), "fixed-uid-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRequestFromDraftEx("api", "固定身份", draft("/b"), "fixed-uid-1", ""); !errors.Is(err, ErrRequestConflict) {
		t.Errorf("同 uid 默认应冲突：%v", err)
	}

	// overwrite：同 uid 复用 uid/path，内容更新
	ov, err := c.CreateRequestFromDraftEx("api", "固定身份", draft("/b"), "fixed-uid-1", "overwrite")
	if err != nil {
		t.Fatal(err)
	}
	if ov.UID != "fixed-uid-1" || ov.URL != "/b" {
		t.Errorf("overwrite 不符：%+v", ov)
	}
	read, err := c.ReadRequest("fixed-uid-1")
	if err != nil || read.URL != "/b" || read.Path != ov.Path {
		t.Errorf("overwrite 应落盘到原路径：%+v err=%v", read, err)
	}

	// 非法策略
	if _, err := c.CreateRequestFromDraftEx("api", "X", draft("/x"), "", "nope"); err == nil {
		t.Errorf("非法 onConflict 应报错")
	}
}
