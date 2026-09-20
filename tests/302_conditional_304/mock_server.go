package main

import (
	"fmt"
	"net/http"
	"time"
)

const (
	mockETag         = `"packages-v1-hash"`
	mockLastModified = "Sat, 20 Sep 2026 08:00:00 GMT"
	mockBody         = "Package: test-pkg\nVersion: 1.0.0\nArchitecture: amd64\n"
)

// 这个服务器用于验证 url-proxy 对条件请求（If-Modified-Since, If-None-Match）与 304 Not Modified 的处理。
func main() {
	http.HandleFunc("/conditional-test", func(w http.ResponseWriter, r *http.Request) {
		clientETag := r.Header.Get("If-None-Match")
		clientModified := r.Header.Get("If-Modified-Since")

		fmt.Printf("[%s] 收到请求: %s %s, If-None-Match: %s, If-Modified-Since: %s\n",
			time.Now().Format(time.RFC3339), r.Method, r.URL.Path, clientETag, clientModified)

		// 检查条件请求是否匹配
		if clientETag == mockETag || (clientModified != "" && clientModified == mockLastModified) {
			fmt.Printf("[%s] 命中缓存条件，返回 304 Not Modified (0 字节负载)\n", time.Now().Format(time.RFC3339))
			w.Header().Set("ETag", mockETag)
			w.Header().Set("Last-Modified", mockLastModified)
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// 首次请求或条件不匹配，返回 200 OK 并带上协商头部
		fmt.Printf("[%s] 未命中条件，返回 200 OK 完整数据\n", time.Now().Format(time.RFC3339))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", mockETag)
		w.Header().Set("Last-Modified", mockLastModified)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockBody))
	})

	port := ":9994"
	fmt.Printf("测试用条件协商目标服务器已启动，监听端口 %s\n", port)
	fmt.Printf("测试目标地址: http://localhost%s/conditional-test\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
