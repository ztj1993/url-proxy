package main

import (
	"net/http"
	"sync"
	"time"
)

type RecordedRequest struct {
	Method string
	Path   string
	Header http.Header
}

type MockConditionalServer struct {
	mu           sync.Mutex
	requests     []RecordedRequest
	lastModified time.Time
	etag         string
	content      string
}

func NewMockConditionalServer(lastModified time.Time, etag string, content string) *MockConditionalServer {
	return &MockConditionalServer{
		requests:     make([]RecordedRequest, 0),
		lastModified: lastModified.Truncate(time.Second),
		etag:         etag,
		content:      content,
	}
}

func (m *MockConditionalServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.requests = append(m.requests, RecordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
	})
	m.mu.Unlock()

	// 1. RFC 9110: If-None-Match 优先级最高
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if inm == m.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// ETag 不匹配，直接强制返回 200 全量数据 (无视 If-Modified-Since)
		w.Header().Set("Last-Modified", m.lastModified.UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", m.etag)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(m.content))
		return
	}

	// 2. 评估 If-Modified-Since
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil {
			if !m.lastModified.After(t) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}

	// 3. 无条件请求或未命中条件，返回 200 OK 全量数据
	w.Header().Set("Last-Modified", m.lastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", m.etag)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(m.content))
}

func (m *MockConditionalServer) UpdateContent(newModTime time.Time, newETag string, newContent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastModified = newModTime.Truncate(time.Second)
	m.etag = newETag
	m.content = newContent
}

func (m *MockConditionalServer) LastRequest() *RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return nil
	}
	req := m.requests[len(m.requests)-1]
	return &req
}

func (m *MockConditionalServer) TotalRequests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}
