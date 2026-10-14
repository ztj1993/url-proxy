package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

func createTestPipelineProxy(tmpDir string) *proxy.Server {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/repo/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     5 * time.Second,
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	return proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")
}

// 1. 验证常规持久化制品的并发下载合并与广播
func TestStaticDownloadCoalescing(t *testing.T) {
	stats := &MockPipelineStats{}
	mock := NewMockPipelineHandler(stats, 30*time.Millisecond)
	upstream := httptest.NewServer(mock)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-338-static")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createTestPipelineProxy(tmpDir)
	targetURL := upstream.URL + "/pool/main/app.deb"

	concurrency := 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	responses := make([][]byte, concurrency)
	statusCodes := make([]int, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/"+targetURL, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			resp := w.Result()
			statusCodes[idx] = resp.StatusCode
			body, _ := io.ReadAll(resp.Body)
			responses[idx] = body
		}(i)
	}

	wg.Wait()

	// 断言：上游仅收到 1 次请求
	upstreamStaticReqs := atomic.LoadUint64(&stats.StaticReqs)
	if upstreamStaticReqs != 1 {
		t.Fatalf("expected upstream static reqs to be 1, got %d", upstreamStaticReqs)
	}

	// 断言：所有客户端均成功接收 200 OK 且内容完整一致
	for i := 0; i < concurrency; i++ {
		if statusCodes[i] != http.StatusOK {
			t.Fatalf("client %d got status %d, expected 200", i, statusCodes[i])
		}
		if string(responses[i]) != string(mock.StaticPayload) {
			t.Fatalf("client %d body mismatch: %s", i, string(responses[i]))
		}
	}

	// 断言：本地缓存文件正确落盘
	cacheFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	stat, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("cache file not found on disk: %v", err)
	}
	if stat.Size() != int64(len(mock.StaticPayload)) {
		t.Fatalf("cache file size %d != expected %d", stat.Size(), len(mock.StaticPayload))
	}
}

// 2. 验证磁盘 TTL 时效资源的并发 304 重验证合并与续期
func TestDiskTTLRevalidationCoalescing(t *testing.T) {
	stats := &MockPipelineStats{}
	mock := NewMockPipelineHandler(stats, 30*time.Millisecond)
	upstream := httptest.NewServer(mock)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-338-ttl-304")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createTestPipelineProxy(tmpDir)
	targetURL := upstream.URL + "/repo/InRelease"
	cacheFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

	// 预热落盘
	wPre := httptest.NewRecorder()
	server.ServeHTTP(wPre, httptest.NewRequest("GET", "/"+targetURL, nil))
	if wPre.Result().StatusCode != http.StatusOK {
		t.Fatalf("preheat failed")
	}

	// 模拟过期
	past := time.Now().Add(-10 * time.Second)
	_ = os.Chtimes(cacheFile, past, past)

	mock.Always304.Store(true)
	reqsBefore := atomic.LoadUint64(&stats.TTLReqs)

	concurrency := 25
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/"+targetURL, nil)
			server.ServeHTTP(w, req)
			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 OK, got %d", resp.StatusCode)
			}
			if resp.Header.Get("X-Cache-Status") != "REVALIDATED-304" {
				t.Errorf("expected REVALIDATED-304 header, got %s", resp.Header.Get("X-Cache-Status"))
			}
		}()
	}

	wg.Wait()

	reqsAfter := atomic.LoadUint64(&stats.TTLReqs)
	// 上游应该只增加 1 次请求探测
	if reqsAfter-reqsBefore != 1 {
		t.Fatalf("expected 1 upstream TTL revalidation, got %d", reqsAfter-reqsBefore)
	}

	// 验证租约已被刷新 (mtime 晚于 past)
	newStat, _ := os.Stat(cacheFile)
	if !newStat.ModTime().After(past.Add(5 * time.Second)) {
		t.Fatalf("expected lease mtime renewal, but mtime was %v", newStat.ModTime())
	}
}

// 3. 验证上游 500 故障时的 STALE-FALLBACK 旧文件降级与广播
func TestDiskTTLStaleFallback(t *testing.T) {
	stats := &MockPipelineStats{}
	mock := NewMockPipelineHandler(stats, 30*time.Millisecond)
	upstream := httptest.NewServer(mock)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-338-stale")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createTestPipelineProxy(tmpDir)
	targetURL := upstream.URL + "/repo/InRelease"
	cacheFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

	// 预热落盘
	wPre := httptest.NewRecorder()
	server.ServeHTTP(wPre, httptest.NewRequest("GET", "/"+targetURL, nil))
	if wPre.Result().StatusCode != http.StatusOK {
		t.Fatalf("preheat failed")
	}

	// 模拟过期
	past := time.Now().Add(-10 * time.Second)
	_ = os.Chtimes(cacheFile, past, past)

	// 上游模拟 500 崩溃
	mock.Force500.Store(true)

	concurrency := 15
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/"+targetURL, nil)
			server.ServeHTTP(w, req)
			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 OK on stale fallback, got %d", resp.StatusCode)
			}
			if resp.Header.Get("X-Cache-Status") != "STALE-FALLBACK" {
				t.Errorf("expected STALE-FALLBACK, got %s", resp.Header.Get("X-Cache-Status"))
			}
			body, _ := io.ReadAll(resp.Body)
			if string(body) != string(mock.TTLPayload) {
				t.Errorf("stale body mismatch: %s", string(body))
			}
		}()
	}

	wg.Wait()
}

// 4. 验证非 200 响应纯流式透传且绝不落盘（防污染）
func TestNon200PassthroughAntiPollution(t *testing.T) {
	stats := &MockPipelineStats{}
	mock := NewMockPipelineHandler(stats, 10*time.Millisecond)
	upstream := httptest.NewServer(mock)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-338-non200")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createTestPipelineProxy(tmpDir)
	targetURL := upstream.URL + "/not-exist"

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "404 Target Not Found" {
		t.Fatalf("expected 404 body, got %s", string(body))
	}

	// 断言：磁盘上绝不生成任何缓存文件或临时文件
	cacheFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
		t.Fatalf("anti-pollution violated: non-200 file exists at %s", cacheFile)
	}
}
