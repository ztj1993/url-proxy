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
)

func createCleanupTestProxy(tmpDir string) (*proxy.Server, *config.ProxyConfig) {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/repo/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     10 * time.Second,
	})
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/auth/token",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	// NewServer 已移除废弃的 ttlMgr 参数
	server := proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")
	return server, cfg
}

func TestLegacyMemoryTTLCleanupAndDiskConvergence(t *testing.T) {
	stats := &MockServerStats{}
	upstream := httptest.NewServer(NewMockServerHandler(stats))
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-337-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server, _ := createCleanupTestProxy(tmpDir)
	inReleaseURL := upstream.URL + "/repo/InRelease"
	tokenURL := upstream.URL + "/auth/token"

	// 1. 验证带 TTL 的 bypass 请求走统一磁盘时效缓存管线
	t.Run("DiskTTL_Pipeline_Freshness_And_Revalidation", func(t *testing.T) {
		// 1.1 首次请求：回源并落盘
		req1 := httptest.NewRequest("GET", "/"+inReleaseURL, nil)
		w1 := httptest.NewRecorder()
		server.ServeHTTP(w1, req1)

		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w1.Code)
		}
		if status := w1.Header().Get("X-Cache-Status"); status != "MISS-DISK-TTL" {
			t.Errorf("expected MISS-DISK-TTL, got %s", status)
		}

		diskFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(inReleaseURL))
		if _, err := os.Stat(diskFile); os.IsNotExist(err) {
			t.Fatalf("expected disk file to exist at %s", diskFile)
		}

		// 1.2 保鲜期内第二次请求：磁盘 HIT-DISK-TTL 命中
		req2 := httptest.NewRequest("GET", "/"+inReleaseURL, nil)
		w2 := httptest.NewRecorder()
		server.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w2.Code)
		}
		if status := w2.Header().Get("X-Cache-Status"); status != "HIT-DISK-TTL" {
			t.Errorf("expected HIT-DISK-TTL, got %s", status)
		}

		// 1.3 手工让磁盘文件过期，触发 304 条件续期
		oldTime := time.Now().Add(-20 * time.Second)
		_ = os.Chtimes(diskFile, oldTime, oldTime)

		req3 := httptest.NewRequest("GET", "/"+inReleaseURL, nil)
		w3 := httptest.NewRecorder()
		server.ServeHTTP(w3, req3)

		if w3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w3.Code)
		}
		if status := w3.Header().Get("X-Cache-Status"); status != "REVALIDATED-304" {
			t.Errorf("expected REVALIDATED-304, got %s", status)
		}
	})

	// 2. 验证无 TTL 的普通 bypass 请求纯流式直通，严禁在磁盘上生成文件
	t.Run("PureBypass_Streaming_ZeroDiskPersistence", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/"+tokenURL, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		body, _ := io.ReadAll(w.Body)
		if !strings.Contains(string(body), "mock-jwt-token") {
			t.Errorf("expected body to contain token, got: %s", string(body))
		}

		tokenDiskFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(tokenURL))
		if _, err := os.Stat(tokenDiskFile); !os.IsNotExist(err) {
			t.Errorf("expected pure bypass request NOT to create disk file, but found: %s", tokenDiskFile)
		}
	})

	// 3. 验证非 GET/HEAD 请求安全流式穿透，不落盘且不抛异常
	t.Run("NonGet_Request_SafePassthrough", func(t *testing.T) {
		reqPost := httptest.NewRequest("POST", "/"+tokenURL, strings.NewReader("user=admin"))
		wPost := httptest.NewRecorder()
		server.ServeHTTP(wPost, reqPost)

		if wPost.Code != http.StatusOK {
			t.Errorf("expected 200 OK for POST bypass, got %d", wPost.Code)
		}
	})

	// 4. 验证 Mock 上游调用次数符合预期
	if inHits := atomic.LoadUint64(&stats.InReleaseHits); inHits != 2 {
		t.Errorf("expected 2 inrelease hits (initial fetch + 1 revalidation), got %d", inHits)
	}
	if tokenHits := atomic.LoadUint64(&stats.TokenHits); tokenHits != 2 {
		t.Errorf("expected 2 token hits (GET + POST), got %d", tokenHits)
	}
}
