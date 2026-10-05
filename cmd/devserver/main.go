// devserver 是客户端内核的开发验证入口：无 Wails 桌面环境时，
// 用浏览器（打开前端 dist 或 vite dev 代理）驱动同一套 App 方法。
//
//	go run ./cmd/devserver -dir /tmp/my-collection -web frontend/dist
//
// 另提供 /echo 回显端点，用于端到端验证「变量替换 + 请求发送」。
package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"api-doc-go-client/internal/app"
	mcpsrv "api-doc-go-client/internal/mcp"
)

func main() {
	dir := flag.String("dir", "", "集合目录（不存在会初始化）")
	addr := flag.String("addr", "127.0.0.1:8175", "监听地址")
	web := flag.String("web", "frontend/dist", "前端静态目录")
	tlsEcho := flag.String("tls-echo", "", "自签 HTTPS 回显端点监听地址（验证忽略证书开关；留空不启用）")
	// /api/App/* 反射桥等于「遥控整个 App」（读写集合、发请求、改设置）。
	// 默认只监听回环 + 只接受同源 JSON 请求（见 guardAPI）；绑非回环时必须显式给令牌。
	token := flag.String("token", "", "API 访问令牌；绑定非回环地址时必填（前端需带 X-Dev-Token 头）")
	flag.Parse()
	if *dir == "" {
		log.Fatal("需要 -dir 指定集合目录")
	}
	if !loopbackListenAddr(*addr) && strings.TrimSpace(*token) == "" {
		log.Fatalf("监听地址 %s 非回环：必须用 -token 指定访问令牌，否则同网段任意主机都能读写集合", *addr)
	}
	if *tlsEcho != "" {
		log.Printf("自签 HTTPS 回显端点: https://%s/echo", startTLSEcho(*tlsEcho))
	}

	core := app.NewApp()
	// 与桌面端一致：浏览器调试态也注入内嵌 MCP 服务（设置里启用后即可用）
	core.SetMCPBackend(mcpsrv.NewBackend(log.New(os.Stderr, "mcp ", log.LstdFlags)))
	core.SetHeadlessDir(*dir)
	if _, err := core.OpenCollection(*dir); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	// 测试端点：回显请求（验证变量替换与请求组装）
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"query":  r.URL.Query(),
			"header": map[string]string{
				"Authorization": r.Header.Get("Authorization"),
				"Content-Type":  r.Header.Get("Content-Type"),
			},
			"bodyRaw": string(body),
		})
	})
	// 应用事件流（SSE）：浏览器态没有 Wails 事件总线，文件监听等事件经这里推给前端。
	// 每个连接独立订阅，断开即退订。
	mux.HandleFunc("/api/events", guardAPI(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		flusher.Flush()
		events, done := core.SubscribeEvents()
		defer done()
		notify := r.Context().Done()
		for {
			select {
			case ev := <-events:
				data, _ := json.Marshal(map[string]any{"name": "collection:changed", "payload": ev})
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-notify:
				return
			}
		}
	}, *token))
	// App 方法反射桥：POST /api/App/<Method>，body 为参数数组。
	// guardAPI 负责「同源 + JSON + 可选令牌」三重门禁：这是本进程唯一能改数据的入口。
	mux.HandleFunc("/api/App/", guardAPI(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/App/")
		w.Header().Set("Content-Type", "application/json")
		data, err := invoke(core, name, r)
		if err != nil {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	}, *token))
	// 前端静态资源（SPA 回退 index.html）
	webRoot, _ := filepath.Abs(*web)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(webRoot, filepath.Clean("/"+strings.TrimPrefix(r.URL.Path, "/")))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			http.ServeFile(w, r, p)
			return
		}
		http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
	})

	log.Printf("devserver: http://%s   (collection: %s, web: %s)", *addr, *dir, webRoot)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// guardAPI 保护 /api/* 端点。
