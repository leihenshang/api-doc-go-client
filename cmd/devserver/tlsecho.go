package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"log"
	"math/big"
	"net"
	"net/http"
	"time"
)

// startTLSEcho 启动一个自签证书的 HTTPS 回显端点，仅用于端到端验证
// 「忽略 SSL 证书校验」开关（严格模式应报证书错误，忽略模式应返回 200）。
func startTLSEcho(addr string) string {
	cert, err := selfSignedCert()
	if err != nil {
		log.Fatalf("生成自签证书: %v", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("监听 TLS 端点: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tls": r.TLS != nil, "host": r.Host, "path": r.URL.Path,
		})
	})
	srv := &http.Server{
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	return ln.Addr().String()
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "api-doc-client-dev"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
