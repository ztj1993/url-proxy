package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// MockServerStats 统计测试上游请求数
type MockServerStats struct {
	DockerProbeHits uint64
	DockerTokenHits uint64
	DockerBlobHits  uint64
	PlainBlobHits   uint64
}

const (
	TestDockerBlobDigest  = "sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678"
	TestDockerBlobPayload = "docker-plugin-test-layer-blob-binary-payload-data-abcdef1234567890"
	TestPlainBlobPayload  = "plain-non-docker-payload-data-9876543210"
)

// NewMockHandler 创建 Mock 上游请求处理程序
func NewMockHandler(stats *MockServerStats) http.Handler {
	mux := http.NewServeMux()

	// 1. Docker /v2/ 握手探测与 Blobs
	mux.HandleFunc("/docker/v2/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/docker/v2/" || path == "/docker/v2" {
			atomic.AddUint64(&stats.DockerProbeHits, 1)
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				realm := fmt.Sprintf("http://%s/docker/v2/token", r.Host)
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s",service="registry.docker.io"`, realm))
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{}`)
			return
		}

		if path == "/docker/v2/token" {
			atomic.AddUint64(&stats.DockerTokenHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"token":"mock-docker-test-token"}`)
			return
		}

		if strings.Contains(path, "/blobs/") {
			atomic.AddUint64(&stats.DockerBlobHits, 1)
			w.Header().Set("Content-Length", strconv.Itoa(len(TestDockerBlobPayload)))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				w.Write([]byte(TestDockerBlobPayload))
			}
			return
		}

		w.WriteHeader(http.StatusNotFound)
	})

	// 2. 普通/通用路由 (未绑定 plugin:docker)
	mux.HandleFunc("/plain/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/blobs/") {
			atomic.AddUint64(&stats.PlainBlobHits, 1)
			w.Header().Set("Content-Length", strconv.Itoa(len(TestPlainBlobPayload)))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				w.Write([]byte(TestPlainBlobPayload))
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "plain-ok")
	})

	// 3. Stats 接口
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "docker_probe: %d\ndocker_token: %d\ndocker_blob: %d\nplain_blob: %d\n",
			atomic.LoadUint64(&stats.DockerProbeHits),
			atomic.LoadUint64(&stats.DockerTokenHits),
			atomic.LoadUint64(&stats.DockerBlobHits),
			atomic.LoadUint64(&stats.PlainBlobHits))
	})

	return mux
}

func main() {
	port := flag.Int("port", 9997, "Mock server port")
	flag.Parse()

	stats := &MockServerStats{}
	handler := NewMockHandler(stats)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("[mock_server] listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("[mock_server] listen failed: %v", err)
	}
}
