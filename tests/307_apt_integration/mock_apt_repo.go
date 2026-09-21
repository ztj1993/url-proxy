package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type RepoStats struct {
	InReleaseRequests uint64 `json:"inrelease_requests"`
	InRelease304s     uint64 `json:"inrelease_304s"`
	PackagesRequests  uint64 `json:"packages_requests"`
	DebRequests       uint64 `json:"deb_requests"`
}

var (
	inReleaseRequests uint64
	inRelease304s     uint64
	packagesRequests  uint64
	debRequests       uint64
	inReleaseSeq      uint64
	repoModTime       = time.Date(2026, time.September, 21, 0, 0, 0, 0, time.UTC)
	helloDebData      = bytes.Repeat([]byte("HELLODEBPKGDATA!"), 640) // 10,240 字节
	pkgToolDebData    = bytes.Repeat([]byte("PKGTOOLSDATADEB!"), 256) // 4,096 字节
	packagesXzData    = bytes.Repeat([]byte("PACKAGESXZMOCK!!"), 32)  // 512 字节
)

func main() {
	mux := http.NewServeMux()

	// 1. 动态仓库索引 InRelease
	mux.HandleFunc("/ubuntu/dists/jammy/InRelease", func(w http.ResponseWriter, r *http.Request) {
		reqCount := atomic.AddUint64(&inReleaseRequests, 1)
		seq := atomic.AddUint64(&inReleaseSeq, 1)
		etag := `"inrelease-v1.0"`
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Mon, 21 Sep 2026 00:00:00 GMT")

		if match := r.Header.Get("If-None-Match"); match == etag {
			atomic.AddUint64(&inRelease304s, 1)
			w.WriteHeader(http.StatusNotModified)
			log.Printf("[mock_repo] InRelease conditional match (304 Not Modified, req=%d)", reqCount)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Origin: Ubuntu\nSuite: jammy\nVersion: 22.04\nSequence: %d\nTimestamp: %s\n", seq, time.Now().Format(time.RFC3339Nano))
		log.Printf("[mock_repo] InRelease served 200 OK (seq=%d, req=%d)", seq, reqCount)
	})

	// 2. 动态包清单 Packages.xz
	mux.HandleFunc("/ubuntu/dists/jammy/main/binary-amd64/Packages.xz", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&packagesRequests, 1)
		w.Header().Set("Content-Type", "application/x-xz")
		w.Header().Set("ETag", `"packages-xz-v1.0"`)
		w.WriteHeader(http.StatusOK)
		w.Write(packagesXzData)
		log.Printf("[mock_repo] Packages.xz served 200 OK (%d bytes)", len(packagesXzData))
	})

	// 3. 真实标准软件包 hello.deb (支持 Range 切片与断点续传)
	mux.HandleFunc("/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&debRequests, 1)
		log.Printf("[mock_repo] hello.deb requested (Range: %s, method: %s)", r.Header.Get("Range"), r.Method)
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		http.ServeContent(w, r, "hello_2.10-2ubuntu2_amd64.deb", repoModTime, bytes.NewReader(helloDebData))
	})

	// 4. 敏感词软件包 packages-tool.deb (验证防误杀白名单)
	mux.HandleFunc("/ubuntu/pool/main/p/packages-tool/packages-tool_1.0_amd64.deb", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&debRequests, 1)
		log.Printf("[mock_repo] packages-tool.deb requested (whitelist test)")
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		http.ServeContent(w, r, "packages-tool_1.0_amd64.deb", repoModTime, bytes.NewReader(pkgToolDebData))
	})

	// 5. 统计与审计端点 (供自动化测试量化验证上游网络请求)
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := RepoStats{
			InReleaseRequests: atomic.LoadUint64(&inReleaseRequests),
			InRelease304s:     atomic.LoadUint64(&inRelease304s),
			PackagesRequests:  atomic.LoadUint64(&packagesRequests),
			DebRequests:       atomic.LoadUint64(&debRequests),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	})

	// 6. 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// 包装 Handler：兼容各种正向代理与域名转发带来的 URL 前缀
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		prefixes := []string{
			"/http://archive.ubuntu.com",
			"/https://archive.ubuntu.com",
			"/http://127.0.0.1:9997",
			"/https://127.0.0.1:9997",
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(p, prefix) {
				r.URL.Path = strings.TrimPrefix(p, prefix)
				break
			}
		}
		mux.ServeHTTP(w, r)
	})

	addr := "127.0.0.1:9997"
	log.Printf("mock APT repository server listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("mock repo error: %v", err)
	}
}
