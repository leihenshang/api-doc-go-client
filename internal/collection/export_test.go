package collection

import (
	"strings"
	"testing"
)

func TestExportMarkdownAndHTML(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	r, err := c.CreateRequest("用户", "用户-列表", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "{{host}}/api/user/list"
	r.Params = []KV{{Name: "page", Value: "1", Enabled: true, Description: "页码"}}
	r.Docs = "## 说明\n- 查询用户"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}

	md, err := c.ExportMarkdown()
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	for _, want := range []string{"# ", "用户-列表", "GET", "{{host}}/api/user/list", "page", "查询用户"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺 %q:\n%s", want, md)
		}
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	for _, want := range []string{"<!DOCTYPE html>", "<h1", "<h2", "用户-列表", "<table>", "查询用户", "</html>"} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("HTML 缺 %q:\n%s", want, htmlOut)
		}
	}
	// 导出产物允许内嵌一段交互脚本（目录树折叠/搜索），但不允许出现第二段 <script>
	// （那会来自未转义的用户内容）。用户提供的请求名/文档必须经 html.EscapeString 转义。
	if got := strings.Count(htmlOut, "<script>"); got != 1 {
		t.Fatalf("HTML 含 %d 个 <script>，期望恰好 1 个内嵌交互脚本（用户内容必须转义）:\n%s", got, htmlOut)
	}
	if !strings.Contains(htmlOut, "document.querySelector('.toc-list')") {
		t.Fatalf("HTML 缺少内嵌目录树交互脚本")
	}

	// 导出的 HTML 必须含目录树（TOC），且 TOC 锚点链接与正文标题 id 一一对应。
	if !strings.Contains(htmlOut, `class="toc"`) {
		t.Fatalf("HTML 缺少 TOC 目录树:\n%s", htmlOut)
	}
	// 目录树链接数 = 树中 folder/request 节点数 + 1（集合名），与 mdToHTML 的 toc-N 顺序一致。
	var countNodes func(nodes []*Node) int
	countNodes = func(nodes []*Node) int {
		n := 0
		for _, x := range nodes {
			if x.Type == "doc" {
				continue
			}
			n++
			n += countNodes(x.Children)
		}
		return n
	}
	wantTOC := countNodes(tree) + 1 // +1 为集合名
	if got := strings.Count(htmlOut, `href="#toc-`); got != wantTOC {
		t.Fatalf("TOC 链接数 = %d, 期望 %d:\n%s", got, wantTOC, htmlOut)
	}
	// 正文必须包含集合名锚点 id=toc-0
	if !strings.Contains(htmlOut, `id="toc-0"`) {
		t.Fatalf("HTML 缺少集合名锚点 id=toc-0:\n%s", htmlOut)
	}
}

