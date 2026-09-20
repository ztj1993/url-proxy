package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// 这个服务器用于模拟一个慢速传输的大文件上游服务器。
// 用于验证 url-proxy 的请求合并（Request Coalescing）与共享流式传输（Shared Streaming）功能：
// 1. 当多个客户端同时请求该文件时，上游服务器应仅收到 1 次请求。
// 2. 数据以固定速率分块传输，给 Follower 客户端提供充足的时间加入并观察断点追赶。
func main() {
	var totalRequests int64
	fileSize := 5 * 1024 * 1024          // 5MB 模拟文件
	chunkSize := 32 * 1024                // 32KB 每块
	chunkDelay := 40 * time.Millisecond   // 每块间隔 40ms，5MB 大约需要 6.4 秒传输完成

	// 生成确定性的固定数据内容用于校验
	dummyData := make([]byte, chunkSize)
	for i := 0; i < chunkSize; i++ {
		dummyData[i] = byte(i % 256)
	}

	http.HandleFunc("/slow-file", func(w http.ResponseWriter, r *http.Request) {
		reqNum := atomic.AddInt64(&totalRequests, 1)
		nowStr := time.Now().Format(time.RFC3339)

		if reqNum == 1 {
			fmt.Printf("[%s] [上游] 收到首个下载请求 (Request #%d)，开始慢速流式传输...\n", nowStr, reqNum)
		} else {
			fmt.Printf("[%s] [警告] 收到上游重复请求 (Request #%d)！说明 url-proxy 请求合并未生效！\n", nowStr, reqNum)
		}

		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)

		flusher, isFlusher := w.(http.Flusher)

		sentBytes := 0
		for sentBytes < fileSize {
			toSend := chunkSize
			if sentBytes+toSend > fileSize {
				toSend = fileSize - sentBytes
			}

			if _, err := w.Write(dummyData[:toSend]); err != nil {
				fmt.Printf("[%s] [上游] 客户端连接中断: %v\n", time.Now().Format(time.RFC3339), err)
				return
			}
			if isFlusher {
				flusher.Flush()
			}

			sentBytes += toSend
			time.Sleep(chunkDelay)
		}

		fmt.Printf("[%s] [上游] 请求 #%d 数据传输完成 (共 %d 字节)。\n", time.Now().Format(time.RFC3339), reqNum, sentBytes)
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		count := atomic.LoadInt64(&totalRequests)
		fmt.Fprintf(w, "total_upstream_requests: %d\n", count)
	})

	port := ":9998"
	fmt.Printf("测试用慢速服务器已启动，监听端口 %s\n", port)
	fmt.Printf("提供 5MB 测试文件地址: http://localhost%s/slow-file\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
