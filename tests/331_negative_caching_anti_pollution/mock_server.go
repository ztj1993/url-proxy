package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync/atomic"
)

const (
	NotFoundPayload    = "<html><head><title>404 Not Found</title></head><body><h1>404 Not Found</h1><p>The requested package does not exist.</p></body></html>"
	ServerErrorPayload = "500 Internal Server Error: database connection failed"
	BadGatewayPayload  = "502 Bad Gateway: upstream origin unreachable"
	ValidDebPayload    = "Package: example-lib\nVersion: 2.4.0\nArchitecture: amd64\nPayload-Content: [binary-deb-stream-data-mock-payload-bytes]\n"
)

// MockServerStats 统计测试请求指标
type MockServerStats struct {
	NotFoundHits    uint64
	ServerErrorHits uint64
	BadGatewayHits  uint64
	ValidHits       uint64
	RecoverableHits uint64
}

// MockHandler 模拟真实源站错误与正常响应
type MockHandler struct {
	Stats           *MockServerStats
	RecoverableFail *atomic.Bool
}

func NewMockHandler(stats *MockServerStats, recoverableFail *atomic.Bool) *MockHandler {
	return &MockHandler{
		Stats:           stats,
		RecoverableFail: recoverableFail,
	}
}

func (h *MockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/repo/not-found.deb":
		atomic.AddUint64(&h.Stats.NotFoundHits, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(NotFoundPayload))

	case "/repo/server-error.deb":
		atomic.AddUint64(&h.Stats.ServerErrorHits, 1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(ServerErrorPayload))

	case "/repo/bad-gateway.deb":
		atomic.AddUint64(&h.Stats.BadGatewayHits, 1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(BadGatewayPayload))

	case "/repo/valid-pkg.deb":
		atomic.AddUint64(&h.Stats.ValidHits, 1)
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ValidDebPayload))

	case "/repo/recoverable.deb":
		atomic.AddUint64(&h.Stats.RecoverableHits, 1)
		if h.RecoverableFail != nil && h.RecoverableFail.Load() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(ServerErrorPayload))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ValidDebPayload))

	case "/stats":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "not_found: %d\nserver_error: %d\nbad_gateway: %d\nvalid: %d\nrecoverable: %d\n",
			atomic.LoadUint64(&h.Stats.NotFoundHits),
			atomic.LoadUint64(&h.Stats.ServerErrorHits),
			atomic.LoadUint64(&h.Stats.BadGatewayHits),
			atomic.LoadUint64(&h.Stats.ValidHits),
			atomic.LoadUint64(&h.Stats.RecoverableHits),
		)

	default:
		http.NotFound(w, r)
	}
}

func Sha256String(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}