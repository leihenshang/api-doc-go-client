package proto

import "testing"

// 定义文件名不能越出 import 路径：protocompile 内部是 filepath.Join(importPath, name)，
// 一份 `import "../../../../etc/passwd";` 就能读到集合外的文件。
func TestUnsafeProtoName(t *testing.T) {
	bad := []string{
		"", "..", "../a.proto", "a/../../b.proto", `..\..\b.proto`,
		"/etc/passwd", `\windows\system32\drivers\etc\hosts`,
		"C:/x.proto", "c:secret.proto", "a\x00b.proto",
	}
	for _, name := range bad {
		if !unsafeProtoName(name) {
			t.Errorf("%q 应被判为越界", name)
		}
	}
	ok := []string{"greeter.proto", "sub/dir/msg.proto", "sub\\dir\\msg.proto", "a..b.proto", "google/api/annotations.proto"}
	for _, name := range ok {
		if unsafeProtoName(name) {
			t.Errorf("%q 不应被判为越界", name)
		}
	}
}
