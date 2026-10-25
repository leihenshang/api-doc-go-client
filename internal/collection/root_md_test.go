package collection

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mdUIDFor 复算「无 frontmatter 文档」的稳定 uid（md- + sha256(相对路径) 前 16 位）。
func mdUIDFor(rel string) string {
	sum := sha256.Sum256([]byte(rel))
	return "md-" + hex.EncodeToString(sum[:])[:16]
}

// writeTestFile 建目录并写文件（测试内联工具）。
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// docsByPath 把 ListDocs 结果转成 path → 条目。
func docsByPath(t *testing.T, c *Collection) map[string]*DocEntry {
	t.Helper()
	list, err := c.ListDocs()
	if err != nil {
		t.Fatalf("ListDocs: %v", err)
	}
	out := make(map[string]*DocEntry, len(list))
	for _, d := range list {
		out[d.Path] = d
	}
	return out
}

// TestRootMarkdownWithoutFrontmatter 集合根目录下的 .md（含 README.md）没有 frontmatter
// 也必须作为文档条目出现：uid 由相对路径哈希稳定生成，Name 回落文件名；
// 子目录里的 README.md 仍按说明文件排除。
func TestRootMarkdownWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	writeTestFile(t, filepath.Join(dir, "README.md"), "# 集合说明\n")
	writeTestFile(t, filepath.Join(dir, "guide.md"), "# 指南\n")
	writeTestFile(t, filepath.Join(dir, "docs", "README.md"), "# 子目录说明\n")

	docs := docsByPath(t, c)

	readme, ok := docs["README.md"]
	if !ok {
		t.Fatalf("根目录 README.md 应作为文档条目出现，实际条目: %v", pathsOf(docs))
	}
	if want := mdUIDFor("README.md"); readme.UID != want {
		t.Fatalf("README.md 的 uid 应为 %q（路径哈希），实际 %q", want, readme.UID)
	}
	if readme.Name != "README" {
		t.Fatalf("无 frontmatter 时 Name 应回落文件名 README，实际 %q", readme.Name)
	}
	if readme.Content != "# 集合说明\n" {
		t.Fatalf("无 frontmatter 时 Content 应为全文，实际 %q", readme.Content)
	}

	guide, ok := docs["guide.md"]
	if !ok {
		t.Fatalf("根目录 guide.md 应作为文档条目出现，实际条目: %v", pathsOf(docs))
	}
	if want := mdUIDFor("guide.md"); guide.UID != want {
		t.Fatalf("guide.md 的 uid 应为 %q，实际 %q", want, guide.UID)
	}
	if guide.Name != "guide" {
		t.Fatalf("guide.md 的 Name 应为 guide，实际 %q", guide.Name)
	}

	if _, ok := docs["docs/README.md"]; ok {
		t.Fatalf("子目录 docs/README.md 不应作为文档条目出现")
	}

	// 两次扫描 uid 必须一致（稳定，不随时间/遍历顺序变化）
	again := docsByPath(t, c)
	if again["README.md"] == nil || again["README.md"].UID != readme.UID {
		t.Fatalf("两次扫描的 README.md uid 不一致: %v → %v", readme.UID, again["README.md"])
	}

	// 首次保存：frontmatter 写回文件（含该 uid），之后读回 uid 不变
	readme.Content = "# 集合说明\n\n补充一行。\n"
	if err := c.SaveDoc(readme); err != nil {
		t.Fatalf("SaveDoc(README): %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("读取保存后的 README.md: %v", err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("保存后文件顶部应有 frontmatter，实际: %q", text)
	}
	if !strings.Contains(text, "uid: "+readme.UID) {
		t.Fatalf("保存后 frontmatter 应包含稳定 uid %q，实际: %q", readme.UID, text)
	}
	if !strings.HasSuffix(text, "补充一行。\n") {
		t.Fatalf("保存后正文应原样写回，实际: %q", text)
	}

	after := docsByPath(t, c)
	if after["README.md"] == nil || after["README.md"].UID != readme.UID {
		t.Fatalf("保存后读回的 uid 应保持不变（%q），实际 %+v", readme.UID, after["README.md"])
	}

	// 树：根级（非分组内）包含该 doc 节点
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	var atRoot bool
	for _, n := range tree {
		if n.Type == "doc" && n.UID == readme.UID {
			atRoot = true
		}
	}
	if !atRoot {
		t.Fatalf("根级树应包含 README 文档节点（uid=%s）", readme.UID)
	}
}