// TestExportHTMLTOC 验证导出 HTML 的目录树（TOC）锚点与正文标题 id 严格一一对应：
// 每个 toc-N 链接都有对应的 id=toc-N 锚点，反之亦然（集合名占 toc-0，之后按树 DFS 先序分配，
// doc 节点不生成标题也不占 id）。
func TestExportHTMLTOC(t *testing.T) {
	c := newTestCollection(t)
	c.Name = "TOC 测试集合"

	// 多层 folder + 多个 request，并混入一个 doc 节点（不应占 id）。
	mustFolder := func(parent, name string) {
		if err := c.CreateFolder(parent, name); err != nil {
			t.Fatalf("建分组 %s/%s: %v", parent, name, err)
		}
	}
	mustFolder("", "A")
	mustFolder("A", "B")
	mustFolder("A/B", "C")
	mustFolder("", "D")
	mustFolder("A", "E")

	reqs := []struct{ folder, name string }{
		{"", "根请求"},
		{"A", "A 请求"},
		{"A/B", "B 请求"},
		{"A/B/C", "C 请求"},
		{"D", "D 请求"},
		{"A", "A 另一请求"},
		{"A/E", "E 请求"},
	}
	for _, r := range reqs {
		if _, err := c.CreateRequest(r.folder, r.name, "GET"); err != nil {
			t.Fatalf("建请求 %s/%s: %v", r.folder, r.name, err)
		}
	}
	// 文档节点（不应出现在 TOC 与 heading 中）
	if _, err := c.CreateDoc("说明文档", ""); err != nil {
		t.Fatalf("建文档: %v", err)
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}

	// 提取 TOC 链接集合 A（href="#toc-N"）与正文锚点集合 B（id="toc-N"）
	hrefs := collectTOCIDs(htmlOut, `href="#toc-`)
	ids := collectTOCIDs(htmlOut, `id="toc-`)

	// 1) A 与 B 作为集合必须完全相等（每个链接都有对应锚点，且反之亦然）
	if !equalSet(hrefs, ids) {
		t.Fatalf("TOC 链接集合 ≠ 正文锚点集合:\n  hrefs=%v\n  ids  =%v", hrefs, ids)
	}
	// 2) 集合应为 {0,1,2,...,N-1} 连续无重复无遗漏（id 分配无错位/无空洞/无重复）
	wantSeq := make([]int, len(ids))
	for i := range wantSeq {
		wantSeq[i] = i
	}
	if !equalSet(ids, wantSeq) {
		t.Fatalf("TOC id 不连续或重复: got=%v want=%v", ids, wantSeq)
	}
	// 3) 数量 = 树中 folder/request 节点数 + 1（集合名）；doc 节点不计入
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	var countNodes func(nodes []*Node) int
	countNodes = func(nodes []*Node) int {
		n := 0
		for _, x := range nodes {
			if x.Type == "doc" {
				continue
			}
			n++
			n += countNodes(x.Children)
		}
		return n
	}
	wantN := countNodes(tree) + 1
	if len(ids) != wantN {
		t.Fatalf("TOC 数量 = %d, 期望 %d (节点数+1)", len(ids), wantN)
	}
}

// TestExportHTMLTOCConnectorLines 验证导出 HTML 的目录树带树形连接线样式：
// 嵌套 ul/li 的 ::before（竖线）、::after（横线）、以及末项 :last-child::before（收尾成 └）。
// 该样式为内联 <style> 的新增规则，纯视觉；本测试作为回归守卫，确保连接线规则未被误删，
// 且确认它不改变既有的 TOC 锚点逻辑（id="toc-N" / href="#toc-N" 仍由 TestExportHTMLTOC 保证）。
func TestExportHTMLTOCConnectorLines(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "分组"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	if _, err := c.CreateRequest("分组", "请求一", "GET"); err != nil {
		t.Fatalf("建请求: %v", err)
	}
	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	// 树形连接线的三段关键 CSS 规则必须存在
	for _, want := range []string{
		`.toc-list ul > li::before`,
		`.toc-list ul > li::after`,
		`.toc-list ul > li:last-child::before`,
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("HTML 缺少目录树连接线样式 %q:\n%s", want, htmlOut)
		}
	}
	// 连接线规则存在的同时，TOC 锚点逻辑不受影响（集合名锚点仍生成）。
	if !strings.Contains(htmlOut, `id="toc-0"`) {
		t.Fatalf("连接线改动破坏了 TOC 锚点 id=toc-0:\n%s", htmlOut)
	}
}

