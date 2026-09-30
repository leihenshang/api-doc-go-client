package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 设计文档 §3.3 的 Bruno 超集样例：读入 → 保存必须逐字段 round-trip，
// 且未知顶层字段（vars/script/assert）不能丢。
const brunoSample = `info:
  name: 用户-列表
  type: http
  seq: 1
meta:
  uid: 0f5c2d7a-9b31-4c8e-a2f1-7d0e5b6a1c33
  base_rev: 4211
http:
  method: GET
  url: "{{host}}/api/user/list"
  params:
    - { name: name, value: "{{userName}}", type: query }
  headers:
    - { name: Authorization, value: "Bearer {{token}}" }
  auth: inherit
settings: { encodeUrl: true, timeout: 0, followRedirects: true, maxRedirects: 5 }
vars:
  pre-request:
    - { name: traceId, value: "{{$uuid}}", enabled: true }
script:
  pre-request: bru.setVar('ts', Date.now())
assert:
  - { name: 状态码为 200, expr: "res.status === 200" }
docs: |-
  ## 简要描述
  - 用户查询接口
`

func readSample(t *testing.T) (*requestFile, *Request) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "user-list.yml")
	if err := os.WriteFile(path, []byte(brunoSample), 0o644); err != nil {
		t.Fatalf("写入样例: %v", err)
	}
	f, err := readRequestFile(path)
	if err != nil {
		t.Fatalf("读取样例: %v", err)
	}
	return f, fromFile("user-list.yml", f)
}

func TestFromFileParsesAuthAndSettings(t *testing.T) {
	_, r := readSample(t)
	if r.Auth == nil || r.Auth.Type != "inherit" {
		t.Fatalf("auth 未解析: %+v", r.Auth)
	}
	if r.Settings == nil || !r.Settings.EncodeURL || r.Settings.MaxRedirects != 5 || r.Settings.FollowRedirects == nil {
		t.Fatalf("settings 未解析: %+v", r.Settings)
	}
	if r.Method != "GET" || r.UID != "0f5c2d7a-9b31-4c8e-a2f1-7d0e5b6a1c33" || r.BaseRev != 4211 {
		t.Fatalf("基础字段异常: %+v", r)
	}
	if len(r.Params) != 1 || r.Params[0].Value != "{{userName}}" {
		t.Fatalf("params 异常: %+v", r.Params)
	}
}

func TestMarshalKeepsUnknownFields(t *testing.T) {
	f, r := readSample(t)
	out := r.toFile()
	out.MergeExtra(f.Extra)
	for _, key := range []string{"vars", "script", "assert"} {
		if _, ok := out.Extra[key]; !ok {
			t.Fatalf("未知顶层字段 %q 丢失", key)
		}
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	text := string(data)
	for _, want := range []string{"auth: inherit", "encodeUrl: true", "traceId", "bru.setVar", "状态码为 200", "用户查询接口"} {
		if !strings.Contains(text, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, text)
		}
	}
}

// 保存路径不应丢失磁盘上已有的未知字段（前端不了解这些字段）；
// vars/script/assert 已升为一等字段，经 Request 往返，不走 Extra 合并。
func TestSaveRequestMergesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	c := &Collection{Dir: dir}
	path := filepath.Join(dir, "req.yml")
	if err := os.WriteFile(path, []byte(brunoSample), 0o644); err != nil {
		t.Fatalf("写入样例: %v", err)
	}
	// 模拟前端：读入 → 改方法 → 保存
	f, err := readRequestFile(path)
	if err != nil {
		t.Fatalf("读入: %v", err)
	}
	r := fromFile("req.yml", f)
	r.Method = "POST"
	r.URL = "{{host}}/x"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "method: POST") {
		t.Fatalf("新值未写入:\n%s", text)
	}
	if !strings.Contains(text, "traceId") || !strings.Contains(text, "bru.setVar") {
		t.Fatalf("脚本/变量字段被丢弃:\n%s", text)
	}
}

// 清空脚本/断言后保存，磁盘上的字段应被移除（而不是从旧 Extra 合并回来）。
func TestSaveRequestClearsScriptFields(t *testing.T) {
	dir := t.TempDir()
	c := &Collection{Dir: dir}
	path := filepath.Join(dir, "req.yml")
	if err := os.WriteFile(path, []byte(brunoSample), 0o644); err != nil {
		t.Fatalf("写入样例: %v", err)
	}
	f, err := readRequestFile(path)
	if err != nil {
		t.Fatalf("读入: %v", err)
	}
	r := fromFile("req.yml", f)
	if r.Script == nil || len(r.Asserts) == 0 {
		t.Fatalf("样例脚本字段未解析: %+v", r)
	}
	r.VarsPreRequest = nil
	r.Script = nil
	r.Asserts = nil
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, wantGone := range []string{"traceId", "bru.setVar", "状态码为 200"} {
		if strings.Contains(text, wantGone) {
			t.Fatalf("清空后仍残留 %q:\n%s", wantGone, text)
		}
	}
}

// 认证配置写回：只有类型时保持标量，带字段时为映射。
func TestAuthMarshalShape(t *testing.T) {
	data, err := yaml.Marshal(Auth{Type: "inherit"})
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	if strings.TrimSpace(string(data)) != "inherit" {
		t.Fatalf("标量写法丢失: %q", string(data))
	}
	data, err = yaml.Marshal(Auth{Type: "basic", Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	if !strings.Contains(string(data), "username: u") {
		t.Fatalf("映射写法异常: %q", string(data))
	}
}
