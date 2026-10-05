package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxDelayMS 延迟端点的上限：夹具不能被一个 ?ms=999999999 挂住整条用例。
const maxDelayMS = 60_000

// registerFaultRoutes 注册「制造故障」的端点：状态码 / 重定向 / 延迟。
func registerFaultRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/status/", statusHandler)
	mux.HandleFunc("/redirect/", redirectHandler)
	mux.HandleFunc("/delay", delayHandler)
}

// statusHandler 返回指定状态码；204/304 按规范不带响应体。
// 404 会带上 {"status":404}，供用例断言「有状态码也有正文」。
func statusHandler(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/status/"))
	if err != nil || code < 100 || code > 599 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "用法：/status/{100-599}"})
		return
	}
	if code == http.StatusNoContent || code == http.StatusNotModified {
		w.WriteHeader(code)
		return
	}
	writeJSON(w, code, map[string]any{"status": code})
}

// delayHandler 睡 ms 后返回 200（配合 timeoutSec=1 验证超时分支）。
func delayHandler(w http.ResponseWriter, r *http.Request) {
	ms := queryInt(r, "ms", 0)
	if ms > maxDelayMS {
		ms = maxDelayMS
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]any{"delayed": ms})
}

// redirectHandler 处理 /redirect/{n}：n 跳到 n-1，n=0 时返回 {"hops":0}。
//
//	?abs=1   用绝对地址做 Location（验证绝对跳转）
//	?loop=1  永远跳回自己（验证「超过重定向上限」分支，不会收敛）
func redirectHandler(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/redirect/"))
	if err != nil || n < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "用法：/redirect/{n}"})
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"hops": 0})
		return
	}
	target := fmt.Sprintf("/redirect/%d", n-1)
	if r.URL.Query().Get("loop") == "1" {
		target = "/redirect/1" // 自环：每次都是 /redirect/1（不再递减，永不收敛）
	}
	if r.URL.Query().Get("abs") == "1" {
		target = "http://" + r.Host + target
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusFound)
}
