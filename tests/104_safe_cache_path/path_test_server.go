package main

import (
	"fmt"
	"net/http"
	"time"
)

// 这个服务器用于提供不同参数、冒号端口及斜杠结尾等多样化 URL，用于验证 url-proxy 的跨平台安全缓存路径生成逻辑。
func main() {
	// 场景 1：同一路径不同 Query 参数
	http.HandleFunc("/api/item", func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Query().Get("v")
		fmt.Printf("[%s] 收到 API 请求，参数 v=%s\n", time.Now().Format(time.RFC3339), version)

		content := fmt.Sprintf("Response content for version: %s\n", version)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})

	// 场景 2：以斜杠 / 结尾的无文件名请求
	http.HandleFunc("/folder/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] 收到目录请求: %s\n", time.Now().Format(time.RFC3339), r.URL.Path)

		content := "Response content for folder index\n"
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})

	port := ":9995"
	fmt.Printf("测试用安全路径验证服务器已启动，监听端口 %s\n", port)
	fmt.Printf("提供带参测试接口: http://localhost%s/api/item?v=1\n", port)
	fmt.Printf("提供目录测试接口: http://localhost%s/folder/\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
