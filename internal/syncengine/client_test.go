package syncengine

import "testing"

// 同步请求会带 `Authorization: Bearer <PAT>`：非回环地址禁止明文 http。
func TestValidateServerURL(t *testing.T) {
	ok := map[string]string{
		"https://api.example.com":        "https://api.example.com",
		"https://api.example.com/":       "https://api.example.com",
		"  https://api.example.com/x/  ": "https://api.example.com/x",
		"http://127.0.0.1:8080":          "http://127.0.0.1:8080",
		"http://localhost:8080":          "http://localhost:8080",
		"http://[::1]:8080":              "http://[::1]:8080",
	}
	for in, want := range ok {
		got, err := ValidateServerURL(in)
		if err != nil {
			t.Errorf("%q 应通过校验: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}

	bad := []string{
		"",
		"ftp://example.com",
		"api.example.com",          // 缺 scheme
		"http://192.168.1.10:8080", // 非回环却用明文
		"http://example.com",
		"https://",
	}
	for _, in := range bad {
		if _, err := ValidateServerURL(in); err == nil {
			t.Errorf("%q 应被拒绝", in)
		}
	}
}
