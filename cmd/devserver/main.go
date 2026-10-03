// devserver 是客户端内核的开发验证入口：无 Wails 桌面环境时，
// 用浏览器（打开前端 dist 或 vite dev 代理）驱动同一套 App 方法。
//
//	go run ./cmd/devserver -dir /tmp/my-collection -web frontend/dist
//
// 另提供 /echo 回显端点，用于端到端验证「变量替换 + 请求发送」。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"api-doc-go-client/internal/app"
)

func main() {
	dir := flag.String("dir", "", "集合目录（不存在会初始化）")
	addr := flag.String("addr", "127.0.0.1:8175", "监听地址")
	web := flag.String("web", "frontend/dist", "前端静态目录")
	tlsEcho := flag.String("tls-echo", "", "自签 HTTPS 回显端点监听地址（验证忽略证书开关；留空不启用）")
	flag.Parse()
	if *dir == "" {
		log.Fatal("需要 -dir 指定集合目录")
	}
	if *tlsEcho != "" {
		log.Printf("自签 HTTPS 回显端点: https://%s/echo", startTLSEcho(*tlsEcho))
	}

	core := app.NewApp()
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
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
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
	})
	// App 方法反射桥：POST /api/App/<Method>，body 为参数数组
	mux.HandleFunc("/api/App/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/App/")
		w.Header().Set("Content-Type", "application/json")
		data, err := invoke(core, name, r)
		if err != nil {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	})
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

// invoke 反射调用 App 的导出方法：args 为 JSON 参数数组。
func invoke(core *app.App, name string, r *http.Request) (any, error) {
	if name == "" || strings.HasPrefix(strings.ToLower(name[:1]), strings.ToLower(name[:1])) && !isExported(name) {
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
