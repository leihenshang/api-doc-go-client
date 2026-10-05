// testfixtures 是客户端 e2e 用的「受控故障矩阵」HTTP 服务。
//
// 它让用例在不依赖外网的前提下断言真实响应形态：状态码 / 重定向 / 延迟 / 大响应 /
// 二进制 / gzip / 非 UTF-8 / Cookie / 认证 / multipart。只用于测试，不要部署到生产。
//
//	go run ./cmd/testfixtures -addr 127.0.0.1:8188
//
// 端点清单与「哪个用例依赖它」见 frontend/e2e/README.md。
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8188", "监听地址（e2e 传随机端口）")
	flag.Parse()

	mux := http.NewServeMux()
	registerJSONRoutes(mux)
	registerFaultRoutes(mux)
	registerShapeRoutes(mux)
	registerSessionRoutes(mux)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("testfixtures: http://%s", *addr)
	log.Fatal(srv.ListenAndServe())
}
