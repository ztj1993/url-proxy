package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	// 模拟镜像网关接收端点 (用于验证 forward 指令重定向)
	mux.HandleFunc("/mirror/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[mock_server] received forwarded request: %s", r.URL.RequestURI())
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Forwarded-Mock", "true")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "unified proxy forwarding test passed: URI=%s\n", r.URL.RequestURI())
	})

	// 健康检查端点
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	addr := ":9994"
	log.Printf("mock server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
