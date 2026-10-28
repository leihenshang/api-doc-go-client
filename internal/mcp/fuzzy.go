package mcp

import (
	"fmt"
	"strings"
	"unicode"
)

// Match 一条模糊匹配结果（带分数与命中字段，便于 AI 判断相关性再决定是否取详情）。
type Match struct {
	UID     string   `json:"uid"`
	Name    string   `json:"name"`
	Method  string   `json:"method,omitempty"`
	URL     string   `json:"url,omitempty"`
	Path    string   `json:"path"`    // 相对集合根（含模块）
	Project string   `json:"project"` // 所属项目（跨项目搜索时用）
	Score   int      `json:"score"`
	Matched []string `json:"matched"` // 命中的字段名，如 ["name","uri"]
	// Snippet 命中的那一行及前后各 1 行（截到 200 字符），AI 不用取详情就能判断是否相关。
	Snippet string `json:"snippet,omitempty"`
	// Hash 磁盘内容哈希（来自索引）：传给 update_request 的 ifMatch 可获得冲突保护。
	Hash string `json:"hash,omitempty"`
}

// 命中方式对应的基础分：全串子串 > 分词累加 > 有序子序列（兜底）。
const (
	scoreSubstring = 100
	scoreToken     = 30
	scoreSubseq    = 8
)

// 字段权重：名称最可信，其次 URI（方法/URL/模块路径），再次 docs/字段表/header/body。
var fieldWeights = map[string]int{
	"name":       10,
	"uri":        6,
	"docs":       3,
	"respDoc":    2,
	"bodyFields": 2,
	"header":     2,
	"body":       1,
}

// searchFieldNames fields 参数的合法取值（不传 = 全部）。
// name=名称；uri=方法+URL+模块路径；docs=接口说明；respDoc=响应字段/样例；
// bodyFields=请求体字段表；header=请求头；body=请求体原文。
var searchFieldNames = []string{"name", "uri", "docs", "respDoc", "bodyFields", "header", "body"}

// SearchOptions 一次搜索的口径（search_requests / 内部调用共用）。
type SearchOptions struct {
	// Exact true = 只认「原样整串出现」：不切词、不做有序子序列兜底。
	// 长标识符（如 ministryreportingunits）用它可以消掉大量分词误报。
	Exact bool
	// Fields 非空 = 只在这些字段里搜（取值见 searchFieldNames）。
	Fields map[string]bool
}

