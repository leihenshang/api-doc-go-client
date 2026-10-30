package mcp

// 文档条目（集合树里的独立 Markdown 文件）在 MCP 侧的读写。
//
// 先分清两种「doc」，它们的载体与工具完全不同：
//   - request.docs：接口自带的说明，存在请求文件的 frontmatter 里，
//     走 create_request / update_request 的 docs 字段，get_request_detail 以「说明文档」返回；
//   - doc 条目：集合树里独立的 .md（uid 独立、有自己的路径，可挂在任意分组目录下），
//     客户端能新建/改名/移动/删除。此前 MCP 只在 get_request_detail 的 relatedDocs 里露出文件名，
//     既读不到正文也改不了 —— 本文件补齐这部分（list/get/create/update/delete_doc）。

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
)

var (
	errNeedDocName = coded(CodeInvalid, errors.New("缺少 name：创建文档必须给名称"))
	errNeedDocUID  = coded(CodeInvalid, errors.New("缺少 uid：请先用 list_docs 找到目标文档"))
	errNeedDocRef  = coded(CodeInvalid, errors.New("缺少 uid 或 path：请先用 list_docs 找到目标文档（两者给一个即可）"))
)

// DocItem 文档条目元信息（不含正文），list_docs 的一行。
// 名字避开 service.go 里已有的 DocBrief（那是 get_request_detail 的 relatedDocs 用的精简版）。
type DocItem struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Path string `json:"path"`
	Icon string `json:"icon,omitempty"`
	Seq  int    `json:"seq,omitempty"`
	Size int    `json:"size"` // 正文字节数：AI 可据此判断要不要 get_doc 取全文
	// Hash 文档文件当前 hash（默认返回）：可直接回传 update_doc.ifMatch，省一次 get_doc。
	Hash string `json:"hash,omitempty"`
}

// DocDetail 文档详情：正文 + 磁盘 hash。
// hash 可回传给 update_doc.ifMatch —— 与请求的 ifMatch 同语义，防「读完到写回之间被外部改动」。
type DocDetail struct {
	DocItem
	Content string `json:"content"`
	Hash    string `json:"hash,omitempty"`
	// Planned dryRun 时的动作（create / overwrite / update）；正常写入为空。
	Planned string `json:"planned,omitempty"`
	// PrevContent 仅 dryRun 更新时用于计算「改动行」预览（不序列化给客户端）。
	PrevContent string `json:"-"`
}

// ListDocs 列出项目里的文档条目（按路径排序）；query 对名称/路径做大小写不敏感的子串过滤。
// limit/offset 分页（limit<=0 用默认 200，上限 1000）；includeHash 默认由调用方传 true。
func (s *Service) ListDocs(project, query string, limit, offset int, includeHash bool) ([]DocItem, int, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return nil, 0, err
	}
	docs, err := a.ListDocs()
	if err != nil {
		return nil, 0, coded(CodeIO, fmt.Errorf("列不出文档: %w", err))
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]DocItem, 0, len(docs))
	for _, d := range docs {
		if q != "" &&
			!strings.Contains(strings.ToLower(d.Name), q) &&
			!strings.Contains(strings.ToLower(d.Path), q) {
			continue
		}
		out = append(out, docItem(d))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	total := len(out)
	if offset >= total {
		return []DocItem{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := out[offset:end]
	if includeHash {
		if hashes, herr := a.DocHashes(); herr == nil {
			for i := range page {
				page[i].Hash = hashes[page[i].UID]
			}
		}
	}
	return page, total, nil
}

// GetDoc 读单篇文档（含正文）；按 uid 定位。
func (s *Service) GetDoc(project, uid string) (*DocDetail, error) {
	return s.GetDocAt(project, uid, "")
}

// GetDocAt 读单篇文档：uid 与 path 二选一（path 支持集合内相对路径或纯文件名）。
func (s *Service) GetDocAt(project, uid, docPath string) (*DocDetail, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return nil, err
	}
	uid, err = s.resolveDocUID(a, uid, docPath)
	if err != nil {
		return nil, err
	}
	d, err := a.ReadDoc(uid)
	if err != nil {
		return nil, withNotFound(fmt.Errorf("读取文档 %s 失败（用 list_docs 查看可用 uid）: %w", uid, err))
	}
	return &DocDetail{DocItem: docItem(d), Content: d.Content, Hash: a.DocFileHash(uid)}, nil
}

// resolveDocUID 把「uid 或 path」统一成 uid；两者都给时以 uid 为准。
//
// path 依次匹配：集合内相对路径（docs/guide.md）、去掉 .md 的路径（docs/guide）、
// 不含目录时的纯文件名（guide.md）。精确命中优先于文件名命中；找不到/歧义时
// 报错并列出候选 —— 调用方常常只有「用户说的那个文件名」，让它再猜一次不如直接给清单。
func (s *Service) resolveDocUID(a ProjectApp, uid, docPath string) (string, error) {
	if uid = strings.TrimSpace(uid); uid != "" {
		return uid, nil
	}
	p := strings.TrimSpace(filepath.ToSlash(docPath))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", errNeedDocRef
	}
	docs, err := a.ListDocs()
	if err != nil {
		return "", coded(CodeIO, fmt.Errorf("列不出文档: %w", err))
	}
	var exact, loose []*collection.DocEntry
	for _, d := range docs {
		dp := strings.TrimPrefix(filepath.ToSlash(d.Path), "/")
		switch {
		case dp == p || strings.TrimSuffix(dp, ".md") == p:
			exact = append(exact, d)
		case !strings.Contains(p, "/") && path.Base(dp) == p:
			loose = append(loose, d)
		}
	}
	hit := exact
	if len(hit) == 0 {
		hit = loose
	}
	switch len(hit) {
	case 1:
		return hit[0].UID, nil
	case 0:
		return "", withNotFound(fmt.Errorf("找不到文档 %q（用 list_docs 查看可用路径）：%s", p, docCandidates(docs)))
	default:
		names := make([]string, 0, len(hit))
		for _, d := range hit {
			names = append(names, d.Path)
		}
		return "", coded(CodeConflict, fmt.Errorf("路径 %q 匹配到多篇文档：%s；请传完整 path 或 uid", p, strings.Join(names, ", ")))
	}
}

