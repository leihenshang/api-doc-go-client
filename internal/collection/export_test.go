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
	if _, err := c.CreateDoc("说明文档", "", ""); err != nil {
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
	mustFolder("", "A")    // 第一个顶层目录：默认展开
	mustFolder("A", "A-子") // 嵌套目录：默认折叠
	mustFolder("", "B")    // 第二个顶层目录：默认折叠
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
	// 请求标题带前置方法徽标（item 6：与客户端一致展示 HTTP 方法）。
	wants := []struct {
		id   string
		text string
	}{
		{"toc-0", "带文档标题的集合"},
		{"toc-1", `<span class="tm m-get">GET</span> 请求一`},
		{"toc-2", "文档标题A"},
		{"toc-3", "文档标题B"},
		{"toc-4", `<span class="tm m-get">GET</span> 请求二`},
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
		t.Fatalf("HTML 中表格数 = %d，期望至少 2（参数表 + 请求体表）:\n%s", got, htmlOut)
	}
}

// TestExportHTMLDocsNestedUnderRequest 验证 HTML 导出把请求的 Docs 章节作为请求标题的子节输出：
//   - Docs 内的 ## 标题整体降为 <h3>（请求标题是 <h2>），单节展示（showSection 按标题级别圈节）
//     不再把 Docs 排除在该 API 小节之外；
//   - Docs 里的 - 列表渲染成 <ul><li>（生成的接口文档大量使用列表）；
//   - TOC 请求条目带与客户端 MethodTag 同色值的方法徽标；
//   - Markdown 导出保持原层级不变（## 仍是 ##）。
func TestExportHTMLDocsNestedUnderRequest(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "用户-列表", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "{{host}}/api/user/list"
	r.Docs = "## 简要描述\n- **名称**: 用户列表\n- 分页查询\n\n## 响应字段\n\n| 参数名 | 必选 | 类型 | 说明 |\n| --- | --- | --- | --- |\n| id | 是 | string | 用户ID |\n"
	// item 8/9：带响应字段文档与快照的请求，导出必须把「响应字段/样例/响应头」补进生成内容
	r.RespDoc = &ResponseDoc{
		Type:    "object",
		Params:  []ResponseParam{{FieldName: "total", Type: "number", Required: true, Description: "总数", Example: "66"}},
		Sample:  `{"total":66}`,
		Status:  200,
		Headers: []KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
	}
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}
	r2, err := c.CreateRequest("", "用户-新建", "POST")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r2.URL = "{{host}}/api/user"
	if err := c.SaveRequest(r2); err != nil {
		t.Fatalf("保存: %v", err)
	}

	// Markdown 导出：Docs 标题层级保持原样
	md, err := c.ExportMarkdown()
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	if !strings.Contains(md, "## 简要描述") {
		t.Fatalf("Markdown 导出不应改动 Docs 标题层级:\n%s", md)
	}
	// item 9：响应字段表/样例/响应头进生成内容，且「文档」分节在生成内容之下（带分隔线 label）
	for _, want := range []string{"**响应字段**", "| `total` | 是 | number | 总数 | 66 |", "**响应样例**", "**响应头**", "**文档**"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 导出缺少 %q:\n%s", want, md)
		}
	}
	if strings.Index(md, "**响应字段**") > strings.Index(md, "**文档**") {
		t.Fatalf("「文档」应在生成的响应内容之后:\n%s", md)
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	// 请求标题仍是 <h2>，Docs 标题降为 <h3>（toc 顺序：集合 h1 / 请求 h2 / 两个 Docs h3…）；
	// HTML 正文标题的方法徽标前置且与 TOC 同源（item 6：同客户端一致展示 HTTP 方法）
	if !strings.Contains(htmlOut, `<h2 id="toc-1"><span class="tm m-get">GET</span> 用户-列表</h2>`) {
		t.Fatalf("请求标题不是预期的 h2（徽标前置）:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `<h3 id="toc-2">简要描述</h3>`) {
		t.Fatalf("Docs 标题未降级嵌套到请求下（应出现 h3「简要描述」）:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `<h3 id="toc-3">响应字段</h3>`) {
		t.Fatalf("Docs「响应字段」小节缺失或层级不对:\n%s", htmlOut)
	}
	// 列表渲染为 <ul><li>，行内 **粗体** 生效
	if !strings.Contains(htmlOut, "<ul>\n<li><strong>名称</strong>: 用户列表</li>") {
		t.Fatalf("Docs 列表未正确渲染:\n%s", htmlOut)
	}
	// Docs 字段表格仍在
	if !strings.Contains(htmlOut, "<td>id</td>") || !strings.Contains(htmlOut, "<td>用户ID</td>") {
		t.Fatalf("Docs 响应字段表格内容缺失:\n%s", htmlOut)
	}
	// item 9/10：集合树在左；右侧是「当前请求」的小节大纲（URL/参数/请求头/文档内容等，JS 动态生成可点击跳转）；
	// 请求行整行选中态 + 徽标反色（CSS 与 JS 都要在）。
	if strings.Contains(htmlOut, "order: 2") || !strings.Contains(htmlOut, `<nav class="ol" aria-label="当前页目录">`) ||
		!strings.Contains(htmlOut, "function buildOutline") {
		t.Fatalf("缺少右侧当前请求大纲或集合树未回左侧:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, ".toc-request:hover, .toc-request.selected { background: #18a058; }") ||
		!strings.Contains(htmlOut, ".toc-request:hover .tm, .toc-request.selected .tm { background: #fff") ||
		!strings.Contains(htmlOut, "classList.add('selected')") {
		t.Fatalf("请求行选中态/徽标反色缺失:\n%s", htmlOut)
	}
	// TOC 方法徽标：GET 绿 / POST 蓝（class 与客户端 token 同名）
	if !strings.Contains(htmlOut, `<span class="tm m-get">GET</span>`) {
		t.Fatalf("TOC 缺少 GET 方法徽标:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `<span class="tm m-post">POST</span>`) {
		t.Fatalf("TOC 缺少 POST 方法徽标:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, ".m-get { color: #18a058; }") || !strings.Contains(htmlOut, ".m-post { color: #2080f0; }") {
		t.Fatalf("TOC 方法徽标配色与客户端不一致:\n%s", htmlOut)
	}
	// TOC 只给 folder/request 编号，Docs 子标题占正文锚点但不进 TOC：
	// TOC 链接集合必须是正文锚点集合的子集，且请求二链接指向 toc-4（中间两个编号被请求一的 Docs 占用）。
	hrefs := collectTOCIDs(htmlOut, `href="#toc-`)
	ids := collectTOCIDs(htmlOut, `id="toc-`)
	idSet := map[int]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	for _, h := range hrefs {
		if !idSet[h] {
			t.Fatalf("TOC 链接 %d 找不到对应正文锚点:\n  hrefs=%v\n  ids=%v", h, hrefs, ids)
		}
	}
	if !idSet[4] || hrefs[len(hrefs)-1] != 4 {
		t.Fatalf("请求二的 TOC 链接应指向 toc-4（被请求一的 2 个 Docs 标题占用两号），hrefs=%v", hrefs)
	}
}

// TestExportHTMLSafeHTMLTags 验证 Docs 内 Markdown 与 HTML 标签混用的处理口径：
// 白名单排版标签（<br>/<details>/<b>…）保留，script/事件属性/javascript: 链接被清洗或转义。
func TestExportHTMLSafeHTMLTags(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "标签混用", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.Docs = strings.Join([]string{
		"## 说明",
		"第一行<br>第二行",
		"这是 <b>粗体</b> 与 <a href=\"https://example.com\" onclick=\"evil()\">安全链接</a>",
		"<details>",
		"<summary>展开详情</summary>",
		"详情正文",
		"</details>",
		"危险：<script>alert(1)</script><a href=\"javascript:evil()\">x</a><img src=x onerror=alert(1)>",
		"代码里的标签：`<div>` 原样显示",
	}, "\n")
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	// 恰好 1 个 <script>（页面自带的交互脚本）：Docs 里的 script 必须被转义
	if got := strings.Count(htmlOut, "<script>"); got != 1 {
		t.Fatalf("HTML 含 %d 个原生 <script>，Docs 内脚本必须转义:\n%s", got, htmlOut)
	}
	if !strings.Contains(htmlOut, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("Docs 内 script 标签未按文本转义:\n%s", htmlOut)
	}
	// 白名单标签保留
	for _, want := range []string{"第一行<br>第二行", "<b>粗体</b>", "<details>", "<summary>展开详情</summary>", "</details>"} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("白名单 HTML 未保留，缺 %q:\n%s", want, htmlOut)
		}
	}
	// 安全链接保留 href，但事件属性被删除
	if !strings.Contains(htmlOut, `<a href="https://example.com">安全链接</a>`) {
		t.Fatalf("安全链接被误删:\n%s", htmlOut)
	}
	if strings.Contains(htmlOut, "onclick") || strings.Contains(htmlOut, "onerror") {
		t.Fatalf("事件属性未被清洗:\n%s", htmlOut)
	}
	if strings.Contains(htmlOut, `href="javascript:`) {
		t.Fatalf("javascript: 链接未被剔除:\n%s", htmlOut)
	}
	// 行内代码中的标签转义显示
	if !strings.Contains(htmlOut, "<code>&lt;div&gt;</code>") {
		t.Fatalf("行内代码中的 HTML 标签应转义显示:\n%s", htmlOut)
	}
}

// TestShiftHeadings 标题降级纯函数：整体平移、封到 h6、代码围栏内的 # 不算标题。
func TestShiftHeadings(t *testing.T) {
	in := "# A\n## B\n\n```\n# not a heading\n```\n"
	got := shiftHeadings(in, 3)
	want := "### A\n#### B\n\n```\n# not a heading\n```\n"
	if got != want {
		t.Fatalf("shiftHeadings=\n%q\nwant\n%q", got, want)
	}
	// 已经足够深时原样返回
	deep := "##### A\n###### B\n"
	if got2 := shiftHeadings(deep, 3); got2 != deep {
		t.Fatalf("深层标题不应被改动:\n%q", got2)
	}
	// 深层封顶 h6（标题数量不变）
	flat := "# A\n###### B\n"
	got3 := shiftHeadings(flat, 4)
	if !strings.Contains(got3, "#### A\n") || !strings.Contains(got3, "###### B\n") {
		t.Fatalf("深层标题应封到 h6:\n%q", got3)
	}
}

// TestInfoRespUIDs 集合概要携带「响应字段非空」的请求 uid 集合（侧栏 Resp 节点数据源）。
func TestInfoRespUIDs(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "有响应文档", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r2, err := c.CreateRequest("", "无响应文档", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.RespDoc = &ResponseDoc{
		Type:   "object",
		Params: []ResponseParam{{FieldName: "id", Type: "string", Description: "主键", Example: "1"}},
	}
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}

	info, err := c.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	set := map[string]bool{}
	for _, uid := range info.RespUIDs {
		set[uid] = true
	}
	if !set[r.UID] {
		t.Fatalf("RespUIDs 应包含带响应字段的请求 %q，got=%v", r.UID, info.RespUIDs)
	}
	if set[r2.UID] {
		t.Fatalf("RespUIDs 不应包含无响应文档的请求 %q，got=%v", r2.UID, info.RespUIDs)
	}

	// 直接查 RespUIDs 与 Info 口径一致
	uids, err := c.RespUIDs()
	if err != nil {
		t.Fatalf("RespUIDs: %v", err)
	}
	if len(uids) != 1 || uids[0] != r.UID {
		t.Fatalf("RespUIDs=%v，期望仅 [%s]", uids, r.UID)
	}
}
