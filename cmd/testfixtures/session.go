package main

import (
	"mime"
	"net/http"
	"sort"
	"strings"
)

// multipartMemory 解析 multipart 的内存上限（夹具只回显字段，不需要落盘）。
const multipartMemory = 32 << 20

// registerSessionRoutes 注册与「会话状态」有关的端点：Cookie / 认证 / multipart。
func registerSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/cookies/set", cookiesSetHandler)
	mux.HandleFunc("/cookies/echo", cookiesEchoHandler)
	mux.HandleFunc("/auth/echo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"auth": r.Header.Get("Authorization")})
	})
	mux.HandleFunc("/auth/require", authRequireHandler)
	mux.HandleFunc("/multipart", multipartHandler)
}

// cookiesSetHandler 下发一个 host-only Cookie（不带 Domain，Path=/），
// 供用例验证「响应写入的 Cookie 会被后续请求带上」。
func cookiesSetHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	value := r.URL.Query().Get("value")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 name"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
	writeJSON(w, http.StatusOK, map[string]any{"set": name + "=" + value})
}

// cookiesEchoHandler 以文本回显请求带的 Cookie（用例断言正文里出现 name=value）。
func cookiesEchoHandler(w http.ResponseWriter, r *http.Request) {
	pairs := []string{}
	for _, c := range r.Cookies() {
		pairs = append(pairs, c.Name+"="+c.Value)
	}
	sort.Strings(pairs)
	if len(pairs) == 0 {
		writeText(w, http.StatusOK, "(no cookies)")
		return
	}
	writeText(w, http.StatusOK, strings.Join(pairs, "\n"))
}

// authRequireHandler 没有 Authorization 就 401；有则把收到的头回显出来
// （用例断言界面里能看到 "Basic "）。
func authRequireHandler(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="testfixtures"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auth": auth, "ok": true})
}

// multipartHandler 回显 multipart 表单：字段、文件名与**去参数的媒体类型**
// （用例断言 contentType 恰好是 multipart/form-data，证明客户端带上了 boundary）。
func multipartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "请用 POST"})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "需要 multipart/form-data", "contentType": mediaType,
		})
		return
	}
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	fields := map[string]string{}
	for k, v := range r.MultipartForm.Value {
		if len(v) > 0 {
			fields[k] = v[0]
		}
	}
	files := map[string]string{}
	for k, v := range r.MultipartForm.File {
		if len(v) > 0 {
			files[k] = v[0].Filename
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":      r.Method,
		"contentType": mediaType,
		"fields":      fields,
		"files":       files,
	})
}
