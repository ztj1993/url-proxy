package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	NobleInReleaseV1 = "Origin: Ubuntu\nSuite: noble\nVersion: 24.04\nData: [APT InRelease payload v1]\n"
	NobleInReleaseV2 = "Origin: Ubuntu\nSuite: noble\nVersion: 24.04.1\nData: [APT InRelease payload v2]\n"
)

type MockRevalidateStats struct {
	TotalRequests uint64
	Requests200   uint64
	Requests304   uint64
	RequestsError uint64
}

type MockRevalidateHandler struct {
	Stats        *MockRevalidateStats
	Delay        time.Duration
	Always304    atomic.Bool
	ForceError   atomic.Bool
	ServeV2      atomic.Bool
	LastModified time.Time
}

func NewMockRevalidateHandler(stats *MockRevalidateStats, delay time.Duration) *MockRevalidateHandler {
	return &MockRevalidateHandler{
		Stats:        stats,
		Delay:        delay,
		LastModified: time.Now().Add(-1 * time.Hour).Truncate(time.Second),
	}
}

func (h *MockRevalidateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&h.Stats.TotalRequests, 1)
	if h.Delay > 0 {
		time.Sleep(h.Delay)
	}

	if r.URL.Path == "/stats" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "total: %d\nrequests_200: %d\nrequests_304: %d\nrequests_error: %d\n",
			atomic.LoadUint64(&h.Stats.TotalRequests),
			atomic.LoadUint64(&h.Stats.Requests200),
			atomic.LoadUint64(&h.Stats.Requests304),
			atomic.LoadUint64(&h.Stats.RequestsError),
		)
		return
	}

	if r.URL.Path == "/repo/InRelease" {
		if h.ForceError.Load() {
			atomic.AddUint64(&h.Stats.RequestsError, 1)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("502 Bad Gateway"))
			return
		}

		if h.Always304.Load() {
			atomic.AddUint64(&h.Stats.Requests304, 1)
			w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusNotModified)
			return
		}

		ims := r.Header.Get("If-Modified-Since")
		if ims != "" && !h.ServeV2.Load() {
			atomic.AddUint64(&h.Stats.Requests304, 1)
			w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusNotModified)
			return
		}

		atomic.AddUint64(&h.Stats.Requests200, 1)
		content := NobleInReleaseV1
		if h.ServeV2.Load() {
			content = NobleInReleaseV2
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", h.LastModified.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
		return
	}

	http.NotFound(w, r)
}