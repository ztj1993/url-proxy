package main

import (
	"fmt"
	"net/http"
	"time"
)

type MockTuningHandler struct{}

func (h *MockTuningHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/slow-header":
		// 模拟上游源站假死：延迟 500ms 才输出响应头
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("slow header done"))

	case "/slow-body":
		// 快速响应响应头，但慢速分块传输正文（总时长超过 250ms）
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			time.Sleep(50 * time.Millisecond)
			fmt.Fprintf(w, "chunk-%d\n", i)
			if ok {
				flusher.Flush()
			}
		}

	case "/normal":
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("normal response payload"))

	default:
		http.NotFound(w, r)
	}
}