// docCandidates 找不到文档时的候选清单（最多 5 条，避免报错本身又把上下文打爆）。
func docCandidates(docs []*collection.DocEntry) string {
	const maxShown = 5
	if len(docs) == 0 {
		return "项目里还没有文档（可用 create_doc 新建）"
	}
	parts := make([]string, 0, maxShown)
	for i, d := range docs {
		if i >= maxShown {
			break
		}
		parts = append(parts, fmt.Sprintf("%s（uid=%s）", d.Path, d.UID))
	}
	out := strings.Join(parts, "、")
	if len(docs) > maxShown {
		out += fmt.Sprintf(" 等 %d 篇", len(docs))
	}
	return out
}

// CreateDocInput create_doc 的入参。
type CreateDocInput struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Name    string `json:"name" jsonschema:"文档显示名（不含 .md，不能带路径分隔符）；可重复，身份以 uid/path 为准"`
	Folder  string `json:"folder,omitempty" jsonschema:"目标分组目录（如 docs 或 用户/说明，须已存在）；留空 = 默认 docs/（不存在会自动创建）"`
	Content string `json:"content,omitempty" jsonschema:"Markdown 正文；留空 = 空文档，之后用 update_doc 写入"`
	// UID 可选：批量导入/幂等创建时传固定 uid；已存在时按 onConflict 处理。
	UID string `json:"uid,omitempty" jsonschema:"可选：指定 uid（批量导入做幂等）；已存在时按 onConflict 处理"`
	// OnConflict 同名路径/同 uid 的冲突策略：error（默认）| suffix | overwrite。
	OnConflict string `json:"onConflict,omitempty" jsonschema:"已存在时：error（默认，回 [CONFLICT] + 已有 uid/path）/ suffix（-2）/ overwrite（复用覆盖正文）"`
	// DryRun 只做校验与冲突判定，返回将发生的变化，不落盘。
	DryRun bool `json:"dryRun,omitempty" jsonschema:"true = 只返回将创建/覆盖的路径与冲突判断，不写盘"`
	// FileName 可选：落盘文件名（不含 .md）。留空 = 由 name slug 化推导 —— 中文 name
	// 会得到中文文件名（集合是纯文本、文件即真相源）；要 ASCII 名（脚本/工具链友好）就在这里给。
	FileName string `json:"fileName,omitempty" jsonschema:"落盘文件名（不含 .md）；留空 = 由 name 推导；需要 ASCII 名时显式传"`
}

