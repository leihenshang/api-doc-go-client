package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempProto(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	return p
}

func TestImportProtosCopiesIntoCollection(t *testing.T) {
	c := newTestCollection(t)
	src := writeTempProto(t, "greeter.proto", "syntax = \"proto3\";\npackage demo;\n")

	infos, err := c.ImportProtos([]string{src})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(infos) != 1 || infos[0].Rel != "protos/greeter.proto" {
		t.Fatalf("落点异常: %+v", infos)
	}
	got, err := os.ReadFile(filepath.Join(c.Dir, "protos", "greeter.proto"))
	if err != nil {
		t.Fatalf("集合内未落盘: %v", err)
	}
	if !strings.Contains(string(got), "package demo") {
		t.Errorf("落盘内容不一致: %s", got)
	}

	list, err := c.ListProtos()
	if err != nil {
		t.Fatalf("列举失败: %v", err)
	}
	if len(list) != 1 || list[0].Rel != "protos/greeter.proto" || list[0].Size == 0 {
		t.Errorf("列举结果异常: %+v", list)
	}
}

// 同名文件再次导入 = 更新定义（覆盖），不产生 greeter-2.proto。
func TestImportProtosOverwritesSameName(t *testing.T) {
	c := newTestCollection(t)
	if _, err := c.ImportProtos([]string{writeTempProto(t, "greeter.proto", "// v1\n")}); err != nil {
		t.Fatalf("首次导入失败: %v", err)
	}
	infos, err := c.ImportProtos([]string{writeTempProto(t, "greeter.proto", "// v2\n")})
	if err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	if infos[0].Rel != "protos/greeter.proto" {
		t.Errorf("同名应覆盖，实际落点 %q", infos[0].Rel)
	}
	list, _ := c.ListProtos()
	if len(list) != 1 {
		t.Errorf("同名覆盖后应仍只有 1 个定义: %+v", list)
	}
	got, _ := os.ReadFile(filepath.Join(c.Dir, "protos", "greeter.proto"))
	if !strings.Contains(string(got), "v2") {
		t.Errorf("覆盖未生效: %s", got)
	}
}

// 同一次导入里选中多个同名文件：第二个起加序号。
func TestImportProtosSameBatchSameName(t *testing.T) {
	c := newTestCollection(t)
	a := writeTempProto(t, "one/greeter.proto", "// a\n")
	b := writeTempProto(t, "two/greeter.proto", "// b\n")
	infos, err := c.ImportProtos([]string{a, b})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	rels := []string{infos[0].Rel, infos[1].Rel}
	if rels[0] != "protos/greeter.proto" || rels[1] != "protos/greeter-2.proto" {
		t.Errorf("批量同名落点异常: %v", rels)
	}
}

func TestImportProtosRejectsBadInput(t *testing.T) {
	c := newTestCollection(t)
	if _, err := c.ImportProtos(nil); err == nil {
		t.Error("空列表应报错")
	}
	txt := writeTempProto(t, "readme.txt", "hi")
	if _, err := c.ImportProtos([]string{txt}); err == nil {
		t.Error("非 .proto 应报错")
	}
	if _, err := c.ImportProtos([]string{filepath.Join(t.TempDir(), "absent.proto")}); err == nil {
		t.Error("源文件不存在应报错")
	}
}

func TestListProtosEmptyWhenNoDir(t *testing.T) {
	c := newTestCollection(t)
	list, err := c.ListProtos()
	if err != nil {
		t.Fatalf("目录不存在时不应报错: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("应为空列表: %+v", list)
	}
}

func TestRemoveProtoAndPathGuard(t *testing.T) {
	c := newTestCollection(t)
	if err := c.RemoveProto("protos/absent.proto"); err == nil {
		t.Error("不存在的定义应报错")
	}
	for _, bad := range []string{"../outside.proto", "api/ping.yml", "", "/abs/x.proto"} {
		if _, err := c.ProtoPath(bad); err == nil {
			t.Errorf("越界路径 %q 应报错", bad)
		}
	}
	if _, err := c.ImportProtos([]string{writeTempProto(t, "x.proto", "syntax = \"proto3\";\n")}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if err := c.RemoveProto("protos/x.proto"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if list, _ := c.ListProtos(); len(list) != 0 {
		t.Errorf("删除后应为空: %+v", list)
	}
}
