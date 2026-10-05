package main

import (
	"fmt"
	"io"
	"net/http"
	"sort"
)

// registerJSONRoutes 注册固定形状的 JSON 端点。
//
// withArray / fewer 是一对：「字段只追加不删除」用例先打 withArray 记录叶子字段
// （total、items.0.id、items.0.name、items.1.id、items.1.name），再打 fewer（只有 total）
// 断言字段数不变，所以 fewer 的叶子必须是 withArray 的子集。
func registerJSONRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/json/flat", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"flat": true, "value": 1})
	})
	mux.HandleFunc("/json/nested", func(w http.ResponseWriter, r *http.Request) {
		// probe 回显 query：用例用它确认「参数真的拼到了地址上」。
		writeJSON(w, http.StatusOK, map[string]any{
			"nested": map[string]any{"level1": map[string]any{"value": "deep"}, "list": []any{1, 2}},
			"probe":  r.URL.Query().Get("probe"),
		})
	})
	mux.HandleFunc("/json/withArray", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"total": 2,
			"items": []map[string]any{{"id": 1, "name": "row-0"}, {"id": 2, "name": "row-1"}},
		})
	})
	mux.HandleFunc("/json/fewer", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"total": 1})
	})
	mux.HandleFunc("/json/big", func(w http.ResponseWriter, r *http.Request) {
		n := queryInt(r, "rows", 500)
		writeJSON(w, http.StatusOK, map[string]any{"count": n, "rows": bigRows(n)})
	})
	mux.HandleFunc("/echo", echoHandler)
}

// bigRows 生成「大 JSON」的行数组（用例断言正文里能看到 row-0）。
func bigRows(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{"index": i, "name": fmt.Sprintf("row-%d", i)})
	}
	return out
}

// echoHandler 回显方法 / 路径 / 查询 / 请求头 / 原始体（手工调试与「请求组装」用例用）。
// 头按名字排序输出：Go 的 map 顺序随机，不排序会让断言与截图不稳定。
func echoHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	headers := map[string]string{}
	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		headers[k] = r.Header.Get(k)
	}
	query := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   query,
		"headers": headers,
		"bodyRaw": string(body),
	})
}
