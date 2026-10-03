package mcp

import (
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
	Matched []string `json:"matched"` // 命中的字段，如 ["name","url"]
	// Hash 磁盘内容哈希（来自索引）：传给 update_request 的 if_match 可获得冲突保护。
	Hash string `json:"hash,omitempty"`
}

// 命中方式对应的基础分：全串子串 > 分词累加 > 有序子序列（兜底）。
const (
	scoreSubstring = 100
	scoreToken     = 30
	scoreSubseq    = 8
)

// 字段权重：名称最可信，其次 URL、方法、模块路径，再次 header/docs/body。
var fieldWeights = map[string]int{
	"name":   10,
	"url":    6,
	"method": 4,
	"path":   3,
	"header": 2,
	"docs":   2,
	"body":   1,
}

// candidate 参与打分的候选（由调用方从 index.Node / Request 组装）。
type candidate struct {
	UID    string
	Name   string
	Method string
	URL    string
	Path   string
	Header string // header 名与值拼一起
	Docs   string
	Body   string // body 摘要（截断后）
	hash   string // 磁盘内容哈希（仅从索引来时填充）
}

// scoreCandidate 给单个候选打分；query 为空时返回 0（调用方按默认顺序列出）。
func scoreCandidate(c candidate, query string) (int, []string) {
	q := normalize(query)
	if q == "" {
		return 0, nil
	}
	// 切词必须基于「原始查询」：normalize 会把空格/分隔符都去掉，先切词就拿不到词边界了
	tokens := tokenize(query)
	fields := []struct {
		name string
		text string
	}{
		{"name", c.Name},
		{"url", c.URL},
		{"method", c.Method},
		{"path", c.Path},
		{"header", c.Header},
		{"docs", c.Docs},
		{"body", c.Body},
	}
	total := 0
	var matched []string
	for _, f := range fields {
		text := normalize(f.text)
		if text == "" {
			continue
		}
		s := 0
		switch {
		case strings.Contains(text, q):
			s = scoreSubstring
		case tokenHit(text, tokens) > 0:
			s = scoreToken
		case isSubsequence(q, text):
			s = scoreSubseq
		}
		if s == 0 {
			continue
		}
		total += s * fieldWeights[f.name]
		matched = append(matched, f.name)
	}
	return total, matched
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
