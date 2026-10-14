package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type MockShutdownStats struct {
	TotalRequests uint64
	FastReqs      uint64
	SlowReqs      uint64
}

type MockShutdownHandler struct {
	Stats        *MockShutdownStats
	SlowDelay    time.Duration
	SlowChunks   int
	ChunkSize    int
	FastPayload  []byte
	StartedSlow  chan struct{}
	ContinueSlow chan struct{}
}

func NewMockShutdownHandler(stats *MockShutdownStats) *MockShutdownHandler {
	return &MockShutdownHandler{
		Stats:        stats,
		SlowDelay:    50 * time.Millisecond,
		SlowChunks:   5,
		ChunkSize:    1024,
		FastPayload:  []byte("FAST-PACKAGE-DATA-CHUNK"),
		StartedSlow:  make(chan struct{}, 10),
		ContinueSlow: make(chan struct{}, 10),
	}
}

func (h *MockShutdownHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&h.Stats.TotalRequests, 1)

	if r.URL.Path == "/fast/package.deb" {
		atomic.AddUint64(&h.Stats.FastReqs, 1)
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(h.FastPayload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(h.FastPayload)
		return
	}

	if r.URL.Path == "/slow/large-package.deb" {
		atomic.AddUint64(&h.Stats.SlowReqs, 1)
		totalLen := h.SlowChunks * h.ChunkSize
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", totalLen))
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		chunk := make([]byte, h.ChunkSize)
		for i := 0; i < len(chunk); i++ {
			chunk[i] = byte('A' + (i % 26))
		}

		for i := 0; i < h.SlowChunks; i++ {
			select {
			case h.StartedSlow <- struct{}{}:
			default:
			}

			if h.SlowDelay > 0 {
				time.Sleep(h.SlowDelay)
			}

			if _, err := w.Write(chunk); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		return
	}

	http.NotFound(w, r)
}
