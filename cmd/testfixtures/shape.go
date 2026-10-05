package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
)

// binaryBytes 按需生成确定性二进制内容（0x00..0xff 循环，避免每次跑出不同字节）。
func binaryBytes(size int) []byte {
	out := make([]byte, size)
	for i := range out {
		out[i] = byte(i % 256)
	}
	return out
}

// latin1Bytes 是固定的「非法 UTF-8」字节：用于验证客户端「Content-Type 没表态时
// 按 UTF-8 合法性判断二进制」这条分支（声明 text/* 则信任声明、不判二进制）。
var latin1Bytes = []byte{0xff, 0xfe, 0x41, 0xc3, 0x28, 0x42, 0x80}

// registerShapeRoutes 注册「响应形态」端点：二进制 / gzip / 非 UTF-8。
func registerShapeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/binary", func(w http.ResponseWriter, r *http.Request) {
		kb := queryInt(r, "kb", 1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(binaryBytes(kb * 1024))
	})
	mux.HandleFunc("/gzip", gzipHandler)
	// 中性类型（既不在二进制的类型前缀表里、也不在文本的表里）+ 非法 UTF-8 → 判二进制
	mux.HandleFunc("/charset/latin1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-unknown")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(latin1Bytes)
	})
	// 同样字节但声明 text/* → 信任声明，按文本展示
	mux.HandleFunc("/text/latin1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=latin1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(latin1Bytes)
	})
}

// gzipHandler 返回 gzip 压缩过的 JSON（Go 的 http.Transport 会自动解压，
// 用例据此断言「压缩响应也能正常展示」）。
func gzipHandler(w http.ResponseWriter, _ *http.Request) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_ = json.NewEncoder(zw).Encode(map[string]any{"gzipped": true})
	_ = zw.Close()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