// CreateDoc 新建文档条目；content 非空时一并写入。
func (s *Service) CreateDoc(in CreateDocInput) (*DocDetail, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errNeedDocName
	}
	mode, verr := collection.NormalizeDocConflict(in.OnConflict)
	if verr != nil {
		return nil, withInvalid(verr)
	}
	// 显式指定的目录必须已存在，但默认文档目录（docs）例外：不传与显式写 docs 等价，
	// 都由集合层兜底创建（AI 习惯显式写 docs，不能因此报错）。
	if folder := strings.TrimSpace(in.Folder); folder != "" &&
		strings.Trim(folder, "/") != collection.DefaultDocFolder && !a.DirExists(folder) {
		return nil, withNotFound(fmt.Errorf("分组 %q 不存在（用 list_docs 或 get_project_modules 查看现有路径）", folder))
	}
	// fileName 只在显式给了才生效：默认仍由 name slug 化推导（中文名落中文文件是既有取舍）。
	opts := collection.CreateDocOpts{
		Name:       name,
		UID:        strings.TrimSpace(in.UID),
		Folder:     strings.TrimSpace(in.Folder),
		OnConflict: mode,
		FileName:   strings.TrimSpace(in.FileName),
	}
	if in.DryRun {
		d, action, derr := a.PlanCreateDocWithOpts(opts)
		if derr != nil {
			if errors.Is(derr, collection.ErrDocConflict) {
				return nil, coded(CodeConflict, derr)
			}
			return nil, withInvalid(derr)
		}
		d.Content = in.Content
		return &DocDetail{DocItem: docItem(d), Content: in.Content, Planned: action}, nil
	}
	d, err := a.CreateDocWithOpts(opts)
	if err != nil {
		if errors.Is(err, collection.ErrDocConflict) {
			return nil, coded(CodeConflict, err)
		}
		return nil, withInvalid(err)
	}
	// CreateDoc 落盘的是空正文；有内容时再存一次（不传 ifMatch：刚创建的文件不存在并发问题）
	if in.Content != "" {
		d.Content = in.Content
		if err := a.SaveDoc(d); err != nil {
			return nil, err
		}
	}
	return &DocDetail{DocItem: docItem(d), Content: d.Content, Hash: a.DocFileHash(d.UID)}, nil
}

