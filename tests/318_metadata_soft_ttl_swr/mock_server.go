package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

// MockServerStats 统计测试上游回源请求数
type MockServerStats struct {
	PypiHits         uint64
	NpmHits          uint64
	AptInReleaseHits uint64
	AptPackagesHits  uint64
}

const (
	PyPIVersion1        = "<html><body><a href=\"/pypi/pkg-1.0.whl\">pkg-1.0.whl</a></body></html>"
	PyPIVersion2        = "<html><body><a href=\"/pypi/pkg-1.0.whl\">pkg-1.0.whl</a><a href=\"/pypi/pkg-2.0.whl\">pkg-2.0.whl</a></body></html>"
	NpmMetaPayload      = `{"name":"express","version":"4.18.2"}`
	AptInReleasePayload = "Origin: MockUbuntu\nSuite: noble\nCodename: noble\n"
	AptPackagesPayload  = "Package: curl\nVersion: 8.5.0\n"
)

// NewMockHandler 创建 Mock 上游服务处理程序
func NewMockHandler(stats *MockServerStats, pypiContent *string, pypiEtag *string) http.Handler {
	mux := http.NewServeMux()

	// 1. PyPI Simple 索引 (支持 ETag 与 304 条件协商)
	mux.HandleFunc("/pypi/simple/pkg/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.PypiHits, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", *pypiEtag)

		if r.Header.Get("If-None-Match") == *pypiEtag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(*pypiContent))
	})

	// 2. npm 元数据
	mux.HandleFunc("/npm/express", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.NpmHits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"npm-v1.0"`)
		if r.Header.Get("If-None-Match") == `"npm-v1.0"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(NpmMetaPayload))
	})

	// 3. APT InRelease (带 ttl 的 bypass)
	mux.HandleFunc("/apt/InRelease", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AptInReleaseHits, 1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"inrelease-v1.0"`)
		if r.Header.Get("If-None-Match") == `"inrelease-v1.0"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(AptInReleasePayload))
	})

	// 4. APT Packages (无 ttl 的普通 bypass，用于对照)
	mux.HandleFunc("/apt/Packages", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AptPackagesHits, 1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(AptPackagesPayload))
	})

	// 5. Stats 统计接口
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "pypi: %d\nnpm: %d\napt_inrelease: %d\napt_packages: %d\n",
			atomic.LoadUint64(&stats.PypiHits),
			atomic.LoadUint64(&stats.NpmHits),
			atomic.LoadUint64(&stats.AptInReleaseHits),
			atomic.LoadUint64(&stats.AptPackagesHits))
	})

	return mux
}

func main() {
	port := flag.Int("port", 9998, "Mock server port")
	flag.Parse()

	stats := &MockServerStats{}
	content := PyPIVersion1
	etag := `"pypi-v1.0"`
	handler := NewMockHandler(stats, &content, &etag)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("[mock_server_318] listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("[mock_server_318] listen failed: %v", err)
	}
}
