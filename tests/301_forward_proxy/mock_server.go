package main

import (
	"fmt"
	"net/http"
	"time"
)

// 这个服务器用于验证 url-proxy 是否支持标准的 HTTP 正向代理协议（如 curl -x 或 http_proxy）。
func main() {
	http.HandleFunc("/proxy-test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] 收到正向代理转发请求: %s %s, Host: %s\n",
			time.Now().Format(time.RFC3339), r.Method, r.URL.Path, r.Host)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Forward-Proxy-Mock", "true")
		w.WriteHeader(http.StatusOK)

		content := "url-proxy standard forward proxy test: PASSED\n"
		_, _ = w.Write([]byte(content))
	})

	port := ":9996"
	fmt.Printf("测试用标准代理目标服务器已启动，监听端口 %s\n", port)
	fmt.Printf("测试目标地址: http://localhost%s/proxy-test\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