// pathsOf 便于失败信息展示的路径列表。
func pathsOf(docs map[string]*DocEntry) []string {
	out := make([]string, 0, len(docs))
	for p := range docs {
		out = append(out, p)
	}
	return out
}

// TestRootMarkdownWithHorizontalRules 无 frontmatter 的 README 若以 `---` 开头且中间不是合法 YAML
// （Markdown 分隔线 / 列表），不能整条丢弃：回落为「全文即正文」，uid 仍按路径哈希稳定生成。
func TestRootMarkdownWithHorizontalRules(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	const readme = `---
# 集合说明
- 第一条
---
- 第二条
---
正文
`
	writeTestFile(t, filepath.Join(dir, "README.md"), readme)

	docs := docsByPath(t, c)
	d, ok := docs["README.md"]
	if !ok {
		t.Fatalf("含 Markdown 分隔线的 README.md 应作为文档条目出现，实际条目: %v", pathsOf(docs))
	}
	if d.Content != readme {
		t.Fatalf("frontmatter 解析失败应回落为全文即正文，实际 Content=%q", d.Content)
	}
	if want := mdUIDFor("README.md"); d.UID != want {
		t.Fatalf("uid 应为路径哈希 %q，实际 %q", want, d.UID)
	}
	again := docsByPath(t, c)
	if again["README.md"] == nil || again["README.md"].UID != d.UID {
		t.Fatalf("两次扫描 uid 应一致: %v → %+v", d.UID, again["README.md"])
	}
}

