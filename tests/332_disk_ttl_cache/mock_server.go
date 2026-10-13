package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	InitialInReleaseContent = "Origin: Ubuntu\nLabel: Noble\nSuite: noble\nVersion: 24.04\nDate: Mon, 20 Apr 2026 00:00:00 UTC\n"
	UpdatedInReleaseContent = "Origin: Ubuntu\nLabel: Noble\nSuite: noble\nVersion: 24.04.1\nDate: Tue, 21 Apr 2026 00:00:00 UTC\n"
)

type MockServerStats struct {
	TotalRequests uint64
	Hit200        uint64
	Hit304        uint64
	HitError      uint64
}

type MockHandler struct {
	Stats           *MockServerStats
	Content         string
	LastModified    time.Time
	ForceError      atomic.Bool
	ForceUpdate     atomic.Bool
	AlwaysReturn304 atomic.Bool
}

func NewMockHandler(stats *MockServerStats) *MockHandler {
	return &MockHandler{
		Stats:        stats,
		Content:      InitialInReleaseContent,
		LastModified: time.Now().Add(-1 * time.Hour).Truncate(time.Second),
	}
}

func (h *MockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&h.Stats.TotalRequests, 1)

	if r.URL.Path == "/stats" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "total: %d\nhit_200: %d\nhit_304: %d\nhit_error: %d\n",
			atomic.LoadUint64(&h.Stats.TotalRequests),
			atomic.LoadUint64(&h.Stats.Hit200),
			atomic.LoadUint64(&h.Stats.Hit304),
			atomic.LoadUint64(&h.Stats.HitError),
		)
		return
	}

	if r.URL.Path == "/repo/InRelease" {
		if h.ForceError.Load() {
			atomic.AddUint64(&h.Stats.HitError, 1)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("502 Bad Gateway: origin error"))
			return
		}

		if h.AlwaysReturn304.Load() {
			atomic.AddUint64(&h.Stats.Hit304, 1)
			w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusNotModified)
			return
		}

		ims := r.Header.Get("If-Modified-Since")
		if ims != "" && !h.ForceUpdate.Load() {
			if t, err := http.ParseTime(ims); err == nil {
				if !h.LastModified.After(t) {
					atomic.AddUint64(&h.Stats.Hit304, 1)
					w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		atomic.AddUint64(&h.Stats.Hit200, 1)
		content := h.Content
		if h.ForceUpdate.Load() {
			content = UpdatedInReleaseContent
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
		return
	}

	http.NotFound(w, r)
}