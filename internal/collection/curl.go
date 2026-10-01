package collection

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// 本文件把 curl 命令解析成请求草稿（新建请求流程的「导入 cURL」）。
// 目标形态是 Chrome / Firefox / Postman 的「Copy as cURL」：bash 的 '…'、cmd 的 "…"、
// 以及 $'…'（ANSI-C）与 \ / ^ 续行都要能吃；解析结果不落盘，由 CreateRequestFromDraft 保存。

// curlShortWithValue 取值型短选项（curl manpage 中需要跟参数的那些）。
var curlShortWithValue = map[byte]bool{
	'X': true, 'H': true, 'd': true, 'F': true, 'u': true, 'b': true, 'A': true, 'e': true,
	'o': true, 'w': true, 'T': true, 'm': true, 'x': true, 'U': true, 'r': true, 'E': true,
	'D': true, 'K': true, 'C': true, 'c': true, 'y': true, 'Y': true, 'z': true, 'Z': true,
	'Q': true, 'P': true, 'R': true, 'W': true, 'a': true, 'B': true, 't': true,
}

// curlLongWithValue 取值型长选项：本工具不解析语义，只保证它们的取值不会被误当成 URL。
var curlLongWithValue = map[string]bool{
	"--output": true, "--write-out": true, "--connect-timeout": true, "--max-time": true,
	"--retry": true, "--retry-delay": true, "--retry-max-time": true, "--proxy": true,
	"--proxy-user": true, "--cacert": true, "--capath": true, "--cert": true, "--key": true,
	"--pass": true, "--ciphers": true, "--interface": true, "--limit-rate": true,
	"--max-filesize": true, "--range": true, "--resolve": true, "--connect-to": true,
	"--dns-servers": true, "--keepalive-time": true, "--expect100-timeout": true,
	"--happy-eyeballs-timeout-ms": true, "--max-redirs": true, "--netrc-file": true,
	"--proto": true, "--proto-redir": true, "--tls-max": true, "--local-port": true,
	"--speed-limit": true, "--speed-time": true, "--stderr": true, "--cookie-jar": true,
	"--output-dir": true, "--trace": true, "--trace-ascii": true, "--dump-header": true,
}

// curlArgs 我们从命令里收集到的取值。
type curlArgs struct {
	method   string
	urlFlag  string
	position []string
	headers  []KV
	data     []string
	forms    []string
	user     string
	jsonBody bool
	get      bool
	head     bool
}

// ParseCurl 把一段 curl 命令解析成请求草稿。
// 返回的 Request 不带 uid / path（落盘交给 CreateRequestFromDraft），失败时给出可读原因。
// 支持 -X/--request、-H/--header、-d/--data*、-F/--form、-u/--user、-b/--cookie、
// -A/--user-agent、-e/--referer、-G/--get、-I/--head、--url、--json 与常见无关键；
// 不支持的取值写法（-d @文件、-F '字段=<文件'）会明确报错而不是静默丢掉内容。
func ParseCurl(text string) (*Request, error) {
	toks, err := tokenizeCurl(text)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("curl 命令为空")
	}
	if !isCurlCommand(toks[0]) {
		return nil, fmt.Errorf("不是 curl 命令（首词为 %q）", toks[0])
	}
	args, err := scanCurlArgs(toks[1:])
	if err != nil {
		return nil, err
	}
	return buildCurlRequest(args)
}

func isCurlCommand(tok string) bool {
	t := strings.ToLower(strings.TrimSpace(tok))
	return t == "curl" || t == "curl.exe" || strings.HasSuffix(t, "/curl") || strings.HasSuffix(t, `\curl.exe`)
}

