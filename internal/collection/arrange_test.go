package collection

import (
	"testing"
)

// rootNames 集合根下按显示顺序列出节点名（分组在前，组内 seq）。
func rootNames(t *testing.T, c *Collection) []string {
	t.Helper()
	nodes, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	return nodeNames(nodes)
}

func nodeNames(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

// childNames 取某个分组的子节点名字（按显示顺序）。
func childNames(t *testing.T, c *Collection, path string) []string {
	t.Helper()
	nodes, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	for _, n := range nodes {
		if n.Type == "folder" && n.Path == path {
			return nodeNames(n.Children)
		}
	}
	t.Fatalf("找不到分组 %s", path)
	return nil
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("顺序不符：want %v got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序不符：want %v got %v", want, got)
		}
	}
}

// 请求同层重排：拖到目标行之前即插到它前面，seq 重编号后重扫仍是该顺序。
func TestArrangeNodeReordersRequests(t *testing.T) {
	c := newTestCollection(t)
	a, err := c.CreateRequest("", "a", "GET")
	if err != nil {
		t.Fatalf("建 a: %v", err)
	}
	if _, err := c.CreateRequest("", "b", "GET"); err != nil {
		t.Fatalf("建 b: %v", err)
	}
	d, err := c.CreateRequest("", "d", "GET")
	if err != nil {
		t.Fatalf("建 d: %v", err)
	}
	assertNames(t, rootNames(t, c), []string{"a", "b", "d"})

	if err := c.ArrangeNode(d.UID, "", a.UID); err != nil {
		t.Fatalf("拖动排序: %v", err)
	}
	assertNames(t, rootNames(t, c), []string{"d", "a", "b"})

	// 换一个集合实例重扫（模拟关闭重开）：顺序必须仍然来自落盘的 seq
	fresh := &Collection{Dir: c.Dir}
	assertNames(t, rootNames(t, fresh), []string{"d", "a", "b"})

	// 拖到末尾：beforeUID 为空 = 同类型末尾
	if err := c.ArrangeNode(d.UID, "", ""); err != nil {
		t.Fatalf("拖到末尾: %v", err)
	}
	assertNames(t, rootNames(t, c), []string{"a", "b", "d"})
}

// 分组同层重排：分组顺序由 folder.yml 的 info.seq 决定（缺失时按名称），改动后重扫一致。
func TestArrangeNodeReordersFolders(t *testing.T) {
	c := newTestCollection(t)
	for _, name := range []string{"甲", "乙", "丙"} {
		if err := c.CreateFolder("", name); err != nil {
			t.Fatalf("建分组 %s: %v", name, err)
		}
	}
	uid甲, ok := c.folderUID("甲")
	if !ok {
		t.Fatal("甲 缺少 uid")
	}
	uid丙, ok := c.folderUID("丙")
	if !ok {
		t.Fatal("丙 缺少 uid")
	}
	// 初始按名称：丙、乙、甲。把「甲」拖到「丙」之前 → 甲、丙、乙
	if err := c.ArrangeNode(uid甲, "", uid丙); err != nil {
		t.Fatalf("拖动排序: %v", err)
	}
	assertNames(t, rootNames(t, c), []string{"甲", "丙", "乙"})

	fresh := &Collection{Dir: c.Dir}
	assertNames(t, rootNames(t, fresh), []string{"甲", "丙", "乙"})
}

// 请求与文档同属「叶子」一组：可以相互排序，但仍排在分组之后。
func TestArrangeNodeOrdersDocsWithRequests(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "分组"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	req, err := c.CreateRequest("", "接口", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	doc, err := c.CreateDoc("说明", "", ".")
	if err != nil {
		t.Fatalf("建文档: %v", err)
	}
	assertNames(t, rootNames(t, c), []string{"分组", "说明", "接口"})

	// 把接口拖到文档之前：分组仍在最前（分组优先于叶子的排序规则不变）
	if err := c.ArrangeNode(req.UID, "", doc.UID); err != nil {
		t.Fatalf("拖动排序: %v", err)
	}
	assertNames(t, rootNames(t, c), []string{"分组", "接口", "说明"})

	fresh := &Collection{Dir: c.Dir}
	assertNames(t, rootNames(t, fresh), []string{"分组", "接口", "说明"})
}

// 跨分组拖到指定位置：既要改上级，也要按落点插入到目标顺序里。
func TestArrangeNodeMovesIntoFolderAtPosition(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	if _, err := c.CreateRequest("用户", "inner1", "GET"); err != nil {
		t.Fatalf("建 inner1: %v", err)
	}
	inner2, err := c.CreateRequest("用户", "inner2", "GET")
	if err != nil {
		t.Fatalf("建 inner2: %v", err)
	}
	outer, err := c.CreateRequest("", "outer", "GET")
	if err != nil {
		t.Fatalf("建 outer: %v", err)
	}

	if err := c.ArrangeNode(outer.UID, "用户", inner2.UID); err != nil {
		t.Fatalf("拖入分组: %v", err)
	}
	assertNames(t, childNames(t, c, "用户"), []string{"inner1", "outer", "inner2"})
	assertNames(t, rootNames(t, c), []string{"用户"}) // 根下只剩分组

	got, err := c.ReadRequest(outer.UID)
	if err != nil {
		t.Fatalf("重读 outer: %v", err)
	}
	if got.Path != "用户/outer.yml" {
		t.Fatalf("请求路径未更新: %q", got.Path)
	}
}

// 分组不能拖进自己的后代（会成环）：与 MoveFolder 的校验一致。
func TestArrangeNodeRejectsFolderCycle(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "甲"); err != nil {
		t.Fatalf("建甲: %v", err)
	}
	if err := c.CreateFolder("甲", "子"); err != nil {
		t.Fatalf("建子: %v", err)
	}
	uid甲, _ := c.folderUID("甲")
	if err := c.ArrangeNode(uid甲, "甲/子", ""); err == nil {
		t.Fatal("分组拖入自己的后代应被拒绝")
	}
}

// 目标分组不存在时报错，不留下半截改动。
func TestArrangeNodeRejectsMissingFolder(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "a", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	if err := c.ArrangeNode(r.UID, "不存在", ""); err == nil {
		t.Fatal("目标分组不存在应报错")
	}
	if names := rootNames(t, c); len(names) != 1 || names[0] != "a" {
		t.Fatalf("失败不应改动树: %v", names)
	}
}