// UpdateDocInput update_doc 的入参（指针字段 = 只改传了的）。
type UpdateDocInput struct {
	Project string  `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID     string  `json:"uid,omitempty" jsonschema:"文档 uid（list_docs / get_doc 返回）；与 path 二选一"`
	Path    string  `json:"path,omitempty" jsonschema:"文档路径（如 docs/guide.md）；与 uid 二选一"`
	Name    *string `json:"name,omitempty" jsonschema:"新的显示名（只改 frontmatter 的 name，文件名不变）"`
	Content *string `json:"content,omitempty" jsonschema:"新正文；空串 = 清空；不传不动；与 replaces/append/insertAfter 互斥"`
	// Replaces 正文的局部替换：与 content 互斥；old 找不到即整单拒绝（不落盘）。
	Replaces []ReplaceOp `json:"replaces,omitempty" jsonschema:"正文替换 [{old,new,replaceAll}]；与 content/append/insertAfter 互斥；old 缺失即拒绝"`
	// Append 在正文末尾追加一段：长文档分段写入时用它，避免整篇重传撞输出上限。
	Append *string `json:"append,omitempty" jsonschema:"在正文末尾追加一段（自动补空行）；与 content/replaces/insertAfter 互斥"`
	// InsertAfter 在某段原文之后插入：anchor 必须唯一命中，找不到或命中多处都整单拒绝。
	InsertAfter *InsertAfterOp `json:"insertAfter,omitempty" jsonschema:"在某段原文之后插入（anchor 必须唯一命中）；与 content/replaces/append 互斥"`
	Folder      *string        `json:"folder,omitempty" jsonschema:"移动到该分组目录（相对集合根）；传空串 = 移到集合根；不传不动"`
	// Section 节点守卫：只改正文或只改元信息时用它，防止顺手带上另一类字段。
	Section string `json:"section,omitempty" jsonschema:"节点守卫：content（正文类）/ meta（name/folder）；省略 = 不限制"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"get_doc 返回的完整 hash；磁盘被外部改动则拒绝写入并回当前 hash"`
	// DryRun 只计算新正文/校验 ifMatch，返回预览，不落盘。
	DryRun bool `json:"dryRun,omitempty" jsonschema:"true = 只返回改动预览与 ifMatch 校验结果，不写盘"`
}

// InsertAfterOp 在 anchor 之后插入 text。
// 默认要求 anchor 唯一（防误插）；长文档里同一小标题出现多次时，用 Occurrence 指定第几处。
type InsertAfterOp struct {
	Anchor        string `json:"anchor,omitempty" jsonschema:"定位用的原文片段（必须与正文完全一致）；与 anchorHeading 二选一"`
	Text          string `json:"text" jsonschema:"要插入的内容（Markdown）"`
	Occurrence    int    `json:"occurrence,omitempty" jsonschema:"命中第几处（1 基）；不传 = 要求只出现一次（防误插）；传 2 表示第 2 处"`
	AnchorHeading string `json:"anchorHeading,omitempty" jsonschema:"按标题插入（如 '## 附录'）：插到该小节末尾；与 anchor 二选一"`
}

// validateDocSection 节点守卫：section=content 只允许正文类字段，meta 只允许 name/folder。
func validateDocSection(sec string, in UpdateDocInput) error {
	sec = strings.ToLower(strings.TrimSpace(sec))
	if sec == "" {
		return nil
	}
	present := make([]string, 0, 6)
	if in.Content != nil {
		present = append(present, "content")
	}
	if len(in.Replaces) > 0 {
		present = append(present, "replaces")
	}
	if in.Append != nil {
		present = append(present, "append")
	}
	if in.InsertAfter != nil {
		present = append(present, "insertAfter")
	}
	if in.Name != nil {
		present = append(present, "name")
	}
	if in.Folder != nil {
		present = append(present, "folder")
	}
	var allowed []string
	switch sec {
	case "content":
		allowed = []string{"content", "replaces", "append", "insertAfter"}
	case "meta":
		allowed = []string{"name", "folder"}
	default:
		return withInvalid(fmt.Errorf("不支持的 section %q：只支持 content（正文）/ meta（名称与目录），省略表示不限制", sec))
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, f := range allowed {
		allowedSet[f] = true
	}
	var bad []string
	for _, f := range present {
		if !allowedSet[f] {
			bad = append(bad, f)
		}
	}
	if len(bad) > 0 {
		return withInvalid(fmt.Errorf("section=%s 只允许改 %s，本次还传了越界字段 %s：请去掉这些字段，或改用对应的 section（content / meta）",
			sec, strings.Join(allowed, "/"), strings.Join(bad, "/")))
	}
	return nil
}

// nextDocContent 按入参算出新正文；changed=false 表示本次不动正文。
// content / replaces / append / insertAfter 四种方式互斥（同时给会让人猜不到以哪个为准）。
func nextDocContent(cur string, in UpdateDocInput) (string, bool, error) {
	modes := 0
	for _, on := range []bool{in.Content != nil, len(in.Replaces) > 0, in.Append != nil, in.InsertAfter != nil} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		return "", false, errors.New("content / replaces / append / insertAfter 互斥：一次只能用一种正文变更方式")
	}
	switch {
	case in.Content != nil:
		return *in.Content, true, nil
	case len(in.Replaces) > 0:
		next, err := applyReplaces(cur, in.Replaces)
		return next, true, err
	case in.Append != nil:
		add := *in.Append
		if strings.TrimSpace(add) == "" {
			return "", false, errors.New("append 不能为空：要清空正文请传 content=\"\"")
		}
		if cur == "" {
			return add, true, nil
		}
		sep := "\n"
		if !strings.HasSuffix(cur, "\n") {
			sep = "\n\n"
		}
		return cur + sep + add, true, nil
	case in.InsertAfter != nil:
		op := in.InsertAfter
		if strings.TrimSpace(op.AnchorHeading) != "" {
			next, herr := insertAfterHeading(cur, op.AnchorHeading, op.Occurrence, op.Text)
			if herr != nil {
				return "", false, herr
			}
			return next, true, nil
		}
		if op.Anchor == "" {
			return "", false, errors.New("insertAfter 需要 anchor 或 anchorHeading（二选一）")
		}
		if op.Occurrence <= 0 {
			// 不传 occurrence：要求唯一，避免长文档里插错位置。
			i := strings.Index(cur, op.Anchor)
			if i < 0 {
				return "", false, fmt.Errorf("insertAfter 未匹配到 anchor（未写入任何内容）：%s", clipRunes(op.Anchor, 60))
			}
			if strings.Contains(cur[i+len(op.Anchor):], op.Anchor) {
				return "", false, fmt.Errorf("insertAfter.anchor 命中多处（未写入任何内容）：%s；请给更长/更唯一的片段，或传 occurrence 指定第几处",
					clipRunes(op.Anchor, 60))
			}
			return insertAfterText(cur, i, op.Anchor, op.Text), true, nil
		}
		// 指定第 N 处：逐次向后找，找不到时如实告知实际出现次数。
		idx, from := -1, 0
		for n := 0; n < op.Occurrence; n++ {
			j := strings.Index(cur[from:], op.Anchor)
			if j < 0 {
				return "", false, fmt.Errorf("insertAfter.occurrence=%d 不存在（正文里只有 %d 处 anchor）：%s",
					op.Occurrence, n, clipRunes(op.Anchor, 60))
			}
			idx = from + j
			from = idx + len(op.Anchor)
		}
		return insertAfterText(cur, idx, op.Anchor, op.Text), true, nil
	}
	return cur, false, nil
}

// UpdateDoc 局部更新文档：正文（带 ifMatch 校验）/ 改名 / 移动可任意组合。
//
// 顺序：先写正文（此时做 hash 校验），再移动、改名 —— 后两者只动路径与 frontmatter，
// 不改文件内容，放在校验之后不会让 ifMatch 失效。
// insertAfterText 在锚点（起点 i）之后插入 text，按前后换行自动补空行。
func insertAfterText(cur string, i int, anchor, text string) string {
	at := i + len(anchor)
	rest := cur[at:]
	prefix := "\n\n"
	if strings.HasPrefix(rest, "\n\n") {
		prefix = ""
	} else if strings.HasPrefix(rest, "\n") {
		prefix = "\n"
	}
	ins := text
	if !strings.HasSuffix(ins, "\n") && !strings.HasPrefix(rest, "\n") {
		ins += "\n"
	}
	return cur[:at] + prefix + ins + rest
}

// insertAfterHeading 在指定 Markdown 标题所在小节的末尾插入 text。
// 小节范围：该标题行之后、下一个同级或更高级标题之前；occurrence 指定第几个同名标题（默认 1）。
//
// 标题按 normalizeHeading 归一化后比较：AI 抄标题时常带全角空格、多个空格或
// 「## 附录 ##」这类闭合写法，精确比较会假失败；找不到时列出文内标题，省掉一次盲重试。
func insertAfterHeading(cur, heading string, occurrence int, text string) (string, error) {
	norm := strings.ReplaceAll(cur, "\r\n", "\n")
	lines := strings.Split(norm, "\n")
	target := normalizeHeading(heading)
	if target == "" {
		return "", errors.New("insertAfter.anchorHeading 不能为空")
	}
	level := headingLevel(target)
	if level == 0 {
		return "", fmt.Errorf("anchorHeading 必须是 Markdown 标题（以 # 开头）：%s", clipRunes(heading, 60))
	}
	if occurrence <= 0 {
		occurrence = 1
	}
	seen, start := 0, -1
	outline := make([]string, 0, 8)
	for i, l := range lines {
		if headingLevel(strings.TrimSpace(l)) == 0 {
			continue
		}
		if len(outline) < 8 {
			outline = append(outline, strings.TrimSpace(l))
		}
		if normalizeHeading(l) != target {
			continue
		}
		seen++
		if seen == occurrence {
			start = i
			break
		}
	}
	if start < 0 {
		hint := ""
		if len(outline) > 0 {
			hint = "；文内标题：" + strings.Join(outline, " / ")
		}
		return "", fmt.Errorf("insertAfter.anchorHeading 未匹配到标题（未写入任何内容）：%s%s", clipRunes(heading, 60), hint)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if lv := headingLevel(strings.TrimSpace(lines[i])); lv > 0 && lv <= level {
			end = i
			break
		}
	}
	// 去掉小节末尾的空行，插入后统一补一个空行，保持 Markdown 段落结构。
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	head := strings.Join(lines[:end], "\n")
	tail := strings.Join(lines[end:], "\n")
	out := head + "\n\n" + strings.TrimRight(text, "\n") + "\n"
	if tail != "" {
		out += "\n" + tail
	}
	return out, nil
}

// normalizeHeading 归一化标题用于比较：全角空格按半角、连续空白折叠为一个空格，
// 去掉首尾空白、闭合式 ATX 标题的行尾 # 与尾部 * 强调。
func normalizeHeading(line string) string {
	s := strings.ReplaceAll(strings.TrimSpace(line), "\u3000", " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(strings.TrimRight(s, "#"))
	return strings.TrimSpace(strings.TrimRight(s, "*"))
}

// headingLevel 返回 Markdown 标题级别（1–6）；不是标题返回 0。
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

func (s *Service) UpdateDoc(in UpdateDocInput) (*DocDetail, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	uid, err := s.resolveDocUID(a, in.UID, in.Path)
	if err != nil {
		return nil, err
	}
	if err := validateDocSection(in.Section, in); err != nil {
		return nil, err
	}
	// 移动目标必须已存在，且要在写正文之前校验：否则会留下「正文已写、移动失败」的半成功状态。
	if in.Folder != nil {
		if target := strings.TrimSpace(*in.Folder); target != "" {
			if _, ok := a.FolderUIDByPath(target); !ok {
				return nil, withNotFound(fmt.Errorf("分组 %q 不存在（用 get_project_modules 或 list_docs 查看现有路径）", target))
			}
		}
	}
	d, err := a.ReadDoc(uid)
	if err != nil {
		return nil, withNotFound(fmt.Errorf("读取文档 %s 失败（用 list_docs 查看可用 uid）: %w", uid, err))
	}
	// 正文四种变更方式（content / replaces / append / insertAfter）互斥，统一在这里算新正文。
	next, changed, err := nextDocContent(d.Content, in)
	if err != nil {
		return nil, err
	}
	if in.DryRun {
		// 只算新正文 + 校验 ifMatch，不写盘（也不改名/移动）
		if want := strings.TrimSpace(in.IfMatch); want != "" {
			if cur := a.DocFileHash(uid); cur != "" && !strings.HasPrefix(cur, want) {
				return nil, conflictError("文档", uid, cur)
			}
		}
		out := *d
		out.Content = next
		return &DocDetail{DocItem: docItem(&out), Content: next, Hash: a.DocFileHash(uid), Planned: "update", PrevContent: d.Content}, nil
	}
	if changed {
		d.Content = next
		if err := a.SaveDocExpectHash(d, strings.TrimSpace(in.IfMatch)); err != nil {
			return nil, s.docWriteErr(a, uid, err)
		}
	}
	if in.Folder != nil {
		if err := a.MoveDoc(uid, strings.TrimSpace(*in.Folder)); err != nil {
			// 审核 L#10：正文已按声明顺序先写并校验，move 失败时把已完成步骤说清楚，避免调用方盲重试撞假冲突。
			if changed {
				return nil, coded(CodeOf(err), fmt.Errorf("正文已写入成功，但移动到 %q 失败：%w（重试可只传 folder，不重复传 content）", *in.Folder, err))
			}
			return nil, err
		}
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errNeedDocName
		}
		if err := a.RenameDoc(uid, name); err != nil {
			if changed || in.Folder != nil {
				return nil, coded(CodeOf(err), fmt.Errorf("已完成的步骤保留在盘上，仅改名 %q 失败：%w（重试可只传 name）", name, err))
			}
			return nil, err
		}
	}
	out, err := a.ReadDoc(uid)
	if err != nil {
		return nil, err
	}
	return &DocDetail{DocItem: docItem(out), Content: out.Content, Hash: a.DocFileHash(uid)}, nil
}

// DeleteDoc 移除文档（进 .trash，可人工找回）；按 uid 定位。
func (s *Service) DeleteDoc(project, uid string) error {
	return s.DeleteDocAt(project, uid, "")
}

// DeleteDocAt 移除文档：uid 与 path 二选一。
func (s *Service) DeleteDocAt(project, uid, docPath string) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	uid, err = s.resolveDocUID(a, uid, docPath)
	if err != nil {
		return err
	}
	if err := a.DeleteDoc(uid); err != nil {
		return withNotFound(err)
	}
	return nil
}

// docBrief 把集合层的 DocEntry 投影成 MCP 返回结构（正文只算长度，列表里不带全文）。
func docItem(d *collection.DocEntry) DocItem {
	return DocItem{UID: d.UID, Name: d.Name, Path: d.Path, Icon: d.Icon, Seq: d.Seq, Size: len(d.Content)}
}

// DocHitLine 文档正文里的一处命中（1-based 行号 + 该行片段，200 字符内）。
type DocHitLine struct {
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// maxDocHitLines 每篇文档最多回几处命中行（避免正文长时把上下文打爆）。
const maxDocHitLines = 5

// DocHit 一篇文档的搜索命中：count = 全文命中行数，matches = 前 maxDocHitLines 处。
type DocHit struct {
	UID     string       `json:"uid"`
	Name    string       `json:"name"`
	Path    string       `json:"path"`
	Project string       `json:"project,omitempty"`
	Count   int          `json:"count"`
	Matches []DocHitLine `json:"matches"`
	// Hash 文档文件 hash（includeHash=true 时回填）：可直接回传 update_doc.ifMatch，
	// 省掉「搜到 → 再 get_doc 拿 hash」的一次往返。
	Hash string `json:"hash,omitempty"`
}

// SaveImage 把一张 base64 图片存进项目本地 assets/，返回可直接写进 Markdown 的 URL。
// 只在可写项目上允许（resolveWrite），与其他写工具一致受只读模式/授权链约束。
// ImageResult save_doc_image 的结构化结果。
type ImageResult struct {
	URL     string
	SHA256  string
	Size    int
	Deduped bool
}

func (s *Service) SaveImage(project, dataBase64 string) (ImageResult, error) {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return ImageResult{}, err
	}
	if strings.TrimSpace(dataBase64) == "" {
		return ImageResult{}, errors.New("data 不能为空：传图片的 base64（可带 data:image/...;base64, 前缀）")
	}
	url, info, err := a.SaveDocImageInfo(dataBase64)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{URL: url, SHA256: info.Hash, Size: info.Size, Deduped: info.Deduped}, nil
}

// SearchDocs 在文档正文里逐行搜索（不读请求的 docs 字段，那是接口说明）。
// exact=true 走整词匹配（词边界，不切词）；否则大小写不敏感的子串。
// project 为空时在所有已加载项目里搜；返回按命中行数降序、名称升序排列。
// includeHash=true 时给每条命中回填文件 hash（按项目批量读一次，不逐篇读盘）。
func (s *Service) SearchDocs(project, pattern string, exact bool, limit int, includeHash bool) ([]DocHit, int, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, 0, errEmptyQuery
	}
	limit = clampLimit(limit)
	targets, err := s.searchTargets(project)
	if err != nil {
		return nil, 0, err
	}
	var hits []DocHit
	matchedDocs := 0
	for key, a := range targets {
		docs, derr := a.ListDocs()
		if derr != nil {
			continue // 单个项目失败不影响整体
		}
		var hashes map[string]string
		if includeHash {
			hashes, _ = a.DocHashes() // 取不到就留空，不影响搜索结果
		}
		for _, d := range docs {
			var lines []DocHitLine
			count := 0
			for i, line := range strings.Split(d.Content, "\n") {
				if !docLineHit(line, pattern, exact) {
					continue
				}
				count++
				if len(lines) < maxDocHitLines {
					lines = append(lines, DocHitLine{Line: i + 1, Snippet: clipRunes(strings.TrimSpace(line), 200)})
				}
			}
			if count == 0 {
				continue
			}
			matchedDocs++
			hits = append(hits, DocHit{
				UID: d.UID, Name: d.Name, Path: d.Path, Project: key, Count: count, Matches: lines,
				Hash: hashes[d.UID],
			})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Count != hits[j].Count {
			return hits[i].Count > hits[j].Count
		}
		return hits[i].Name < hits[j].Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, matchedDocs, nil
}

// docLineHit 单行匹配：exact=true 要求整词（左右词边界），否则大小写不敏感子串。
func docLineHit(line, pattern string, exact bool) bool {
	if exact {
		return wholeWordHit(line, pattern)
	}
	return strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
}

// docWriteErr 映射文档写盘错误：ifMatch 冲突 → 明确「未写入任何内容」并附当前 hash。
func (s *Service) docWriteErr(a ProjectApp, uid string, err error) error {
	if strings.Contains(err.Error(), "[conflict]") {
		return conflictError("文档", uid, a.DocFileHash(uid))
	}
	return err
}
