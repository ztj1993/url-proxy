package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	"uri-proxy/internal/ttl"
)

func createTestProxy(tmpDir string) *proxy.Server {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/repo/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     10 * time.Second,
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	ttlMgr := ttl.NewManager()
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	return proxy.NewServer(cfg, tmpDir, downMgr, ttlMgr, rw, plugins, ":8888")
}

func TestConcurrentRevalidationCoalescing(t *testing.T) {
	stats := &MockRevalidateStats{}
	// 给 Mock 源站增加 30ms 延迟，确保并发请求能充分汇聚到同一个 Leader 上
	mockHandler := NewMockRevalidateHandler(stats, 30*time.Millisecond)
	upstream := httptest.NewServer(mockHandler)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-333-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createTestProxy(tmpDir)
	targetURL := upstream.URL + "/repo/InRelease"
	diskPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

	// 预热：首次拉取并使文件落盘
	reqPre := httptest.NewRequest("GET", "/"+targetURL, nil)
	wPre := httptest.NewRecorder()
	server.ServeHTTP(wPre, reqPre)
	if wPre.Result().StatusCode != http.StatusOK {
		t.Fatalf("preheat failed")
	}

	// 1. 场景一：高并发 304 探测合一验证 (30 并发)
	t.Run("Scenario1_HighConcurrency304Coalescing", func(t *testing.T) {
		// 人为模拟过期
		pastTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskPath, pastTime, pastTime)

		mockHandler.Always304.Store(true)
		defer mockHandler.Always304.Store(false)

		requestsBefore := atomic.LoadUint64(&stats.TotalRequests)
		concurrency := 30
		var wg sync.WaitGroup
		wg.Add(concurrency)

		statusCodes := make([]int, concurrency)
		cacheStatuses := make([]string, concurrency)
		errCh := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/"+targetURL, nil)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, req)

				resp := w.Result()
				statusCodes[idx] = resp.StatusCode
				cacheStatuses[idx] = resp.Header.Get("X-Cache-Status")
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("routine %d status %d", idx, resp.StatusCode)
				}
				if string(body) != NobleInReleaseV1 {
					errCh <- fmt.Errorf("routine %d body mismatch", idx)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("error in concurrent 304: %v", err)
		}

		// 核心断言：30 个并发请求严格合一，上游仅收到 1 次请求！
		requestsAfter := atomic.LoadUint64(&stats.TotalRequests)
		upstreamDelta := requestsAfter - requestsBefore
		if upstreamDelta != 1 {
			t.Errorf("expected strictly 1 upstream request for 30 concurrent probes, got %d", upstreamDelta)
		}

		// 全部客户端收到 REVALIDATED-304
		for i, cs := range cacheStatuses {
			if cs != "REVALIDATED-304" {
				t.Errorf("routine %d expected REVALIDATED-304, got %s", i, cs)
			}
		}
	})

	// 2. 场景二：高并发 200 更新单流下载验证 (30 并发)
	t.Run("Scenario2_HighConcurrency200UpdateCoalescing", func(t *testing.T) {
		pastTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskPath, pastTime, pastTime)

		mockHandler.ServeV2.Store(true)
		defer mockHandler.ServeV2.Store(false)

		requestsBefore := atomic.LoadUint64(&stats.TotalRequests)
		concurrency := 30
		var wg sync.WaitGroup
		wg.Add(concurrency)

		errCh := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/"+targetURL, nil)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, req)

				resp := w.Result()
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("routine %d status %d", idx, resp.StatusCode)
				}
				if string(body) != NobleInReleaseV2 {
					errCh <- fmt.Errorf("routine %d body did not receive V2 content", idx)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("error in concurrent 200 update: %v", err)
		}

		// 核心断言：30 个并发客户端全量更新时，上游严格只发生 1 次下载！
		requestsAfter := atomic.LoadUint64(&stats.TotalRequests)
		upstreamDelta := requestsAfter - requestsBefore
		if upstreamDelta != 1 {
			t.Errorf("expected strictly 1 upstream download for 30 concurrent updates, got %d", upstreamDelta)
		}

		// 检查磁盘文件内容已被原子替换为 V2
		diskData, err := os.ReadFile(diskPath)
		if err != nil {
			t.Fatalf("failed to read disk file: %v", err)
		}
		if string(diskData) != NobleInReleaseV2 {
			t.Errorf("disk file not updated with V2 content")
		}
	})

	// 3. 场景三：Leader 中途断连隔离验证
	t.Run("Scenario3_LeaderAbortIsolation", func(t *testing.T) {
		pastTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskPath, pastTime, pastTime)

		mockHandler.Always304.Store(true)
		defer mockHandler.Always304.Store(false)

		var wg sync.WaitGroup
		wg.Add(5)

		followerErrs := make(chan error, 5)

		// 启动 Leader 协程，模拟客户端 context 提前 cancel
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest("GET", "/"+targetURL, nil).WithContext(ctx)
			w := httptest.NewRecorder()
			// 立即取消 Leader 客户端连接
			cancel()
			server.ServeHTTP(w, req)
		}()

		// 紧接着启动 4 个 Followers
		for i := 0; i < 4; i++ {
			go func(idx int) {
				defer wg.Done()
				time.Sleep(5 * time.Millisecond)
				req := httptest.NewRequest("GET", "/"+targetURL, nil)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, req)

				resp := w.Result()
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					followerErrs <- fmt.Errorf("follower %d status %d", idx, resp.StatusCode)
				}
				if len(body) == 0 {
					followerErrs <- fmt.Errorf("follower %d received empty body", idx)
				}
			}(i)
		}

		wg.Done() // Leader 已由单独 goroutine 处理
		wg.Wait()
		close(followerErrs)

		for err := range followerErrs {
			t.Errorf("follower failed after leader abort: %v", err)
		}
	})

	// 4. 场景四：源站故障下的并发降级验证 (STALE-FALLBACK)
	t.Run("Scenario4_ConcurrentStaleFallback", func(t *testing.T) {
		pastTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskPath, pastTime, pastTime)

		mockHandler.ForceError.Store(true)
		defer mockHandler.ForceError.Store(false)

		concurrency := 20
		var wg sync.WaitGroup
		wg.Add(concurrency)

		errCh := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/"+targetURL, nil)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, req)

				resp := w.Result()
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("routine %d status %d", idx, resp.StatusCode)
				}
				if status := resp.Header.Get("X-Cache-Status"); status != "STALE-FALLBACK" {
					errCh <- fmt.Errorf("routine %d expected STALE-FALLBACK, got %s", idx, status)
				}
				if len(body) == 0 {
					errCh <- fmt.Errorf("routine %d received empty body", idx)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("concurrent fallback error: %v", err)
		}
	})
}

func TestProxyConfig333Syntax(t *testing.T) {
	data, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	ttlRuleCount := 0
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "bypass") && strings.Contains(trimmed, "ttl:") {
			ttlRuleCount++
		}
	}

	if ttlRuleCount < 2 {
		t.Errorf("expected at least 2 bypass rules with ttl:, got %d", ttlRuleCount)
	}
}