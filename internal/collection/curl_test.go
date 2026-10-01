package collection

import (
	"strings"
	"testing"
)

func mustParseCurl(t *testing.T, text string) *Request {
	t.Helper()
	r, err := ParseCurl(text)
	if err != nil {
		t.Fatalf("ParseCurl(%q): %v", text, err)
	}
	return r
}

func TestParseCurlPlainGet(t *testing.T) {
	r := mustParseCurl(t, `curl 'https://api.example.com/api/user/list?page=1&size=20'`)
	if r.Method != "GET" || r.URL != "https://api.example.com/api/user/list" {
		t.Fatalf("method/url 解析错误: %s %s", r.Method, r.URL)
	}
	if len(r.Params) != 3 { // page / size + 尾部空行
		t.Fatalf("query 应拆进参数表: %+v", r.Params)
	}
	if r.Params[0].Name != "page" || r.Params[0].Value != "1" || !r.Params[0].Enabled {
		t.Fatalf("第一个参数解析错误: %+v", r.Params[0])
	}
	if r.Params[2].Name != "" {
		t.Fatalf("参数表末尾应留一行空行: %+v", r.Params[2])
	}
	if r.Body.Type != "none" {
		t.Fatalf("无 -d 时请求体应为 none: %+v", r.Body)
	}
	if r.Name != "list" {
		t.Fatalf("默认请求名应取 URL 末段: %q", r.Name)
	}
}

func TestParseCurlChromeBashCopy(t *testing.T) {
	// Chrome「Copy as cURL (bash)」的典型形态：续行 + 多 -H + --data-raw
	text := `curl 'https://api.example.com/v1/login' \
  -H 'accept: application/json' \
  -H 'content-type: application/json' \
  -H 'authorization: Bearer tok-123' \
  --data-raw '{"user":"alice","pwd":"p@ss"}' \
  --compressed`
	r := mustParseCurl(t, text)
	if r.Method != "POST" {
		t.Fatalf("带请求体默认应是 POST: %s", r.Method)
	}
	if len(r.Headers) != 3 || r.Headers[2].Name != "authorization" {
		t.Fatalf("请求头解析错误: %+v", r.Headers)
	}
	if r.Body.Type != "json" || r.Body.Raw != `{"user":"alice","pwd":"p@ss"}` {
		t.Fatalf("JSON 请求体解析错误: %+v", r.Body)
	}
}

func TestParseCurlAnsiCQuotesAndMethod(t *testing.T) {
	// Linux 上 Chrome 会输出 $'…' 与转义换行
	text := `curl -X PUT $'https://api.example.com/v1/items/9' -H $'x-trace: a\tb' -d $'{"note":"l1\nl2"}'`
	r := mustParseCurl(t, text)
	if r.Method != "PUT" || r.URL != "https://api.example.com/v1/items/9" {
		t.Fatalf("method/url 解析错误: %s %s", r.Method, r.URL)
	}
	if r.Headers[0].Value != "a\tb" {
		t.Fatalf("$'…' 转义未生效: %q", r.Headers[0].Value)
	}
	if r.Body.Raw != "{\"note\":\"l1\nl2\"}" {
		t.Fatalf("$'…' 里的 \\n 应还原: %q", r.Body.Raw)
	}
}

func TestParseCurlCmdQuotesAndCaret(t *testing.T) {
	// Windows cmd 的「Copy as cURL (cmd)」：双引号 + \" 转义 + ^ 续行
	text := "curl \"https://api.example.com/v1/echo\" ^\r\n" +
		"  -H \"accept: */*\" ^\r\n" +
		"  --data-raw \"{\\\"a\\\":1}\""
	r := mustParseCurl(t, text)
	if r.URL != "https://api.example.com/v1/echo" || r.Body.Raw != `{"a":1}` {
		t.Fatalf("cmd 形态解析错误: %q %q", r.URL, r.Body.Raw)
	}
	if r.Headers[0].Value != "*/*" {
		t.Fatalf("cmd 请求头解析错误: %+v", r.Headers)
	}
}

func TestParseCurlFormBodyAndShortCluster(t *testing.T) {
	r := mustParseCurl(t, `curl -sSL -XPOST https://api.example.com/oauth/token -d 'grant_type=password&username=a%20b'`)
	if r.Method != "POST" {
		t.Fatalf("-XPOST 组合短选项未识别: %s", r.Method)
	}
	if r.Body.Type != "form" || len(r.Body.Form) != 3 {
		t.Fatalf("urlencoded 请求体应进表单: %+v", r.Body)
	}
	if r.Body.Form[1].Value != "a b" {
		t.Fatalf("表单值应解码百分号: %+v", r.Body.Form[1])
	}
}