//
// 为什么必须挡：/api/App/<Method> 是「反射调用任意导出方法」的桥，没有门禁时，
// 任何网页都能用 `fetch('http://127.0.0.1:8175/api/App/...', {mode:'no-cors'})`
// （text/plain 简单请求，不触发预检）触发 OpenCollection / SaveDoc / DeleteRequest 等操作。
// 三重门禁：
//  1. Host 必须是本机（防 DNS rebinding：攻击域解析到 127.0.0.1 时 Host 会是攻击域）；
//  2. 带 Origin 的请求必须是本机来源（浏览器才会带 Origin，跨站请求在这里被挡）；
//  3. 只接受 POST + application/json（非 JSON 的简单请求直接 415，跨站表单/图片打不进来）；
//  4. 指定 -token 时校验 X-Dev-Token（绑非回环地址时由 main 强制要求）。
func guardAPI(next http.HandlerFunc, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "forbidden: host not allowed", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !loopbackOrigin(o) {
			http.Error(w, "forbidden: origin not allowed", http.StatusForbidden)
			return
		}
		if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Dev-Token")), []byte(token)) != 1 {
			http.Error(w, "unauthorized: 缺少 X-Dev-Token", http.StatusUnauthorized)
			return
		}
		// SSE 走 GET；其余（含反射桥）必须是带 JSON body 的 POST
		if r.URL.Path != "/api/events" {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
				http.Error(w, "unsupported media type: 需要 application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next(w, r)
	}
}

// loopbackListenAddr 监听地址是否只绑回环（空地址视为非回环：会绑全网卡）。
func loopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	return loopbackHost(host)
}

// loopbackHost 主机名（可带端口）是否指向本机。
func loopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return true // 空 Host（HTTP/1.0 或本地工具）无法用于 rebinding，放行
	}
	if hp, _, err := net.SplitHostPort(h); err == nil {
		h = hp
	}
	switch strings.ToLower(strings.Trim(h, "[]")) {
	case "localhost":
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// loopbackOrigin Origin 头是否来自本机页面（vite dev server / 本机 dist）。
func loopbackOrigin(origin string) bool {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return loopbackHost(u.Host)
}

// invoke 反射调用 App 的导出方法：args 为 JSON 参数数组。
func invoke(core *app.App, name string, r *http.Request) (any, error) {
	if name == "" || !isExported(name) {
		return nil, errors.New("未知方法")
	}
	m := reflect.ValueOf(core).MethodByName(name)
	if !m.IsValid() {
		return nil, errors.New("未知方法: " + name)
	}
	mt := m.Type()
	var raw []json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if mt.IsVariadic() {
		return nil, errors.New("不支持可变参数方法")
	}
	if len(raw) != mt.NumIn() {
		return nil, errors.New(name + " 参数个数不符: 期望 " + itoa(mt.NumIn()) + " 个，实际 " + itoa(len(raw)) + " 个")
	}
	in := make([]reflect.Value, mt.NumIn())
	for i := range in {
		v := reflect.New(mt.In(i))
		if err := json.Unmarshal(raw[i], v.Interface()); err != nil {
			return nil, errors.New("参数 " + itoa(i+1) + " 类型不符: " + err.Error())
		}
		in[i] = v.Elem()
	}
	out := m.Call(in)
	res := struct {
		OK    bool   `json:"ok"`
		Data  any    `json:"data,omitempty"`
		Error string `json:"error,omitempty"`
	}{OK: true}
	if n := len(out); n > 0 {
		if e, ok := out[n-1].Interface().(error); ok {
			if e != nil {
				res.OK, res.Error = false, e.Error()
			}
			if n > 1 {
				res.Data = out[0].Interface()
			}
		} else {
			res.Data = out[0].Interface()
		}
	}
	if !res.OK {
		return nil, errors.New(res.Error)
	}
	return res.Data, nil
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return 'A' <= c && c <= 'Z'
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
