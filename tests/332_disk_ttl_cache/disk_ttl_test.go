package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func createTestServer(tmpDir string) *proxy.Server {
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

func TestDiskTTLCache(t *testing.T) {
	stats := &MockServerStats{}
	mockHandler := NewMockHandler(stats)
	upstream := httptest.NewServer(mockHandler)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-332-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createTestServer(tmpDir)
	targetURL := upstream.URL + "/repo/InRelease"

	// 1. 场景一：首次下载落盘与保鲜期内命中 (HIT-DISK-TTL)
	t.Run("Scenario1_FreshnessHit", func(t *testing.T) {
		req1 := httptest.NewRequest("GET", "/"+targetURL, nil)
		w1 := httptest.NewRecorder()
		server.ServeHTTP(w1, req1)

		resp1 := w1.Result()
		if resp1.StatusCode != http.StatusOK {
			t.Fatalf("first request failed, status: %d", resp1.StatusCode)
		}
		if status := resp1.Header.Get("X-Cache-Status"); status != "MISS-DISK-TTL" {
			t.Errorf("expected MISS-DISK-TTL on first fetch, got %s", status)
		}
		body1, _ := io.ReadAll(resp1.Body)
		_ = resp1.Body.Close()
		if string(body1) != InitialInReleaseContent {
			t.Fatalf("body mismatch")
		}

		// 检查本地磁盘文件是否落盘
		diskPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
		stat, err := os.Stat(diskPath)
		if err != nil {
			t.Fatalf("expected disk file to exist at %s: %v", diskPath, err)
		}

		// 5秒内发起第2、3次请求：应直接保鲜命中，上游回源计数不增加
		hitsBefore := atomic.LoadUint64(&stats.Hit200)
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", "/"+targetURL, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("subsequent hit failed: %d", resp.StatusCode)
			}
			if status := resp.Header.Get("X-Cache-Status"); status != "HIT-DISK-TTL" {
				t.Errorf("expected HIT-DISK-TTL, got %s", status)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) != InitialInReleaseContent {
				t.Errorf("content mismatch on fresh hit")
			}
		}

		hitsAfter := atomic.LoadUint64(&stats.Hit200)
		if hitsAfter != hitsBefore {
			t.Errorf("expected zero upstream fetches during fresh TTL window, diff=%d", hitsAfter-hitsBefore)
		}

		_ = stat
	})

	// 2. 场景二：过期后 304 条件续期与 mtime 刷新 (REVALIDATED-304)
	t.Run("Scenario2_304RevalidationAndTouch", func(t *testing.T) {
		diskPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

		// 人为模拟过期：将文件 mtime 修改为 20 秒前（TTL 为 10 秒）
		pastTime := time.Now().Add(-20 * time.Second)
		if err := os.Chtimes(diskPath, pastTime, pastTime); err != nil {
			t.Fatalf("failed to chtimes: %v", err)
		}

		mockHandler.AlwaysReturn304.Store(true)
		defer mockHandler.AlwaysReturn304.Store(false)

		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK after 304 revalidation, got %d", resp.StatusCode)
		}
		if status := resp.Header.Get("X-Cache-Status"); status != "REVALIDATED-304" {
			t.Errorf("expected REVALIDATED-304, got %s", status)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != InitialInReleaseContent {
			t.Errorf("revalidated body mismatch")
		}

		// 验证磁盘文件的 mtime 是否被刷新为当前时间（大于 pastTime）
		newStat, _ := os.Stat(diskPath)
		if !newStat.ModTime().After(pastTime.Add(15 * time.Second)) {
			t.Errorf("expected mtime to be refreshed, got %v", newStat.ModTime())
		}

		// 紧接着再次请求：应自动恢复为 HIT-DISK-TTL
		reqNext := httptest.NewRequest("GET", "/"+targetURL, nil)
		wNext := httptest.NewRecorder()
		server.ServeHTTP(wNext, reqNext)
		respNext := wNext.Result()
		if status := respNext.Header.Get("X-Cache-Status"); status != "HIT-DISK-TTL" {
			t.Errorf("expected status to recover to HIT-DISK-TTL, got %s", status)
		}
	})

	// 3. 场景三：过期后上游内容更新与原子覆盖 (MISS-DISK-TTL)
	t.Run("Scenario3_ContentUpdateAndAtomicReplace", func(t *testing.T) {
		diskPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

		// 人为设置过期
		pastTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskPath, pastTime, pastTime)

		mockHandler.ForceUpdate.Store(true)
		defer mockHandler.ForceUpdate.Store(false)

		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on update, got %d", resp.StatusCode)
		}
		if status := resp.Header.Get("X-Cache-Status"); status != "MISS-DISK-TTL" {
			t.Errorf("expected MISS-DISK-TTL on content update, got %s", status)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != UpdatedInReleaseContent {
			t.Errorf("expected updated body, got %q", string(body))
		}

		// 验证磁盘文件内容已被原子更新
		fileData, err := os.ReadFile(diskPath)
		if err != nil {
			t.Fatalf("failed to read disk file: %v", err)
		}
		if string(fileData) != UpdatedInReleaseContent {
			t.Errorf("disk file not updated with new content")
		}
	})

	// 4. 场景四：源站故障旧文件容灾降级 (STALE-FALLBACK)
	t.Run("Scenario4_StaleFallbackOnUpstreamError", func(t *testing.T) {
		diskPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

		// 人为设置过期
		pastTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskPath, pastTime, pastTime)

		// 触发源站 502 故障
		mockHandler.ForceError.Store(true)
		defer mockHandler.ForceError.Store(false)

		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK via stale-fallback, got %d", resp.StatusCode)
		}
		if status := resp.Header.Get("X-Cache-Status"); status != "STALE-FALLBACK" {
			t.Errorf("expected STALE-FALLBACK, got %s", status)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != UpdatedInReleaseContent {
			t.Errorf("stale-fallback content mismatch")
		}
	})

	// 5. 场景五：服务重启持久化验证
	t.Run("Scenario5_PersistenceAcrossServerRestart", func(t *testing.T) {
		diskPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

		// 设置文件处于保鲜期内
		now := time.Now()
		_ = os.Chtimes(diskPath, now, now)

		// 模拟重启：销毁旧 server，重新初始化全新 server 实例
		restartedServer := createTestServer(tmpDir)

		hitsBefore := atomic.LoadUint64(&stats.TotalRequests)
		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()
		restartedServer.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("restart hit failed, status: %d", resp.StatusCode)
		}
		if status := resp.Header.Get("X-Cache-Status"); status != "HIT-DISK-TTL" {
			t.Errorf("expected HIT-DISK-TTL after restart, got %s", status)
		}
		hitsAfter := atomic.LoadUint64(&stats.TotalRequests)
		if hitsAfter != hitsBefore {
			t.Errorf("expected 0 requests to upstream after restart, diff=%d", hitsAfter-hitsBefore)
		}
	})
}

func TestProxyConfig332Syntax(t *testing.T) {
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

	if ttlRuleCount < 3 {
		t.Errorf("expected at least 3 bypass rules with ttl:, got %d", ttlRuleCount)
	}
}