func TestParseCurlMultipart(t *testing.T) {
	r := mustParseCurl(t, `curl -F 'name=alice' -F 'avatar=@/tmp/a.png' -F 'note=x;type=text/plain' https://api.example.com/u`)
	if r.Body.Type != "multipart" || len(r.Body.Form) != 4 {
		t.Fatalf("multipart 解析错误: %+v", r.Body)
	}
	if r.Body.Form[1].Type != "file" || r.Body.Form[1].Value != "/tmp/a.png" {
		t.Fatalf("@文件应映射成 file 字段: %+v", r.Body.Form[1])
	}
	if r.Body.Form[2].Value != "x" {
		t.Fatalf(";type= 提示应被去掉: %+v", r.Body.Form[2])
	}
}

func TestParseCurlAuthCookieAndGet(t *testing.T) {
	r := mustParseCurl(t, `curl -G -u alice:secret -b 'sid=1; t=2' -A myUA -e https://ref.example.com --url https://api.example.com/search -d 'q=hello world'`)
	if r.Method != "GET" {
		t.Fatalf("-G 时方法应为 GET: %s", r.Method)
	}
	if r.Auth == nil || r.Auth.Type != "basic" || r.Auth.Username != "alice" || r.Auth.Password != "secret" {
		t.Fatalf("-u 应转成 basic 认证: %+v", r.Auth)
	}
	if r.Body.Type != "none" {
		t.Fatalf("-G 时 -d 应进 query 而不是 body: %+v", r.Body)
	}
	if r.Params[0].Name != "q" || r.Params[0].Value != "hello world" {
		t.Fatalf("-G 的 -d 应进参数表: %+v", r.Params)
	}
	got := map[string]string{}
	for _, h := range r.Headers {
		got[h.Name] = h.Value
	}
	if got["Cookie"] != "sid=1; t=2" || got["User-Agent"] != "myUA" || got["Referer"] != "https://ref.example.com" {
		t.Fatalf("cookie/UA/referer 未转成请求头: %+v", r.Headers)
	}
}

func TestParseCurlIgnoresUnrelatedOptions(t *testing.T) {
	r := mustParseCurl(t, `curl -k --max-time 5 -o out.json --cacert /tmp/ca.pem -H 'X-A: 1' https://api.example.com/ping`)
	if r.URL != "https://api.example.com/ping" {
		t.Fatalf("无关选项的取值被当成 URL 了: %q", r.URL)
	}
	if len(r.Headers) != 1 {
		t.Fatalf("请求头数量异常: %+v", r.Headers)
	}
}

func TestParseCurlErrors(t *testing.T) {
	cases := []struct{ name, text string }{
		{"空命令", "   "},
		{"非 curl", "wget https://api.example.com"},
		{"缺 URL", "curl -H 'X-A: 1'"},
		{"单引号未闭合", "curl 'https://api.example.com"},
		{"-d @文件", "curl https://api.example.com -d @body.json"},
		{"-F 从文件读值", "curl https://api.example.com -F 'a=<file'"},
	}
	for _, tc := range cases {
		if _, err := ParseCurl(tc.text); err == nil {
			t.Fatalf("%s：应当报错", tc.name)
		}
	}
}

func TestParseCurlTemplateURLAndFragment(t *testing.T) {
	r := mustParseCurl(t, `curl '{{host}}/users#frag'`)
	if r.URL != "{{host}}/users" {
		t.Fatalf("fragment 应被丢弃: %q", r.URL)
	}
	if r.Name != "users" {
		t.Fatalf("模板 URL 的末段名解析错误: %q", r.Name)
	}
}

func TestCreateRequestFromDraftWritesContentAndFreshMeta(t *testing.T) {
	c := openTemp(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	draft := &Request{
		UID: "draft-1", Path: "不该被采用.yml", // 草稿里的临时元信息必须被丢弃
		Method:  "post",
		URL:     "https://api.example.com/v1/user",
		Params:  []KV{{Name: "page", Value: "2", Enabled: true}},
		Headers: []KV{{Name: "X-A", Value: "1", Enabled: true}},
		Body:    Body{Type: "json", Raw: `{"name":"alice"}`},
		Docs:    "备注",
	}
	got, err := c.CreateRequestFromDraft("用户", "创建用户", draft)
	if err != nil {
		t.Fatalf("CreateRequestFromDraft: %v", err)
	}
	if got.UID == "" || got.UID == "draft-1" {
		t.Fatalf("uid 应由集合层重新分配: %q", got.UID)
	}
	if !strings.HasPrefix(got.Path, "用户/") || got.Path == draft.Path {
		t.Fatalf("path 应由集合层重新分配: %q", got.Path)
	}
	if got.Method != "POST" || got.Body.Raw != `{"name":"alice"}` || got.Docs != "备注" {
		t.Fatalf("草稿内容未落盘: %+v", got)
	}
	again, err := c.ReadRequest(got.UID)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if again.URL != draft.URL || len(again.Headers) != 1 || again.Params[0].Value != "2" {
		t.Fatalf("重新读盘内容不一致: %+v", again)
	}
}

func TestCreateRequestKeepsURLPlaceholderOutOfTemplate(t *testing.T) {
	c := openTemp(t)
	r, err := c.CreateRequest("", "空地址", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if r.URL != "" {
		t.Fatalf("新建请求不应预填 URL 占位: %q", r.URL)
	}
}
