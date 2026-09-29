// testfixtures 是端到端测试用的「受控故障矩阵」HTTP 服务：把客户端需要处理的各种响应形态
// （状态码 / 重定向 / 超时 / 大响应 / 二进制 / 压缩 / 非 UTF-8 / Cookie / 认证 / multipart）
// 变成确定、可复现的本地端点，避免用例依赖公网或临时脚本。
//
//	go run ./cmd/testfixtures -addr 127.0.0.1:8175
//
// 自签 TLS 端点复用 `cmd/devserver -tls-echo`（见 doc §5 回归要点 1），本服务不做 HTTPS。
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8175", "监听地址")
	flag.Parse()
	log.Printf("testfixtures: http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newMux()))
}
