package main

import (
	"fmt"
	"net/http"
	"time"
)

// 这个服务器用于模拟一个挂起的上游服务器。
// 它会发送响应头和一小块数据，然后无限期地休眠，
// 既不发送新数据，也不关闭连接。
func main() {
	http.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] 收到请求，发送部分数据后将挂起...\n", time.Now().Format(time.RFC3339))

		// 告诉客户端这是一个分块传输或指定一个很大的文件长度
		w.Header().Set("Content-Length", "100000000") // 假装这是一个 100MB 的文件
		w.WriteHeader(http.StatusOK)

		// 发送第一块数据，让 url-proxy 认为连接成功并开始接收
		w.Write([]byte("Hello, this is the start of the data...\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		fmt.Println("初始数据已发送。现在开始休眠以模拟挂起状态...")
		// 通过长时间休眠（1小时）来模拟上游挂起
		// 它不会再发送任何数据，也不会关闭连接。
		time.Sleep(1 * time.Hour)
	})

	port := ":9999"
	fmt.Printf("测试用挂起服务器已启动，监听端口 %s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
