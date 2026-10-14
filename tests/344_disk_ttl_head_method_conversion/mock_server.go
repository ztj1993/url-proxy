package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

type RecordedRequest struct {
	Method string
	Path   string
	Header http.Header
}

type MockHeadServer struct {
	mu           sync.Mutex
	requests     []RecordedRequest
	lastModified time.Time
	etag         string
	content      string
	delay        time.Duration
}

func NewMockHeadServer(lastModified time.Time, etag string, content string, delay time.Duration) *MockHeadServer {
	return &MockHeadServer{
		requests:     make([]RecordedRequest, 0),
		lastModified: lastModified.Truncate(time.Second),
		etag:         etag,
		content:      content,
		delay:        delay,
	}
}

func (m *MockHeadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	m.mu.Lock()
	m.requests = append(m.requests, RecordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
	})
	content := m.content
	lastModified := m.lastModified
	etag := m.etag
	m.mu.Unlock()

	// 1. If-Modified-Since 条件协商
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil {
			if !lastModified.After(t) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}

	// 2. 正常 200 响应
	w.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// 若源站收到 HEAD 请求，只发送头部；若收到 GET 请求，发送完整正文
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(content))
	}
}

func (m *MockHeadServer) UpdateContent(newModTime time.Time, newETag string, newContent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastModified = newModTime.Truncate(time.Second)
	m.etag = newETag
	m.content = newContent
}

func (m *MockHeadServer) LastRequest() *RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return nil
	}
	req := m.requests[len(m.requests)-1]
	return &req
}

func (m *MockHeadServer) Requests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]RecordedRequest, len(m.requests))
	copy(copied, m.requests)
	return copied
}

func (m *MockHeadServer) TotalRequests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}
