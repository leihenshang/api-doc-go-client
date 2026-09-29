package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// newMux 注册全部故障端点的路由表。
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", handleEcho)
	mux.HandleFunc("/status/", handleStatus)
	mux.HandleFunc("/redirect/", handleRedirect)
	mux.HandleFunc("/delay", handleDelay)
	mux.HandleFunc("/json/", handleJSON)
	mux.HandleFunc("/binary", handleBinary)
	mux.HandleFunc("/gzip", handleGzip)
	mux.HandleFunc("/charset/latin1", handleLatin1)
	mux.HandleFunc("/text/latin1", handleTextLatin1)
	mux.HandleFunc("/cookies/set", handleCookieSet)
	mux.HandleFunc("/cookies/echo", handleCookieEcho)
	mux.HandleFunc("/auth/echo", handleAuthEcho)
	mux.HandleFunc("/auth/require", handleAuthRequire)
	mux.HandleFunc("/multipart", handleMultipart)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleEcho 回显请求全貌（方法/路径/查询/头/原始体），用于断言请求组装与变量替换。
func handleEcho(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	writeJSON(w, http.StatusOK, map[string]any{
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   r.URL.Query(),
		"headers": headerMap(r),
		"bodyRaw": string(body),
	})
}

func headerMap(r *http.Request) map[string]string {
	out := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

// handleStatus 返回指定状态码（204/304 按规范不带响应体）。
func handleStatus(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/status/"))
	if err != nil || code < 100 || code > 599 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法状态码"})
		return
	}
	if code == http.StatusNoContent || code == http.StatusNotModified {
		w.WriteHeader(code)
		return
	}
	writeJSON(w, code, map[string]any{"status": code, "path": r.URL.Path})
}

// handleRedirect 多跳重定向：/redirect/3 → 3 次 302 后返回 {"hops":3}；
// ?abs=1 首跳用绝对 Location；?loop=1 自环（验证「超过上限」分支）。
func handleRedirect(w http.ResponseWriter, r *http.Request) {
	hops, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/redirect/"))
	if err != nil || hops < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法跳数"})
		return
	}
	q := r.URL.Query()
	if q.Get("loop") == "1" {
		w.Header().Set("Location", fmt.Sprintf("/redirect/%d?loop=1", hops))
		w.WriteHeader(http.StatusFound)
		return
	}
	if hops == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"hops": 0, "path": r.URL.Path})
		return
	}
	next := fmt.Sprintf("/redirect/%d", hops-1)
	if q.Get("abs") == "1" {
		next = "http://" + r.Host + next
	}
	w.Header().Set("Location", next)
	w.WriteHeader(http.StatusFound)
}

// handleDelay 延迟响应，用于超时（timeoutSec）分支。
func handleDelay(w http.ResponseWriter, r *http.Request) {
	ms, err := strconv.Atoi(r.URL.Query().Get("ms"))
	if err != nil || ms < 0 || ms > 60_000 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法 ms"})
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]any{"delayedMs": ms})
}

// handleJSON 固定形状的 JSON：flat / nested / withArray / fewer / big。
// fewer 与 withArray 配合「响应字段只追加不删除」的回归用例。
func handleJSON(w http.ResponseWriter, r *http.Request) {
	shape := strings.TrimPrefix(r.URL.Path, "/json/")
	switch shape {
	case "flat":
		writeJSON(w, http.StatusOK, map[string]any{"a": 1, "b": "x", "c": true})
	case "nested":
		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{"id": 1, "name": "ada", "profile": map[string]any{"age": 30}},
			"ok":   true,
		})
	case "withArray":
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{{"id": 1, "name": "a"}, {"id": 2, "name": "b"}},
			"total": 2,
		})
	case "fewer":
		writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": 1}}})
	case "big":
		rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
		if rows <= 0 {
			rows = 500
		}
		list := make([]map[string]any, 0, rows)
		for i := 0; i < rows; i++ {
			list = append(list, map[string]any{"id": i, "name": fmt.Sprintf("row-%d", i), "active": i%2 == 0})
		}
		writeJSON(w, http.StatusOK, map[string]any{"rows": list})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知形状: " + shape})
	}
}

// handleBinary 随机二进制（默认 4KB），客户端应判定为二进制并按 base64 展示。
func handleBinary(w http.ResponseWriter, r *http.Request) {
	kb, err := strconv.Atoi(r.URL.Query().Get("kb"))
	if err != nil || kb <= 0 {
		kb = 4
	}
	buf := make([]byte, kb*1024)
	_, _ = rand.Read(buf)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf)
}

// handleGzip 返回 gzip 压缩的 JSON：Go 的 http client 会自动解压，客户端应拿到明文 JSON。
func handleGzip(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_ = json.NewEncoder(zw).Encode(map[string]any{"gzipped": true, "n": 3})
	_ = zw.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// handleLatin1 非 UTF-8 字节 + 中性 Content-Type：客户端应落到「按 UTF-8 合法性判断」分支
// → 判定为二进制（base64 + 二进制提示）。
func handleLatin1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-legacy-bytes")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{0xE4, 0xF6, 0xFC, 0xDF, 0xC4, 0xD6})
}

// handleTextLatin1 同样的字节但声明 text/*：客户端应信任 Content-Type，按文本展示、不判二进制。
func handleTextLatin1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{0xE4, 0xF6, 0xFC, 0xDF, 0xC4, 0xD6})
}

func handleCookieSet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, value := q.Get("name"), q.Get("value")
	if name == "" {
		name, value = "sid", "fixture-cookie"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
	writeJSON(w, http.StatusOK, map[string]any{"set": name})
}

func handleCookieEcho(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"cookie": r.Header.Get("Cookie")})
}

func handleAuthEcho(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authorization": r.Header.Get("Authorization")})
}

// handleAuthRequire 无 Authorization 返回 401，有则 200（验证认证注入）。
func handleAuthRequire(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing authorization"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorization": r.Header.Get("Authorization")})
}

// handleMultipart 回显 multipart 表单字段（验证 multipart 组装）。
func handleMultipart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	fields := map[string]string{}
	for k, v := range r.MultipartForm.Value {
		fields[k] = strings.Join(v, ",")
	}
	files := map[string]int{}
	for k, fhs := range r.MultipartForm.File {
		for _, fh := range fhs {
			files[k] += int(fh.Size)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields, "files": files, "contentType": contentTypeOf(r)})
}

func contentTypeOf(r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return ct
}
