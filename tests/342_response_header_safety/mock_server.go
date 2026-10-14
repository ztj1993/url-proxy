package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"sync"
)

type MockResponseServer struct {
	mu sync.Mutex
}

func NewMockResponseServer() *MockResponseServer {
	return &MockResponseServer{}
}

func (m *MockResponseServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 模拟包含各种静态与动态逐跳标头的上游
	if r.URL.Path == "/api/hop-by-hop-test" {
		w.Header().Set("Server", "mock-nginx/1.24")
		w.Header().Set("X-Custom-Echo", "custom-header-value")

		// 静态逐跳标头
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("Keep-Alive", "timeout=10, max=100")
		w.Header().Set("Upgrade", "TLS/1.0")

		// 动态逐跳标头
		w.Header().Set("Connection", "close, X-Dynamic-Upstream-Hop")
		w.Header().Set("X-Dynamic-Upstream-Hop", "must-be-stripped")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hop-by-hop-payload"))
		return
	}

	// 2. 模拟待重写的响应，携带 ETag, Content-MD5, Digest, Accept-Ranges
	if r.URL.Path == "/simple/pkg" {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("ETag", `"w/orig-12345"`)
		w.Header().Set("Content-MD5", "Q2hlY2sgSW50ZWdyaXR5IQ==")
		w.Header().Set("Digest", "sha-256=X48E9qOokqqrvdts8nOJRJN3OWDUoyWxBf7k8CFgn8A=")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		// 包含需要重写的主机链接
		_, _ = w.Write([]byte(`<html><a href="http://pypi.org/simple/package-1.0.tar.gz">pkg</a></html>`))
		return
	}

	// 3. 模拟违规强行返回 Content-Encoding: gzip 的静态制品下载
	if r.URL.Path == "/pool/forced-gzip.deb" {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Header().Set("Content-Encoding", "gzip")

		rawPayload := []byte("original-uncompressed-debian-binary-package-content")
		var gzBuf bytes.Buffer
		gzWriter := gzip.NewWriter(&gzBuf)
		_, _ = gzWriter.Write(rawPayload)
		_ = gzWriter.Close()

		compressedBytes := gzBuf.Bytes()
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(compressedBytes)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressedBytes)
		return
	}

	// 4. 模拟 Docker Registry Manifest 请求
	if r.URL.Path == "/v2/library/ubuntu/manifests/latest" {
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.Header().Set("Docker-Content-Digest", "sha256:7245b0a2c070ce5f4e1f7484df6f3b55c1e956")
		w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}`))
		return
	}

	// 5. 常规静态包
	if r.URL.Path == "/pool/main/app.deb" {
		data := []byte("mock-deb-binary-app-v1")
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2025 07:28:00 GMT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
