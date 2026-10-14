package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

func createIsolationTestProxy(tmpDir string) *proxy.Server {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/dists/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     100 * time.Millisecond, // 100ms TTL 以便测试过期续期
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	return proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")
}

// 1. 验证未建仓时客户端条件标头全部剥离，杜绝 304 空包黑洞
func TestUncachedFirstFetch_ConditionalStripped(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	mockServer := NewMockConditionalServer(now, `"etag-v1"`, "initial-release-metadata")
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-343-uncached")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createIsolationTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"

	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	// 客户端带上条件头（如果透传会导致上游 304，破坏建仓）
	req.Header.Set("If-None-Match", `"etag-v1"`)
	req.Header.Set("If-Modified-Since", now.UTC().Format(http.TimeFormat))
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on initial fetch, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "initial-release-metadata" {
		t.Errorf("expected body 'initial-release-metadata', got '%s'", string(body))
	}

	lastReq := mockServer.LastRequest()
	if lastReq == nil {
		t.Fatalf("upstream received no request")
	}

	// 核心断言：未缓存时出向请求必须彻底剥离条件头与 Accept-Encoding
	if val := lastReq.Header.Get("If-None-Match"); val != "" {
		t.Errorf("expected If-None-Match stripped on uncached fetch, got: %s", val)
	}
	if val := lastReq.Header.Get("If-Modified-Since"); val != "" {
		t.Errorf("expected If-Modified-Since stripped on uncached fetch, got: %s", val)
	}
	if val := lastReq.Header.Get("Accept-Encoding"); val == "gzip, deflate, br, zstd" {
		t.Errorf("expected client custom Accept-Encoding stripped on disk TTL fetch, got: %s", val)
	}

	// 确认本地文件已成功建仓落盘
	cachedPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	if _, err := os.Stat(cachedPath); err != nil {
		t.Fatalf("expected cached file to be created on disk: %v", err)
	}
}

// 2. 验证过期重验证时剥离客户端异构 ETag，防止 304 协商退化为 200 全量拉取
func TestExpiredRevalidation_ETagStripped_Renew304(t *testing.T) {
	originTime := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	mockServer := NewMockConditionalServer(originTime, `"etag-current"`, "current-release-metadata")
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-343-etag")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createIsolationTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"

	// 2.1 首次拉取建仓
	req1 := httptest.NewRequest("GET", "/"+targetURL, nil)
	w1 := httptest.NewRecorder()
	server.ServeHTTP(w1, req1)
	if w1.Result().StatusCode != http.StatusOK {
		t.Fatalf("setup fetch failed: %d", w1.Result().StatusCode)
	}

	// 人为使本地缓存超出 100ms TTL
	cachedPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	staleTime := time.Now().Add(-1 * time.Second)
	_ = os.Chtimes(cachedPath, staleTime, staleTime)

	// 2.2 客户端携带不匹配的旧 ETag 发起请求
	req2 := httptest.NewRequest("GET", "/"+targetURL, nil)
	req2.Header.Set("If-None-Match", `"mismatched-stale-client-etag"`)

	w2 := httptest.NewRecorder()
	server.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second fetch failed: %d", resp2.StatusCode)
	}

	// 核心断言：代理出向必须剥离 If-None-Match，且使用本地真实修改时间触发 304 续期
	lastReq := mockServer.LastRequest()
	if val := lastReq.Header.Get("If-None-Match"); val != "" {
		t.Errorf("expected client If-None-Match stripped during revalidation, got: %s", val)
	}
	if val := lastReq.Header.Get("If-Modified-Since"); val == "" {
		t.Errorf("expected proxy local If-Modified-Since injected during revalidation")
	}

	// 确认代理响应头标记为续期成功 REVALIDATED-304
	if status := resp2.Header.Get("X-Cache-Status"); status != "REVALIDATED-304" {
		t.Errorf("expected X-Cache-Status REVALIDATED-304, got: %s", status)
	}
}

// 3. 验证“客户端新、缓存旧、源站新”时自愈覆盖，杜绝脏续期死锁
func TestSelfHealing_ClientNewer_CacheOlder_OriginNewer(t *testing.T) {
	tOld := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	tNew := time.Now().Truncate(time.Second)

	// 源站初始为旧版本 tOld
	mockServer := NewMockConditionalServer(tOld, `"etag-old"`, "version-old-content")
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-343-heal")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createIsolationTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"

	// 3.1 代理首次拉取并缓存旧版本
	req1 := httptest.NewRequest("GET", "/"+targetURL, nil)
	w1 := httptest.NewRecorder()
	server.ServeHTTP(w1, req1)
	if w1.Result().StatusCode != http.StatusOK {
		t.Fatalf("initial fetch failed: %d", w1.Result().StatusCode)
	}

	cachedPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	// 标记文件 mtime 为 tOld 且超过 TTL
	_ = os.Chtimes(cachedPath, tOld, tOld)

	// 3.2 源站发布了新版本 tNew
	mockServer.UpdateContent(tNew, `"etag-new"`, "version-new-content")

	// 3.3 客户端已经拥有新版本 tNew，向代理发起带 tNew 条件头的请求
	req2 := httptest.NewRequest("GET", "/"+targetURL, nil)
	req2.Header.Set("If-Modified-Since", tNew.UTC().Format(http.TimeFormat))

	w2 := httptest.NewRecorder()
	server.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("revalidation request failed: %d", resp2.StatusCode)
	}

	// 核心断言 1：代理出向请求决不能使用客户端的 tNew（否则源站会返回 304 产生脏续期死锁）
	lastReq := mockServer.LastRequest()
	outboundIMS := lastReq.Header.Get("If-Modified-Since")
	tNewStr := tNew.UTC().Format(http.TimeFormat)
	if outboundIMS == tNewStr {
		t.Fatalf("deadlock detected: proxy relayed client's newer If-Modified-Since '%s' to upstream", outboundIMS)
	}

	// 核心断言 2：代理拉取到源站 200 OK 新内容并成功原子更新本地缓存文件
	diskBytes, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatalf("failed to read disk cache: %v", err)
	}
	if string(diskBytes) != "version-new-content" {
		t.Errorf("expected disk cache self-healed to 'version-new-content', got '%s'", string(diskBytes))
	}

	// 核心断言 3：客户端收到最新版本内容
	body, _ := io.ReadAll(resp2.Body)
	if string(body) != "version-new-content" {
		t.Errorf("expected client to receive updated body 'version-new-content', got '%s'", string(body))
	}
}
