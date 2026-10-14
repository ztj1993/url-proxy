package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type MockPipelineStats struct {
	TotalRequests uint64
	StaticReqs    uint64
	TTLReqs       uint64
	Non200Reqs    uint64
}

type MockPipelineHandler struct {
	Stats         *MockPipelineStats
	Delay         time.Duration
	Always304     atomic.Bool
	Force500      atomic.Bool
	Force404      atomic.Bool
	LastModified  time.Time
	StaticPayload []byte
	TTLPayload    []byte
}

func NewMockPipelineHandler(stats *MockPipelineStats, delay time.Duration) *MockPipelineHandler {
	return &MockPipelineHandler{
		Stats:         stats,
		Delay:         delay,
		LastModified:  time.Now().Add(-1 * time.Hour).Truncate(time.Second),
		StaticPayload: []byte("STATIC-PACKAGE-BINARY-DATA-CHUNK-V1"),
		TTLPayload:    []byte("DYNAMIC-METADATA-INDEX-JSON-DATA-V1"),
	}
}

func (h *MockPipelineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&h.Stats.TotalRequests, 1)
	if h.Delay > 0 {
		time.Sleep(h.Delay)
	}

	// 1. 静态持久化制品路径
	if r.URL.Path == "/pool/main/app.deb" {
		atomic.AddUint64(&h.Stats.StaticReqs, 1)
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(h.StaticPayload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(h.StaticPayload)
		return
	}

	// 2. 时效性动态元数据路径
	if r.URL.Path == "/repo/InRelease" {
		atomic.AddUint64(&h.Stats.TTLReqs, 1)

		if h.Force500.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("500 Upstream Internal Error"))
			return
		}

		if h.Always304.Load() {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		ims := r.Header.Get("If-Modified-Since")
		if ims != "" {
			if t, err := http.ParseTime(ims); err == nil && !h.LastModified.After(t) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(h.TTLPayload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(h.TTLPayload)
		return
	}

	// 3. 非 200 错误路径
	if r.URL.Path == "/not-exist" || h.Force404.Load() {
		atomic.AddUint64(&h.Stats.Non200Reqs, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 Target Not Found"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
