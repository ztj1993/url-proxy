package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// 这个服务器用于模拟上游服务器并统计下载请求次数，用于验证 url-proxy 的本地文件缓存命中（Cache Hit）机制。
func main() {
	var requestCount int64
	content := "url-proxy local cache test content: hello world!\n"

	http.HandleFunc("/cache-file", func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt64(&requestCount, 1)
		fmt.Printf("[%s] [上游服务器] 收到下载请求 #%d\n", time.Now().Format(time.RFC3339), count)

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		count := atomic.LoadInt64(&requestCount)
		fmt.Fprintf(w, "upstream_requests: %d\n", count)
	})

	port := ":9992"
	fmt.Printf("测试用缓存验证上游服务器已启动，监听端口 %s\n", port)
	fmt.Printf("测试文件地址: http://localhost%s/cache-file\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