// NormalizeSearchFields 校验 fields：未知取值直接报错（不静默忽略，避免 AI 以为筛选生效了）。
func NormalizeSearchFields(fields []string) (map[string]bool, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	out := make(map[string]bool, len(fields))
	for _, raw := range fields {
		f := strings.ToLower(strings.TrimSpace(raw))
		if f == "" {
			continue
		}
		ok := false
		for _, name := range searchFieldNames {
			if f == name {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("fields 含未知取值 %q；可选值：%s", raw, strings.Join(searchFieldNames, "/"))
		}
		out[f] = true
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// fieldOn 字段是否参与本次搜索（未限定 = 全部参与）。
func fieldOn(sel map[string]bool, name string) bool { return len(sel) == 0 || sel[name] }

// candidate 参与打分的候选（由调用方从 index.Node / Request 组装）。
type candidate struct {
	UID        string
	Name       string
	Method     string
	URL        string
	Path       string
	Header     string // header 名与值拼一起
	Docs       string
	RespDoc    string // 响应字段表 + 样例（拼成可搜索文本）
	BodyFields string // 请求体字段表（字段名/说明/示例值）
	Body       string // body 摘要（截断后）
	hash       string // 磁盘内容哈希（仅从索引来时填充）
}

// scoreCandidate 给单个候选打分：返回分数、命中字段、命中片段（行±1）。
// query 为空时返回 0（调用方按默认顺序列出）。
func scoreCandidate(c candidate, query string, opts SearchOptions) (int, []string, string) {
	q := normalize(query)
	if q == "" {
		return 0, nil, ""
	}
	// 切词必须基于「原始查询」：normalize 会把空格/分隔符都去掉，先切词就拿不到词边界了
	tokens := tokenize(query)
	fields := []struct {
		name string
		text string
	}{
		{"name", c.Name},
		{"uri", strings.TrimSpace(c.Method + " " + c.URL + " " + c.Path)},
		{"docs", c.Docs},
		{"respDoc", c.RespDoc},
		{"bodyFields", c.BodyFields},
		{"header", c.Header},
		{"body", c.Body},
	}
	total := 0
	var matched []string
	bestField, bestText := "", ""
	for _, f := range fields {
		if !fieldOn(opts.Fields, f.name) {
			continue
		}
		text := normalize(f.text)
		if text == "" {
			continue
		}
		s := 0
		if opts.Exact {
			// 整词匹配：按原文（区分大小写不敏感）出现，且左右是词边界。
			// 不走分词/子序列，长标识符（ministryreportingunits）不会再被拆碎误命中。
			if wholeWordHit(f.text, query) {
				s = scoreSubstring
			}
		} else if strings.Contains(text, q) {
			s = scoreSubstring
		} else {
			switch {
			case tokenHit(text, tokens) > 0:
				s = scoreToken
			case isSubsequence(q, text):
				s = scoreSubseq
			}
		}
		if s == 0 {
			continue
		}
		total += s * fieldWeights[f.name]
		matched = append(matched, f.name)
		if bestText == "" || fieldWeights[f.name] > fieldWeights[bestField] {
			bestField, bestText = f.name, f.text
		}
	}
	snippet := ""
	if bestText != "" {
		snippet = snippetAround(bestText, query)
	}
	return total, matched, snippet
}

// snippetAround 取命中位置所在行及前后各 1 行，整体截到 200 字符。
// 找不到原样位置（全角/分隔符归一化差异）时退回首个非空行。
func snippetAround(text, query string) string {
	q := strings.TrimSpace(query)
	lower := strings.ToLower(text)
	idx := -1
	if q != "" {
		idx = strings.Index(lower, strings.ToLower(q))
	}
	if idx < 0 {
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) != "" {
				return clipRunes(strings.TrimSpace(line), 200)
			}
		}
		return ""
	}
	lines := strings.Split(text, "\n")
	pos, hit := 0, 0
	for i, line := range lines {
		next := pos + len(line) + 1
		if idx < next || i == len(lines)-1 {
			hit = i
			break
		}
		pos = next
	}
	from, to := hit-1, hit+1
	if from < 0 {
		from = 0
	}
	if to >= len(lines) {
		to = len(lines) - 1
	}
	parts := make([]string, 0, to-from+1)
	for _, l := range lines[from : to+1] {
		parts = append(parts, strings.TrimRight(l, " \t"))
	}
	return clipRunes(strings.TrimSpace(strings.Join(parts, "\n")), 200)
}

// clipRunes 按字符截断（不切坏多字节），被截断时补省略号。
func clipRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// wholeWordHit query 是否在 text 里以「整词」出现（大小写不敏感）：
// 每次出现都要求左右是词边界。词字符只算 ASCII 字母/数字/下划线 ——
// 这样中文/标点/斜杠都算边界，中文查询与 URL 片段都能正常命中。
func wholeWordHit(text, query string) bool {
	lt, lq := strings.ToLower(text), strings.ToLower(query)
	if lq == "" {
		return false
	}
	for from := 0; from <= len(lt)-len(lq); {
		idx := strings.Index(lt[from:], lq)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(lq)
		leftOK := start == 0 || !isWordByte(lt[start-1])
		rightOK := end >= len(lt) || !isWordByte(lt[end])
		if leftOK && rightOK {
			return true
		}
		from = start + 1
	}
	return false
}

// isWordByte 词字符：ASCII 字母 / 数字 / 下划线（多字节字符的高位字节不算）。
func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// tokenHit 统计查询词里有多少个出现在文本中（全部命中给满分，部分命中按比例）。
// 例：query "user list" 能命中 name=user + url=/v1/list 这种跨字段组合。
func tokenHit(text string, tokens []string) int {
	if len(tokens) == 0 {
		return 0
	}
	hits := 0
	for _, tk := range tokens {
		if strings.Contains(text, tk) {
			hits++
		}
	}
	return hits * scoreToken / len(tokens)
}

// tokenize 把原始查询按空白/常见分隔符切词，逐词归一化（全角→半角、小写）。
func tokenize(raw string) []string {
	parts := strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("/._-?&=", r)
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if n := normalize(p); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// isSubsequence 判断 q 的字符是否按序出现在 text 中（拼错/漏字也能捞回来，低分兜底）。
// 按 rune 比较：查询串可能含中文等多字节字符，逐字节比较会失配。
func isSubsequence(q, text string) bool {
	if q == "" {
		return false
	}
	qr := []rune(q)
	i := 0
	for _, r := range text {
		if r == qr[i] {
			i++
			if i == len(qr) {
				return true
			}
		}
	}
	return false
}

// normalize 归一化：全角转半角、小写折叠、去空白与常见分隔符。
// 中文按普通子串处理（不引分词/拼音依赖，AI 调用场景够用）。
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '　' { // 全角空格
			continue
		}
		if r >= 0xFF01 && r <= 0xFF5E { // 全角 ASCII → 半角
			r = r - 0xFEE0
		}
		switch {
		case unicode.IsSpace(r):
			continue
		case strings.ContainsRune("/._-?&=", r):
			r = ' ' // 分隔符统一成空格，避免 "user/list" 与 "userlist" 互相不命中
		}
		b.WriteRune(unicode.ToLower(r))
	}
	// 分隔符转成的空格再压掉
	return strings.ReplaceAll(b.String(), " ", "")
}
