package mcp_test

// 文档条目（集合里的独立 .md）的 MCP 读写：list/get/create/update/delete。
// 与请求自带说明（request.docs）区分 —— 后者由 create_request/update_request 的 docs 字段覆盖。

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 与同目录测试一致：白盒式调用
	"strings"
	"testing"
)

func TestDocCRUD(t *testing.T) {
	_, svc := setup(t)

	// 新建（带正文，挂在 docs 分组下）
	d, err := svc.CreateDoc(CreateDocInput{
		Project: "demo", Name: "接入指南", Folder: "docs", Content: "# 接入指南\n\n先申请 token。",
	})
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	if d.UID == "" || d.Name != "接入指南" || !strings.HasPrefix(d.Path, "docs/") {
		t.Fatalf("新建结果异常: %+v", d)
	}
	if d.Size == 0 || d.Hash == "" {
		t.Fatalf("应带回正文长度与 hash: %+v", d)
	}

	// 列表：默认全量；query 命中名称/路径
	all, total, err := svc.ListDocs("demo", "", 0, 0, false)
	if err != nil || len(all) != 1 || total != 1 || all[0].UID != d.UID {
		t.Fatalf("ListDocs 异常: %+v total=%d %v", all, total, err)
	}
	if hit, _, err := svc.ListDocs("demo", "接入", 0, 0, false); err != nil || len(hit) != 1 {
		t.Fatalf("按名称过滤应命中 1 篇: %+v %v", hit, err)
	}
	if miss, _, err := svc.ListDocs("demo", "不存在的关键词", 0, 0, false); err != nil || len(miss) != 0 {
		t.Fatalf("不匹配时应为空: %+v %v", miss, err)
	}

	// 读正文
	got, err := svc.GetDoc("demo", d.UID)
	if err != nil {
		t.Fatalf("GetDoc: %v", err)
	}
	if !strings.Contains(got.Content, "先申请 token") {
		t.Fatalf("正文异常: %q", got.Content)
	}

	// 带 ifMatch 改正文：只传 get_doc 里显示的那 12 位前缀（与 AI 实际拿到的文本一致）
	hash := got.Hash
	if len(hash) < 12 {
		t.Fatalf("hash 太短: %q", hash)
	}
	prefix := hash[:12]
	upd, err := svc.UpdateDoc(UpdateDocInput{
		Project: "demo", UID: d.UID, Content: strPtr("# 接入指南\n\n改过了。"), IfMatch: prefix,
	})
	if err != nil {
		t.Fatalf("UpdateDoc: %v", err)
	}
	if !strings.Contains(upd.Content, "改过了") {
		t.Fatalf("正文未更新: %q", upd.Content)
	}

	// ifMatch 过期 → 拒绝写入（old hash 已失效）
	if _, err := svc.UpdateDoc(UpdateDocInput{
		Project: "demo", UID: d.UID, Content: strPtr("不该写进去"), IfMatch: prefix,
	}); err == nil {
		t.Fatal("过期的 ifMatch 应被拒绝")
	}

	// 改名（只改显示名，文件名不变 → 路径不变）
	renamed, err := svc.UpdateDoc(UpdateDocInput{Project: "demo", UID: d.UID, Name: strPtr("接入说明")})
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if renamed.Name != "接入说明" || renamed.Path != d.Path {
		t.Fatalf("改名结果异常: %+v", renamed)
	}

	// 缺 uid / 缺 name 的错误提示
	if _, err := svc.GetDoc("demo", ""); err == nil {
		t.Error("GetDoc 缺 uid 应报错")
	}
	if _, err := svc.CreateDoc(CreateDocInput{Project: "demo", Name: "  "}); err == nil {
		t.Error("CreateDoc 缺 name 应报错")
	}

	// 删除（进 .trash），列表里不再出现
	if err := svc.DeleteDoc("demo", d.UID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}
	if left, _, err := svc.ListDocs("demo", "", 0, 0, false); err != nil || len(left) != 0 {
		t.Fatalf("删除后应为空: %+v %v", left, err)
	}
}

// get_doc_template 必须与前端 buildApiDoc（lib/genDocs.ts）的骨架对齐：
// 含「请求体」一节、响应字段表示例值列、请求示例附 JSON 代码块。
// 防两端漂移（曾因模板漏「请求体」而让 AI 复现 H15 漏 body 章的回归）。
func TestDocTemplateAlignedWithGenDocs(t *testing.T) {
	_, svc := setup(t)
	tpl := svc.DocTemplate()
	for _, want := range []string{
		"## 请求参数", "## 请求体", "## 请求示例", "## 响应字段", "示例值", "```json",
		// 请求体字段表为 5 列，且明确来自 body.fields（可为空 → 只出原文）
		"| 参数名 | 必选 | 类型 | 示例值 | 说明 |", "body.fields",
	} {
		if !strings.Contains(tpl, want) {
			t.Errorf("模板缺少 %q（与 genDocs 输出漂移）:\n%s", want, tpl)
		}
	}
}