// scanCurlArgs 逐词元收集取值：--long[=值]、-x[值]、组合短选项（-sS / -XPOST）与位置参数。
func scanCurlArgs(toks []string) (*curlArgs, error) {
	a := &curlArgs{}
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		switch {
		case tok == "--":
			a.position = append(a.position, toks[i+1:]...)
			return a, nil
		case strings.HasPrefix(tok, "--"):
			used, err := applyCurlLong(a, tok, toks, i)
			if err != nil {
				return nil, err
			}
			i += used
		case len(tok) > 1 && tok[0] == '-':
			used, err := applyCurlShort(a, tok[1:], toks, i)
			if err != nil {
				return nil, err
			}
			i += used
		default:
			a.position = append(a.position, tok)
		}
	}
	return a, nil
}

// applyCurlLong 处理一个 --long 选项；返回额外消费的词元数（用空格分开取值时为 1）。
func applyCurlLong(a *curlArgs, tok string, toks []string, i int) (int, error) {
	name, val, hasVal := strings.Cut(tok, "=")
	take := func() (string, int, error) {
		if hasVal {
			return val, 0, nil
		}
		if i+1 >= len(toks) {
			return "", 0, fmt.Errorf("选项 %s 缺少取值", name)
		}
		return toks[i+1], 1, nil
	}
	switch name {
	case "--request":
		v, n, err := take()
		if err != nil {
			return 0, err
		}
		a.method = v
		return n, nil
	case "--header":
		v, n, err := take()
		if err != nil {
			return 0, err
		}
		addCurlHeader(a, v)
		return n, nil
	case "--data", "--data-raw", "--data-ascii", "--data-binary", "--data-urlencode":
		return appendCurlData(a, name, take)
	case "--json":
		return appendCurlJSON(a, take)
	case "--form", "--form-string":
		v, n, err := take()
		if err != nil {
			return 0, err
		}
		a.forms = append(a.forms, v)
		return n, nil
	case "--user":
		v, n, err := take()
		if err != nil {
			return 0, err
		}
		a.user = v
		return n, nil
	case "--cookie":
		return addCurlHeaderOpt(a, "Cookie: ", take)
	case "--user-agent":
		return addCurlHeaderOpt(a, "User-Agent: ", take)
	case "--referer":
		return addCurlHeaderOpt(a, "Referer: ", take)
	case "--url":
		v, n, err := take()
		if err != nil {
			return 0, err
		}
		a.urlFlag = v
		return n, nil
	case "--get":
		a.get = true
	case "--head":
		a.head = true
	default:
		if !hasVal && curlLongWithValue[name] {
			return 1, nil // 取值与本工具无关，跳过以免被当成 URL
		}
	}
	return 0, nil
}

func appendCurlData(a *curlArgs, name string, take func() (string, int, error)) (int, error) {
	v, n, err := take()
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(v, "@") {
		return 0, fmt.Errorf("%s 不支持「从文件读」（%s）：请直接粘贴内容", name, v)
	}
	a.data = append(a.data, v)
	return n, nil
}

func appendCurlJSON(a *curlArgs, take func() (string, int, error)) (int, error) {
	v, n, err := take()
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(v, "@") {
		return 0, fmt.Errorf("--json 不支持「从文件读」（%s）：请直接粘贴内容", v)
	}
	a.data = append(a.data, v)
	a.jsonBody = true
	return n, nil
}

func addCurlHeaderOpt(a *curlArgs, prefix string, take func() (string, int, error)) (int, error) {
	v, n, err := take()
	if err != nil {
		return 0, err
	}
	addCurlHeader(a, prefix+v)
	return n, nil
}

// applyCurlShort 处理一个短选项簇（-sS / -XPOST / -H'…'）；返回额外消费的词元数。
func applyCurlShort(a *curlArgs, body string, toks []string, i int) (int, error) {
	for p := 0; p < len(body); p++ {
		c := body[p]
		if !curlShortWithValue[c] {
			switch c {
			case 'G':
				a.get = true
			case 'I':
				a.head = true
			}
			continue
		}
		val := body[p+1:]
		used := 0
		if val == "" {
			if i+1 >= len(toks) {
				return 0, fmt.Errorf("选项 -%c 缺少取值", c)
			}
			val, used = toks[i+1], 1
		}
		if err := applyCurlShortValue(a, c, val); err != nil {
			return 0, err
		}
		return used, nil
	}
	return 0, nil
}

