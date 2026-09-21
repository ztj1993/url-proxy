package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var inReleaseSeq uint64

func main() {
	mux := http.NewServeMux()

	// 1. 动态索引元数据 InRelease (支持条件协商 304)
	mux.HandleFunc("/dists/jammy/InRelease", func(w http.ResponseWriter, r *http.Request) {
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

	// 2. 动态索引包清单 Packages.xz
	mux.HandleFunc("/dists/jammy/main/binary-amd64/Packages.xz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-xz")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("MOCK_PACKAGES_XZ_BINARY_CONTENT"))
		log.Printf("[mock_server] Packages.xz served")
	})

	// 3. 安装包 (包含 packages 关键字，用于验证白名单防误杀强制落盘)
	mux.HandleFunc("/pool/main/p/packages-base/packages-base_1.0_amd64.deb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("MOCK_DEB_PACKAGE_CONTENT_FOR_WHITELIST_TEST"))
		log.Printf("[mock_server] packages-base.deb served")
	})

	// 4. 普通资源 (名为 Release 但在 /pool/ 目录下，验证 scope 作用域防护)
	mux.HandleFunc("/pool/main/r/release-info/Release", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("MOCK_RELEASE_NOTE_IN_POOL"))
		log.Printf("[mock_server] release-info/Release served")
	})

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/http://mirror.test") {
			r.URL.Path = strings.TrimPrefix(p, "/http://mirror.test")
		} else if strings.HasPrefix(p, "/https://mirror.test") {
			r.URL.Path = strings.TrimPrefix(p, "/https://mirror.test")
		}
		mux.ServeHTTP(w, r)
	})

	addr := "127.0.0.1:9995"
	log.Printf("mock server listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("mock server error: %v", err)
	}
}
