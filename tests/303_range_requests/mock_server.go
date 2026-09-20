package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 预置 100 字节固定测试数据 (由 10 组 0-9 构成，方便验证各切片偏移与内容)
const testPayload = "0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789"

func main() {
	http.HandleFunc("/range-test", func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		totalSize := len(testPayload)

		fmt.Printf("[%s] 收到请求: %s %s, Range: %s\n",
			time.Now().Format(time.RFC3339), r.Method, r.URL.Path, rangeHeader)

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		// 如果没有 Range 请求头，返回 200 OK 全量数据
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(totalSize))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(testPayload))
			return
		}

		// 解析简单的 bytes=start-end 或 bytes=start-
		if !strings.HasPrefix(rangeHeader, "bytes=") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		byteRange := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(byteRange, "-")
		if len(parts) != 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		start := 0
		end := totalSize - 1
		var err error

		if parts[0] != "" {
			start, err = strconv.Atoi(parts[0])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		if parts[1] != "" {
			end, err = strconv.Atoi(parts[1])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		// 检查范围合法性
		if start < 0 || start >= totalSize || end < start {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		if end >= totalSize {
			end = totalSize - 1
		}

		sliceLen := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
		w.Header().Set("Content-Length", strconv.Itoa(sliceLen))
		w.WriteHeader(http.StatusPartialContent)

		sliced := testPayload[start : end+1]
		_, _ = w.Write([]byte(sliced))
	})

	port := ":9993"
	fmt.Printf("测试用 Range 断点续传源站已启动，监听端口 %s\n", port)
	fmt.Printf("测试目标地址: http://localhost%s/range-test\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