func applyCurlShortValue(a *curlArgs, c byte, val string) error {
	switch c {
	case 'X':
		a.method = val
	case 'H':
		addCurlHeader(a, val)
	case 'd':
		if strings.HasPrefix(val, "@") {
			return fmt.Errorf("-d 不支持「从文件读」（%s）：请直接粘贴内容", val)
		}
		a.data = append(a.data, val)
	case 'F':
		a.forms = append(a.forms, val)
	case 'u':
		a.user = val
	case 'b':
		addCurlHeader(a, "Cookie: "+val)
	case 'A':
		addCurlHeader(a, "User-Agent: "+val)
	case 'e':
		addCurlHeader(a, "Referer: "+val)
	}
	return nil
}

// addCurlHeader 解析 `-H 'Name: Value'`；空名或 `Name;`（curl 的「删除该头」写法）直接跳过。
func addCurlHeader(a *curlArgs, line string) {
	name, val, ok := strings.Cut(strings.TrimSpace(line), ":")
	name = strings.TrimSpace(name)
	if !ok || name == "" || strings.TrimSpace(val) == "" {
		return
	}
	a.headers = append(a.headers, KV{Name: name, Value: strings.TrimSpace(val), Enabled: true})
}

// buildCurlRequest 把收集到的取值组装成请求草稿。
func buildCurlRequest(a *curlArgs) (*Request, error) {
	raw := a.urlFlag
	if strings.TrimSpace(raw) == "" {
		raw = pickCurlURL(a.position)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("curl 命令里没找到 URL")
	}
	base, params := splitCurlQuery(raw)
	r := &Request{Method: curlMethod(a), URL: base, Headers: a.headers, Body: Body{Type: "none"}}
	if a.get {
		params = append(params, curlDataPairs(a.data, false)...)
	} else {
		body, err := curlBody(a)
		if err != nil {
			return nil, err
		}
		r.Body = body
	}
	r.Params = append(params, KV{Enabled: true})
	if a.user != "" {
		name, pass, _ := strings.Cut(a.user, ":")
		r.Auth = &Auth{Type: "basic", Username: name, Password: pass}
	}
	r.Name = curlName(base)
	return r, nil
}

// pickCurlURL 从位置参数里挑 URL：优先带 scheme 的词元，否则取第一个（curl 本身也是「第一个位置参数即 URL」）。
func pickCurlURL(position []string) string {
	for _, p := range position {
		if strings.Contains(p, "://") {
			return p
		}
	}
	if len(position) > 0 {
		return position[0]
	}
	return ""
}

func curlMethod(a *curlArgs) string {
	switch {
	case strings.TrimSpace(a.method) != "":
		return strings.ToUpper(strings.TrimSpace(a.method))
	case a.head:
		return "HEAD"
	case a.get:
		return "GET" // -G：把 -d 的内容当 query，方法仍是 GET
	case len(a.data) > 0 || len(a.forms) > 0:
		return "POST" // curl 的默认：带请求体即 POST
	default:
		return "GET"
	}
}

// splitCurlQuery 把 URL 里的 query 拆成参数表（与编辑器的 URL ↔ 参数表联动一致），fragment 丢弃。
func splitCurlQuery(raw string) (string, []KV) {
	base, query, _ := strings.Cut(raw, "?")
	base, _, _ = strings.Cut(base, "#")
	if query == "" {
		return base, nil
	}
	out := []KV{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, val, _ := strings.Cut(pair, "=")
		n, v := name, val
		if dec, err := url.QueryUnescape(name); err == nil {
			n = dec
		}
		if dec, err := url.QueryUnescape(val); err == nil {
			v = dec
		}
		if n == "" {
			continue
		}
		out = append(out, KV{Name: n, Value: v, Enabled: true})
	}
	return base, out
}

