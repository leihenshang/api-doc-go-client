package mcp_test

// 18 个 Tool 的 inputSchema 契约快照：只锁「对外契约面」——属性名、JSON 类型、
// required、enum、数组元素类型；不锁 description 文案（文案改动不构成契约漂移）。
//
// 新增/改名工具或改动入参字段时，必须显式更新快照：
//   go test ./internal/mcp/ -run TestToolSchemaContract -update
// 快照缺失或与反射结果不一致都会让测试变红，禁止静默漂移。

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 与 tools_test.go 一致：白盒式调用元数据访问器
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var updateSnapshots = flag.Bool("update", false, "更新 inputSchema 契约快照")

const schemaSnapshotDir = "testdata/schema"

// contractKeys 只保留契约面字段（递归），description/title 等文案一律剔除。
var contractKeys = []string{"type", "enum", "required", "properties", "items"}

func projectSchema(raw any) any {
	m, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	out := map[string]any{}
	for _, k := range contractKeys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch k {
		case "properties":
			props, ok := v.(map[string]any)
			if !ok {
				continue
			}
			pm := make(map[string]any, len(props))
			names := make([]string, 0, len(props))
			for name := range props {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				pm[name] = projectSchema(props[name])
			}
			out[k] = pm
		case "items":
			out[k] = projectSchema(v)
		default:
			out[k] = v
		}
	}
	return out
}

func listToolSchemas(t *testing.T) map[string]map[string]any {
	t.Helper()
	server := startSelfServer(t, false)
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "schema-check", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]map[string]any, len(res.Tools))
	for _, tool := range res.Tools {
		if tool.InputSchema == nil {
			t.Errorf("工具 %s 没有输入 schema", tool.Name)
			continue
		}
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("工具 %s schema 无法序列化: %v", tool.Name, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("工具 %s schema 无法反序列化: %v", tool.Name, err)
		}
		proj, _ := projectSchema(raw).(map[string]any)
		out[tool.Name] = proj
	}
	return out
}

func canonicalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestToolSchemaContract(t *testing.T) {
	schemas := listToolSchemas(t)

	// 双向覆盖：每个正式 meta 工具都要有 schema；不允许多出 meta 之外的工具。
	if len(schemas) != len(ToolNames()) {
		t.Fatalf("tools/list 返回 %d 个工具，meta 表 %d 个", len(schemas), len(ToolNames()))
	}
	for _, name := range ToolNames() {
		schema, ok := schemas[name]
		if !ok {
			t.Errorf("meta 中的工具 %s 未在 tools/list 出现", name)
			continue
		}
		path := filepath.Join(schemaSnapshotDir, name+".json")
		want, err := os.ReadFile(path)
		if *updateSnapshots {
			if err := os.MkdirAll(schemaSnapshotDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, canonicalJSON(t, schema), 0o644); err != nil {
				t.Fatalf("写快照 %s 失败: %v", path, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("工具 %s 缺少契约快照 %s；新增/改名工具请显式执行：go test ./internal/mcp/ -run TestToolSchemaContract -update",
				name, path)
			continue
		}
		got := canonicalJSON(t, schema)
		if string(got) != string(want) {
			t.Errorf("工具 %s 的 inputSchema 契约漂移：\n--- 快照 %s\n%s\n--- 实际\n%s\n如确为有意变更，用 -update 显式更新快照",
				name, path, want, got)
		}
	}
}
