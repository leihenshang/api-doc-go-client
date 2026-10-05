package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// maxQueryInt 查询参数里整数的上限：防止 ?rows=1e9 之类的请求把夹具自己打爆。
const maxQueryInt = 100_000

// queryInt 读整数查询参数（缺失/非法/负数时返回 def，超过 maxQueryInt 时夹住）。
func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	if n > maxQueryInt {
		return maxQueryInt
	}
	return n
}

// writeJSON 统一 JSON 响应：客户端按 Content-Type 判断「是否按 JSON 渲染」。
func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// writeText 纯文本响应（Cookie 回显等）。
func writeText(w http.ResponseWriter, code int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(text))
}
