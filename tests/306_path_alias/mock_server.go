package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

var inReleaseSeq uint64

func main() {
	mux := http.NewServeMux()

	// 1. Ubuntu 动态索引元数据 InRelease (支持条件协商 304 与自增序列)
	mux.HandleFunc("/upstream/ubuntu/dists/jammy/InRelease", func(w http.ResponseWriter, r *http.Request) {
		seq := atomic.AddUint64(&inReleaseSeq, 1)
		etag := `"v1.0"`
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Mon, 21 Sep 2026 00:00:00 GMT")

		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			log.Printf("[mock_server] InRelease conditional match (304 Not Modified)")
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Origin: Ubuntu\nSuite: jammy\nVersion: 22.04\nSequence: %d\nTime: %s\n", seq, time.Now().Format(time.RFC3339Nano))
		log.Printf("[mock_server] InRelease dynamic response served (seq=%d)", seq)
	})

	// 2. Ubuntu 安装包 (用于验证短别名重写后白名单落盘缓存)
	mux.HandleFunc("/upstream/ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("MOCK_UBUNTU_DEB_CONTENT_306"))
		log.Printf("[mock_server] pkg_1.0_amd64.deb served")
	})

	// 3. Debian 仓库索引 (用于验证多别名隔离)
	mux.HandleFunc("/upstream/debian/dists/bookworm/Release", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Origin: Debian\nSuite: bookworm\nCodename: bookworm\n"))
		log.Printf("[mock_server] Debian Release served")
	})

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	addr := "127.0.0.1:9996"
	log.Printf("mock server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("mock server error: %v", err)
	}
}
