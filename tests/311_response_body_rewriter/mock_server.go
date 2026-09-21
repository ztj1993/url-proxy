package main

import (
	"compress/gzip"
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	// 1. PyPI HTML 索引
	mux.HandleFunc("/upstream/pypi/simple/requests/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"mock-pypi-etag-123"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<!DOCTYPE html><html><body><a href="https://files.pythonhosted.org/packages/a/b/requests-2.31.0.whl">requests</a></body></html>`)
		log.Printf("[mock_server] served PyPI HTML index")
	})

	// 2. npm JSON 元数据
	mux.HandleFunc("/upstream/npm/pkg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"mock-npm-etag-456"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"name":"pkg","dist":{"tarball":"https://registry.npmjs.org/pkg/-/pkg-1.0.tgz"}}`)
		log.Printf("[mock_server] served npm JSON metadata")
	})

	// 3. 二进制安装包 (包含 pattern 字符串但严禁被改动)
	mux.HandleFunc("/upstream/pypi/binary/test.whl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"mock-binary-etag-789"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("RAW_BINARY_STREAM_WITH_https://files.pythonhosted.org/_KEEP_SAFE"))
		log.Printf("[mock_server] served binary file")
	})

	// 4. Gzip 压缩响应 (测试透明解压重写)
	mux.HandleFunc("/upstream/pypi/gzip/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		gw := gzip.NewWriter(w)
		gw.Write([]byte(`<html><a href="https://files.pythonhosted.org/gz.whl">gz</a></html>`))
		gw.Close()
		log.Printf("[mock_server] served gzipped index")
	})

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	addr := "127.0.0.1:9998"
	log.Printf("mock server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("mock server error: %v", err)
	}
}
