package main

import (
	"fmt"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
)

var (
	configRequests uint64
	indexRequests  uint64
	crateRequests  uint64
)

const (
	crateContent = "mock-cargo-crate-binary-payload-data-version-0.8.5-tar-gz"
)

func main() {
	mux := http.NewServeMux()

	// 1. 模拟 index.crates.io 根配置 (config.json)
	mux.HandleFunc("/crates-index/config.json", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&configRequests, 1)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("ETag", `"mock-config-etag-123"`)
		configJson := `{
  "dl": "https://static.crates.io/crates",
  "api": "https://crates.io"
}`
		w.Header().Set("Content-Length", strconv.Itoa(len(configJson)))
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, configJson)
		log.Printf("[mock-cargo] served config.json (req #%d)", atomic.LoadUint64(&configRequests))
	})

	// 2. 模拟 index.crates.io 稀疏索引查询 (/crates-index/...)
	mux.HandleFunc("/crates-index/", func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimRight(r.URL.Path, "/")
		if clean == "/crates-index/config.json" {
			return
		}

		atomic.AddUint64(&indexRequests, 1)
		if r.Header.Get("If-None-Match") == `"mock-index-etag-123"` {
			w.WriteHeader(http.StatusNotModified)
			log.Printf("[mock-cargo] 304 not modified for %s", r.URL.Path)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", `"mock-index-etag-123"`)

		crateName := path.Base(r.URL.Path)
		indexLine := fmt.Sprintf(`{"name":"%s","vers":"0.8.5","deps":[],"cksum":"e9154a43b6732cf3a563ab2290f66671","features":{},"yanked":false}`, crateName)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, indexLine)
		log.Printf("[mock-cargo] served sparse index for crate: %s (req #%d)", crateName, atomic.LoadUint64(&indexRequests))
	})

	// 3. 模拟 static.crates.io 源码包下载 (/crates-static/crates/...)
	mux.HandleFunc("/crates-static/crates/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".crate") {
			atomic.AddUint64(&crateRequests, 1)
			w.Header().Set("Content-Type", "application/x-tar")
			w.Header().Set("Content-Length", strconv.Itoa(len(crateContent)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, crateContent)
			log.Printf("[mock-cargo] served .crate binary: %s (req #%d)", r.URL.Path, atomic.LoadUint64(&crateRequests))
			return
		}
		http.NotFound(w, r)
	})

	// 4. 统计状态端点
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "config_requests: %d\n", atomic.LoadUint64(&configRequests))
		fmt.Fprintf(w, "index_requests: %d\n", atomic.LoadUint64(&indexRequests))
		fmt.Fprintf(w, "crate_requests: %d\n", atomic.LoadUint64(&crateRequests))
	})

	port := ":9994"
	log.Printf("Mock Cargo & Crates Server running on port %s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
