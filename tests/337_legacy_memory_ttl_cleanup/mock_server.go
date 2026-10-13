package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

// MockServerStats 统计测试上游请求命中情况
type MockServerStats struct {
	InReleaseHits uint64
	TokenHits     uint64
}

// NewMockServerHandler 创建 Feature 337 测试使用的 Mock 上游处理逻辑
func NewMockServerHandler(stats *MockServerStats) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/repo/InRelease", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.InReleaseHits, 1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", "Mon, 13 Oct 2026 00:00:00 GMT")

		if ims := r.Header.Get("If-Modified-Since"); ims != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Suite: test-noble\nCodename: noble\n"))
	})

	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.TokenHits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"mock-jwt-token"}`))
	})

	return mux
}

func main() {
	port := flag.Int("port", 19337, "Mock server port")
	flag.Parse()

	stats := &MockServerStats{}
	handler := NewMockServerHandler(stats)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Mock server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
