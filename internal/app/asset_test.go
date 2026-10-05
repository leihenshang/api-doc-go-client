package app

import "testing"

// assetURL 是「文档附件只能从绑定服务端同源下载」这条规则的唯一入口。
// 回归点：曾经任意 http(s) 地址都会带上用户的 Bearer 令牌去下载（凭据外泄 + SSRF）。
func TestAssetURLOnlyAllowsServerOrigin(t *testing.T) {
	const server = "https://doc.example.com"
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"相对路径按服务端补全", "/uploads/a.png", "https://doc.example.com/uploads/a.png", false},
		{"同源绝对地址放行", "https://doc.example.com/uploads/b.png", "https://doc.example.com/uploads/b.png", false},
		{"同源但不同端口视为不同源", "https://doc.example.com:8443/x.png", "", true},
		{"异源必须拒绝", "https://evil.test/steal.png", "", true},
		{"协议不同视为异源", "http://doc.example.com/x.png", "", true},
		{"协议相对地址拒绝", "//evil.test/x.png", "", true},
		{"非 http 协议拒绝", "file:///etc/passwd", "", true},
		{"空地址拒绝", "   ", "", true},
	}
	for _, c := range cases {
		got, err := assetURL(server, c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: 期望报错，实际 %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 不应报错: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// 未配置服务端时，相对路径无从补全：必须报错而不是退化为「原样请求」。
func TestAssetURLRequiresServerForRelativePath(t *testing.T) {
	if _, err := assetURL("", "/uploads/a.png"); err == nil {
		t.Fatal("没有服务端地址时相对路径应报错")
	}
	if _, err := assetURL("not-a-url", "/uploads/a.png"); err == nil {
		t.Fatal("服务端地址非法时相对路径应报错")
	}
}