// TestExportHTMLTOCToggleSearch 验证导出 HTML 的目录树支持折叠/展开与搜索：
//   - 含搜索框（class="toc-search" / type="search"）
//   - 每个 folder 一个折叠开关（class="toc-toggle"），且折叠 CSS 规则存在
//   - 默认仅第一个顶层目录展开（aria-expanded="true" / data-default="true" 全文各恰好 1 处）
func TestExportHTMLTOCToggleSearch(t *testing.T) {
	c := newTestCollection(t)
	c.Name = "可折叠目录测试"
	mustFolder := func(parent, name string) {
		if err := c.CreateFolder(parent, name); err != nil {
			t.Fatalf("建分组 %s/%s: %v", parent, name, err)
		}
	}
	mustFolder("", "A")     // 第一个顶层目录：默认展开
	mustFolder("A", "A-子") // 嵌套目录：默认折叠
	mustFolder("", "B")     // 第二个顶层目录：默认折叠
	if _, err := c.CreateRequest("A", "A 请求", "GET"); err != nil {
		t.Fatalf("建请求 A: %v", err)
	}
	if _, err := c.CreateRequest("B", "B 请求", "GET"); err != nil {
		t.Fatalf("建请求 B: %v", err)
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}

	// 1) 搜索框存在
	for _, want := range []string{`class="toc-search"`, `type="search"`, `placeholder="搜索目录…"`} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("HTML 缺少目录搜索框 %q:\n%s", want, htmlOut)
		}
	}
	// 2) 折叠开关（每个 folder 一个）与折叠 CSS 规则
	if !strings.Contains(htmlOut, `.toc-folder[aria-expanded="false"] > ul`) {
		t.Fatalf("HTML 缺少折叠 CSS 规则:\n%s", htmlOut)
	}
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	var countFolders func(nodes []*Node) int
	countFolders = func(nodes []*Node) int {
		n := 0
		for _, x := range nodes {
			if x.Type == "folder" {
				n++
				n += countFolders(x.Children)
			}
		}
		return n
	}
	if got := strings.Count(htmlOut, `class="toc-toggle"`); got != countFolders(tree) {
		t.Fatalf("toc-toggle 数量 = %d, 期望文件夹数 %d:\n%s", got, countFolders(tree), htmlOut)
	}
	// 3) 默认仅第一个顶层目录展开：仅第一个 folder 的 li 同时带 data-default="true" aria-expanded="true"。
	//    注意：CSS 规则 .toc-folder[aria-expanded="true"] 也含该文本，故用整段 folder 属性串精确匹配。
	if got := strings.Count(htmlOut, `class="toc-folder" data-default="true" aria-expanded="true"`); got != 1 {
		t.Fatalf("默认展开目录数 = %d, 期望仅 1（第一个顶层目录）:\n%s", got, htmlOut)
	}
	if got := strings.Count(htmlOut, `data-default="true"`); got != 1 {
		t.Fatalf("data-default=\"true\" 数量 = %d, 期望仅 1:\n%s", got, htmlOut)
	}
	// 其余 folder 默认折叠（data-default="false" aria-expanded="false"）
	if got := strings.Count(htmlOut, `class="toc-folder" data-default="false" aria-expanded="false"`); got != countFolders(tree)-1 {
		t.Fatalf("默认折叠目录数 = %d, 期望 %d:\n%s", got, countFolders(tree)-1, htmlOut)
	}
	// 4) 点击目录(fodler)不应在右侧展示其下 API：脚本需对 folder 与 request 分流
	//    （folder 仅切换 TOC 展开；只有 request 调 showSection）。防止误改回「点目录即展示」。
	if !strings.Contains(htmlOut, `classList.contains('toc-folder')`) {
		t.Fatalf("HTML 缺少目录点击分流逻辑（folder 不应展示 API）:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `showSection(href.slice(1))`) {
		t.Fatalf("HTML 缺少单节展示逻辑（点 API 应展示对应一节）:\n%s", htmlOut)
	}
}

// TestExportHTMLTOCWithDocHeadings 验证：当请求 Docs 内含 # 标题时，TOC 的 toc-N
// 编号仍与正文「实际标题顺序」严格对应——即给请求编号后需把其 Docs 内标题数计入计数器，
// 否则正文 id 会被文档子标题挤偏，点选靠后的 API 会命中错误内容（错位）。
func TestExportHTMLTOCWithDocHeadings(t *testing.T) {
	c := newTestCollection(t)
	c.Name = "带文档标题的集合"
	// 请求一：Docs 含 2 个 # 标题；请求二：无 Docs。
	r1, err := c.CreateRequest("", "请求一", "GET")
	if err != nil {
		t.Fatalf("建请求一: %v", err)
	}
	r1.Docs = "# 文档标题A\n# 文档标题B"
	if err := c.SaveRequest(r1); err != nil {
		t.Fatalf("保存请求一: %v", err)
	}
	if _, err := c.CreateRequest("", "请求二", "GET"); err != nil {
		t.Fatalf("建请求二: %v", err)
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}

	// 编号推演：toc-0=集合名；toc-1=请求一(其后 Docs 占 2 个序号 → tocID+2)；toc-4=请求二。
	// 正文实际标题：toc-0 集合 / toc-1 请求一 / toc-2 文档标题A / toc-3 文档标题B / toc-4 请求二。
	wants := []struct {
		id   string
		text string
	}{
		{"toc-0", "带文档标题的集合"},
		{"toc-1", "请求一"},
		{"toc-2", "文档标题A"},
		{"toc-3", "文档标题B"},
		{"toc-4", "请求二"},
	}
	for _, w := range wants {
		if !strings.Contains(htmlOut, `id="`+w.id+`">`+w.text) {
			t.Fatalf("正文缺少正确锚点 %s => %q:\n%s", w.id, w.text, htmlOut)
		}
	}
	// 关键：TOC 里请求二链接必须指向 toc-4（而非被文档标题挤到 toc-2），且文档子标题不进 TOC。
	if got := strings.Count(htmlOut, `href="#toc-4"`); got != 1 {
		t.Fatalf("TOC 中请求二链接数 = %d, 期望 1（应指向 toc-4）:\n%s", got, htmlOut)
	}
	if got := strings.Count(htmlOut, `href="#toc-2"`); got != 0 {
		t.Fatalf("文档子标题不应出现在 TOC（href=#toc-2 数 = %d）:\n%s", got, htmlOut)
	}
}