// curlBody 依据 -d / -F 组装请求体：JSON 原文、urlencoded 表单、multipart 表单或纯文本。
func curlBody(a *curlArgs) (Body, error) {
	if len(a.forms) > 0 {
		rows, err := curlFormRows(a.forms)
		if err != nil {
			return Body{}, err
		}
		return Body{Type: "multipart", Form: rows}, nil
	}
	if len(a.data) == 0 {
		return Body{Type: "none"}, nil
	}
	data := strings.Join(a.data, "&")
	if a.jsonBody || isCurlJSON(data) {
		return Body{Type: "json", Raw: data}, nil
	}
	if rows, ok := curlFormPairs(data); ok {
		return Body{Type: "form", Form: append(rows, KV{Enabled: true})}, nil
	}
	return Body{Type: "text", Raw: data}, nil
}

// curlDataPairs 把 -d 的内容拆成参数行（-G 时数据走 query）；decode 控制是否再解一次百分号编码。
func curlDataPairs(data []string, decode bool) []KV {
	out := []KV{}
	for _, d := range data {
		for _, pair := range strings.Split(d, "&") {
			if pair == "" {
				continue
			}
			name, val, _ := strings.Cut(pair, "=")
			if !decode {
				out = append(out, KV{Name: name, Value: val, Enabled: true})
				continue
			}
			n, v := name, val
			if dec, err := url.QueryUnescape(name); err == nil {
				n = dec
			}
			if dec, err := url.QueryUnescape(val); err == nil {
				v = dec
			}
			out = append(out, KV{Name: n, Value: v, Enabled: true})
		}
	}
	return out
}

// isCurlJSON 判断请求体是不是 JSON 原文（只看对象/数组，裸标量按普通文本处理）。
func isCurlJSON(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return false
	}
	return json.Valid([]byte(t))
}

// curlFormPairs 判断 `a=1&b=2` 形态的 urlencoded 请求体并拆成表单行。
func curlFormPairs(s string) ([]KV, bool) {
	if s == "" || strings.ContainsAny(s, "\n\r{[]<>") {
		return nil, false
	}
	rows := []KV{}
	for _, pair := range strings.Split(s, "&") {
		name, val, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			return nil, false
		}
		n, err1 := url.QueryUnescape(name)
		v, err2 := url.QueryUnescape(val)
		if err1 != nil || err2 != nil {
			return nil, false
		}
		rows = append(rows, KV{Name: n, Value: v, Enabled: true})
	}
	return rows, len(rows) > 0
}

// curlFormRows 解析 `-F '字段=值'`：`@路径` 是文件 part（值即路径），`;type=` 提示忽略。
func curlFormRows(forms []string) ([]KV, error) {
	rows := make([]KV, 0, len(forms)+1)
	for _, f := range forms {
		name, val, ok := strings.Cut(f, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("无法解析 -F %q（应为 字段=值）", f)
		}
		if i := strings.IndexByte(val, ';'); i >= 0 {
			val = val[:i]
		}
		switch {
		case strings.HasPrefix(val, "@"):
			rows = append(rows, KV{Name: name, Value: val[1:], Enabled: true, Type: "file"})
		case strings.HasPrefix(val, "<"):
			return nil, fmt.Errorf("-F %q 的「从文件读值」暂不支持（请改用 @文件）", f)
		default:
			rows = append(rows, KV{Name: name, Value: val, Enabled: true, Type: "text"})
		}
	}
	return append(rows, KV{Enabled: true, Type: "text"}), nil
}

// curlName 用 URL 末段猜一个默认请求名（保存对话框预填，用户可改）。
func curlName(base string) string {
	u := base
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	i := strings.IndexByte(u, '/')
	if i < 0 {
		return ""
	}
	u = strings.TrimRight(u[i:], "/")
	if j := strings.LastIndexByte(u, '/'); j >= 0 {
		u = u[j+1:]
	}
	return strings.TrimSpace(u)
}
