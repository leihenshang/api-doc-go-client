package app

// 换绑项目必须被拒绝：服务端 group/api/doc 的 ext_uid 是全局唯一索引，
// 同一集合推到另一个项目会整批撞唯一键（260 条 op 全部 rejected），
// 用户看到的是一堆 parent_missing / invalid，完全无从下手。

import (
	"strings"
	"testing"
)

func TestSetSyncBindRejectsProjectSwitch(t *testing.T) {
	core, _ := newTestApp(t)
	roots, err := core.ListCollections()
	if err != nil || len(roots) == 0 {
		t.Fatalf("取集合根失败: %v %v", roots, err)
	}
	root := roots[0].Root

	// 首次绑定
	if _, err := core.SetSyncBindFor(SyncBindReq{
		Root: root, ServerURL: "https://example.com", ProjectID: 11, Mode: "auto",
	}); err != nil {
		t.Fatalf("首次绑定: %v", err)
	}

	// 换到另一个项目：必须拒绝，且错误里说清怎么办
	_, err = core.SetSyncBindFor(SyncBindReq{
		Root: root, ServerURL: "https://example.com", ProjectID: 22, Mode: "auto",
	})
	if err == nil {
		t.Fatal("换绑项目应被拒绝")
	}
	if !strings.Contains(err.Error(), "不能直接切换") {
		t.Fatalf("错误信息不够明确: %v", err)
	}

	// 同一项目只改模式：仍允许
	if _, err := core.SetSyncBindFor(SyncBindReq{
		Root: root, ServerURL: "https://example.com", ProjectID: 11, Mode: "manual",
	}); err != nil {
		t.Fatalf("同项目改模式不应失败: %v", err)
	}
}