// collectTOCIDs 从 HTML 中抽取所有形如 prefix"N" 的数字 N（prefix 形如 `href="#toc-` 或 `id="toc-`）。
func collectTOCIDs(html, prefix string) []int {
	var out []int
	from := 0
	for {
		i := strings.Index(html[from:], prefix)
		if i < 0 {
			break
		}
		start := from + i + len(prefix)
		j := start
		for j < len(html) && html[j] >= '0' && html[j] <= '9' {
			j++
		}
		if j > start {
			n := 0
			for k := start; k < j; k++ {
				n = n*10 + int(html[k]-'0')
			}
			out = append(out, n)
		}
		from = j
	}
	return out
}

// equalSet 比较两个整型切片作为集合是否相等（忽略顺序）。
func equalSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[int]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
	}
	for _, v := range seen {
		if v != 0 {
			return false
		}
	}
	return true
}

// TestExportMarkdownBodyFormTable 回归：请求同时含 URL 参数与请求体（表单）时，
// 两者在生成的 Markdown 中「分开展示」为两张独立的表格（参数 / 请求体参数），
// 请求体表单不再以无序列表渲染。
func TestExportMarkdownBodyFormTable(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "订单"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	r, err := c.CreateRequest("订单", "订单-创建", "POST")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	// URL 参数
	r.Params = []KV{{Name: "version", Value: "2", Enabled: true, Description: "API 版本"}}
	// 请求体：表单字段（原应渲染为表格，而非列表）
	r.Body = Body{
		Type: "form-urlencoded",
		Form: []KV{
			{Name: "username", Value: "admin", Enabled: true},
			{Name: "pwd", Value: "123", Enabled: true},
		},
	}
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存请求: %v", err)
	}

	md, err := c.ExportMarkdown()
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}

	// 1) 两个章节各自有独立表格标题
	if !strings.Contains(md, "**参数**") {
		t.Fatalf("Markdown 缺少 URL 参数表格标题:\n%s", md)
	}
	if !strings.Contains(md, "**请求体参数**") {
		t.Fatalf("Markdown 缺少请求体参数表格标题（应分开展示为表格）:\n%s", md)
	}
	// 2) 请求体表单字段以表格行呈现（带 | 分隔），而非无序列表
	if !strings.Contains(md, "| `username` | `admin` |") {
		t.Fatalf("请求体表单未以表格行展示:\n%s", md)
	}
	if strings.Contains(md, "- `username`") {
		t.Fatalf("请求体表单错误地以无序列表展示（应分开展示为表格）:\n%s", md)
	}
	// 3) URL 参数也在其表中
	if !strings.Contains(md, "| `version` | `2` |") {
		t.Fatalf("URL 参数未出现在参数表中:\n%s", md)
	}

	// 4) 导出 HTML 中两张表都应存在
	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	if got := strings.Count(htmlOut, "<table>"); got < 2 {
		t.Fatalf("HTML 中表格数 = %d, 期望至少 2（参数表 + 请求体表）:\n%s", got, htmlOut)
	}
}
