package mcp

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Get User", "getuser"},
		{"ＧＥＴ　Ｕｓｅｒ", "getuser"},    // 全角 + 全角空格
		{"/user/list", "userlist"}, // 分隔符归一
		{"user_list-2", "userlist2"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalize(c.in); got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestScoreCandidate(t *testing.T) {
	cases := []struct {
		name       string
		q          string
		wantScore  bool // 是否期望命中（score>0）
		wantFields []string
	}{
		{"名称子串", "ping", true, []string{"name"}},
		{"URL 子串", "echo", true, []string{"url"}},
		{"方法命中", "get", true, []string{"method"}},
		{"全角查询", "ＰＩＮＧ", true, []string{"name"}},
		{"docs 字段命中", "探活", true, []string{"docs"}},
		{"子序列兜底", "png", true, []string{"name"}},
		{"完全不相关", "zzzz", false, nil},
		{"空查询不命中", "", false, nil},
	}
	base := candidate{
		UID: "u1", Name: "ping", Method: "GET", URL: "{{host}}/echo",
		Path: "api/ping.yml", Header: "Content-Type:application/json", Docs: "探活接口",
	}
	for _, c := range cases {
		score, fields := scoreCandidate(base, c.q)
		if got := score > 0; got != c.wantScore {
			t.Errorf("%s: query=%q score=%d 命中=%v，期望命中=%v", c.name, c.q, score, fields, c.wantScore)
			continue
		}
		if c.wantScore && len(fields) == 0 {
			t.Errorf("%s: query=%q 有分数但没有命中字段", c.name, c.q)
		}
	}
}

func TestTokenScoreAcrossFields(t *testing.T) {
	c := candidate{Name: "user", URL: "/v1/list"}
	if score, _ := scoreCandidate(c, "user list"); score == 0 {
		t.Errorf("跨字段的分词查询应命中：user + list")
	}
	if score, _ := scoreCandidate(c, "zzz"); score != 0 {
		t.Errorf("无关查询不应命中：%d", score)
	}
}

func TestScoreCandidatePrefersName(t *testing.T) {
	a := candidate{Name: "order-list", URL: "/a"}
	b := candidate{Name: "user", URL: "/order/list"}
	sa, _ := scoreCandidate(a, "order")
	sb, _ := scoreCandidate(b, "order")
	if sa <= sb {
		t.Errorf("名称命中的分数应高于 URL 命中：%d vs %d", sa, sb)
	}
}

func TestIsSubsequence(t *testing.T) {
	cases := []struct {
		q, text string
		want    bool
	}{
		{"abc", "axbxc", true},
		{"abc", "acb", false},
		{"", "abc", false},
		{"用户列表", "用户列表演示", true},
	}
	for _, c := range cases {
		if got := isSubsequence(c.q, c.text); got != c.want {
			t.Errorf("isSubsequence(%q,%q)=%v want %v", c.q, c.text, got, c.want)
		}
	}
}