// TestMoveDocKeepsSyntheticUID 无 frontmatter 文档的 uid 来自路径哈希：移动前必须把 uid 写回
// frontmatter，否则 rename 后路径变化会让 uid 改变（已开页签/引用失效）。
func TestMoveDocKeepsSyntheticUID(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	writeTestFile(t, filepath.Join(dir, "guide.md"), "# 指南\n")
	before := docsByPath(t, c)
	g := before["guide.md"]
	if g == nil {
		t.Fatalf("根级 guide.md 应作为文档条目出现，实际条目: %v", pathsOf(before))
	}
	if !strings.HasPrefix(g.UID, "md-") {
		t.Fatalf("无 frontmatter 的文档 uid 应为路径哈希（md- 前缀），实际 %q", g.UID)
	}

	if err := c.CreateFolder("", "docs"); err != nil {
		t.Fatalf("CreateFolder(docs): %v", err)
	}
	if err := c.MoveDoc(g.UID, "docs"); err != nil {
		t.Fatalf("MoveDoc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "guide.md")); err != nil {
		t.Fatalf("移动后文件应存在于 docs/guide.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "guide.md")); !os.IsNotExist(err) {
		t.Fatalf("移动后旧路径应不存在")
	}

	raw, err := os.ReadFile(filepath.Join(dir, "docs", "guide.md"))
	if err != nil {
		t.Fatalf("读取移动后的文件: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\n") || !strings.Contains(string(raw), "uid: "+g.UID) {
		t.Fatalf("移动后文件应写回带 uid 的 frontmatter，实际: %q", string(raw))
	}

	after := docsByPath(t, c)
	if _, ok := after["guide.md"]; ok {
		t.Fatalf("旧路径不应再出现在 ListDocs")
	}
	moved := after["docs/guide.md"]
	if moved == nil {
		t.Fatalf("移动后应能在 docs/guide.md 找到文档，实际条目: %v", pathsOf(after))
	}
	if moved.UID != g.UID {
		t.Fatalf("移动后 uid 应保持不变（%q），实际 %q", g.UID, moved.UID)
	}
	if moved.Content != "# 指南\n" {
		t.Fatalf("移动后正文应原样保留，实际 %q", moved.Content)
	}

	// 树里仍只有一条 doc
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	docCount := 0
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if n.Type == "doc" {
				docCount++
			}
			if n.Type == "folder" {
				walk(n.Children)
			}
		}
	}
	walk(tree)
	if docCount != 1 {
		t.Fatalf("树里应只有 1 条文档条目，实际 %d", docCount)
	}

	again := docsByPath(t, c)
	if again["docs/guide.md"] == nil || again["docs/guide.md"].UID != g.UID {
		t.Fatalf("再次扫描 uid 应不变（%q），实际 %+v", g.UID, again["docs/guide.md"])
	}
}

// TestMoveRootReadmeIntoSudirRejected 根级 README.md 移进子目录后会「扫不出来」：
// 入口必须拒绝且不改动文件（uid、路径、树都保持原样）。
func TestMoveRootReadmeIntoSudirRejected(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	writeTestFile(t, filepath.Join(dir, "README.md"), "# 集合说明\n")
	writeTestFile(t, filepath.Join(dir, "docs", "keep.txt"), "x") // 让 docs/ 存在
	readme := docsByPath(t, c)["README.md"]
	if readme == nil {
		t.Fatalf("根级 README.md 应先作为文档条目出现")
	}

	err = c.MoveDoc(readme.UID, "docs")
	if err == nil {
		t.Fatalf("根级 README.md 移进子目录应被拒绝")
	}
	if !strings.Contains(err.Error(), "请先改名再移动") {
		t.Fatalf("错误提示应说明先改名再移动，实际: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "README.md")); statErr != nil {
		t.Fatalf("拒绝后根级文件应保持不变: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "docs", "README.md")); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝后不应在 docs/ 下产生 README.md")
	}

	after := docsByPath(t, c)
	if after["README.md"] == nil || after["README.md"].UID != readme.UID {
		t.Fatalf("拒绝后 README 条目 uid 应不变，实际 %+v", after["README.md"])
	}
	if _, ok := after["docs/README.md"]; ok {
		t.Fatalf("docs/README.md 不应出现在 ListDocs")
	}
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	found := false
	for _, n := range tree {
		if n.Type == "doc" && n.UID == readme.UID {
			found = true
		}
	}
	if !found {
		t.Fatalf("拒绝后树里仍应有根级 README 节点")
	}
}

// TestCreateDocReadmeInSubdirRejected 子目录里新建 README（README / README.md）会造出看不见的文档：
// 入口拒绝且不产生文件；根目录（folder="."）仍允许。
func TestCreateDocReadmeInSubdirRejected(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	for _, name := range []string{"README", "README.md"} {
		if _, err := c.CreateDoc(name, "", "docs"); err == nil {
			t.Fatalf("子目录新建 %q 应被拒绝", name)
		} else if !strings.Contains(err.Error(), "请先改名再创建") {
			t.Fatalf("错误提示应说明先改名再创建，实际: %v", err)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, "docs", "README.md")); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝后不应产生 docs/README.md")
	}

	// 根级 README 允许：folder="." 表示集合根（空 folder 仍默认 docs/，语义不变）
	d, err := c.CreateDoc("README", "", ".")
	if err != nil {
		t.Fatalf("根级 CreateDoc(README) 应允许: %v", err)
	}
	docs := docsByPath(t, c)
	got := docs["README.md"]
	if got == nil || got.UID != d.UID {
		t.Fatalf("根级 README 应作为可见文档条目出现，实际 %+v", got)
	}
}

// TestRenameDocToReadmeInSubdirRejected 子目录里的文档改名为 README：入口拒绝，名称与文件都不变。
func TestRenameDocToReadmeInSubdirRejected(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	d, err := c.CreateDoc("指南", "", "docs")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	err = c.RenameDoc(d.UID, "README")
	if err == nil {
		t.Fatalf("子目录文档改名为 README 应被拒绝")
	}
	if !strings.Contains(err.Error(), "请换一个名称") {
		t.Fatalf("错误提示应说明换一个名称，实际: %v", err)
	}
	got, err := c.ReadDoc(d.UID)
	if err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
	if got.Name != "指南" {
		t.Fatalf("拒绝后显示名应保持不变，实际 %q", got.Name)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "docs", "指南.md")); statErr != nil {
		t.Fatalf("拒绝后文件名应保持不变: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "docs", "README.md")); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝后不应出现 docs/README.md")
	}
}
