package main

import (
	"fmt"
	"net/http"
	"sync"
)

// RecordedRequest 记录上游收到的请求信息
type RecordedRequest struct {
	Method string
	Path   string
	Host   string
	Header http.Header
}

// MockHeaderServer 记录上游收到的请求头以供测试断言
type MockHeaderServer struct {
	mu       sync.Mutex
	requests []RecordedRequest
}

func NewMockHeaderServer() *MockHeaderServer {
	return &MockHeaderServer{
		requests: make([]RecordedRequest, 0),
	}
}

func (m *MockHeaderServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	reqCopy := RecordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Host:   r.Host,
		Header: r.Header.Clone(),
	}
	m.requests = append(m.requests, reqCopy)
	m.mu.Unlock()

	// 特殊测试路由
	if r.URL.Path == "/pool/main/app.deb" {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Header().Set("Content-Length", "12")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock-deb-pkg"))
		return
	}

	if r.URL.Path == "/v2/manifests/latest" {
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.Header().Set("Docker-Content-Digest", "sha256:mockdigest123")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"schemaVersion":2}`)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func (m *MockHeaderServer) GetRequests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]RecordedRequest, len(m.requests))
	copy(res, m.requests)
	return res
}

func (m *MockHeaderServer) LastRequest() *RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return nil
	}
	req := m.requests[len(m.requests)-1]
	return &req
}
