package main

import (
	"fmt"
	"net/http"
	"time"
)

// 这个服务器用于模拟标准上游服务器，用于验证 url-proxy 的基础透明中继与 Header 透传能力。
func main() {
	http.HandleFunc("/test-file", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] 收到下载请求: %s %s\n", time.Now().Format(time.RFC3339), r.Method, r.URL.Path)

		// 设置特征 Header，用于验证 proxy 的 Header 透传能力
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Custom-Header", "ProxyTestPass")
		w.Header().Set("Server", "Mock-Basic-Server/1.0")
		w.WriteHeader(http.StatusOK)

		content := "url-proxy basic transparent proxy test: PASSED\n"
		_, _ = w.Write([]byte(content))
	})

	port := ":9991"
	fmt.Printf("测试用基础上游服务器已启动，监听端口 %s\n", port)
	fmt.Printf("测试接口地址: http://localhost%s/test-file\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
