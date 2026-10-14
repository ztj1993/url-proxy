package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

func createHeadConversionTestProxy(tmpDir string) *proxy.Server {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/dists/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     100 * time.Millisecond, // 100ms TTL 便于快速测试过期
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	return proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")
}

// 1. 验证未缓存首拉 HEAD 请求强制转 GET 回源，客户端零 Body 交付，本地完整持久化建仓
func TestUncachedHeadConvertedToGetPersistence(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	content := "Suite: stable\nCodename: bookworm\nComponents: main\nSHA256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	mockServer := NewMockHeadServer(now, `"etag-v1"`, content, 0)
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-344-uncached-head")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeadConversionTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"

	// 客户端发起 HEAD 请求
	req := httptest.NewRequest("HEAD", "/"+targetURL, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for HEAD request, got %d", resp.StatusCode)
	}
	if status := resp.Header.Get("X-Cache-Status"); status != "MISS-DISK-TTL" {
		t.Fatalf("expected MISS-DISK-TTL header, got %s", status)
	}

	// 下游客户端收到的 Body 必须严格为 0 字节
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(body) != 0 {
		t.Fatalf("expected strictly 0 body bytes for HEAD response, got %d bytes: %s", len(body), string(body))
	}

	// 核心断言 1：上游实际接收到的回源方法必须被强制转换为 GET
	lastReq := mockServer.LastRequest()
	if lastReq == nil {
		t.Fatalf("expected upstream to receive a request")
	}
	if lastReq.Method != "GET" {
		t.Fatalf("expected upstream fetch method to be converted to GET, but got %s", lastReq.Method)
	}

	// 核心断言 2：本地磁盘必须生成非 0 字节的完整有效文件 (等待后台 Leader 异步写入与重命名完成)
	diskFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	var stat os.FileInfo
	for i := 0; i < 50; i++ {
		stat, err = os.Stat(diskFile)
		if err == nil && stat.Size() == int64(len(content)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("expected disk cache file to exist at %s: %v", diskFile, err)
	}
	if stat.Size() != int64(len(content)) {
		t.Fatalf("expected disk file size %d, got %d (0-byte corruption detected!)", len(content), stat.Size())
	}

	diskData, err := os.ReadFile(diskFile)
	if err != nil || string(diskData) != content {
		t.Fatalf("disk content mismatch: expected %q, got %q", content, string(diskData))
	}

	// 核心断言 3：随后的 GET 请求在保鲜期内直接命中 (HIT-DISK-TTL) 且内容完整
	reqGet := httptest.NewRequest("GET", "/"+targetURL, nil)
	wGet := httptest.NewRecorder()
	server.ServeHTTP(wGet, reqGet)

	respGet := wGet.Result()
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for subsequent GET, got %d", respGet.StatusCode)
	}
	if status := respGet.Header.Get("X-Cache-Status"); status != "HIT-DISK-TTL" {
		t.Fatalf("expected HIT-DISK-TTL for subsequent GET, got %s", status)
	}
	bodyGet, _ := io.ReadAll(respGet.Body)
	_ = respGet.Body.Close()
	if string(bodyGet) != content {
		t.Fatalf("subsequent GET body mismatch: expected %q, got %q", content, string(bodyGet))
	}

	// 上游请求数应该仍然为 1（后续 GET 完全命中缓存）
	if total := mockServer.TotalRequests(); total != 1 {
		t.Fatalf("expected exactly 1 upstream request, got %d", total)
	}
}

// 2. 验证过期资源更新时客户端发 HEAD 请求，本地旧文件被原子替换为新版本完整数据，绝不被 0 字节截断
func TestExpiredHeadAtomicallyReplacesWithFullContent(t *testing.T) {
	oldTime := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	oldContent := "Version: 1.0.0"
	mockServer := NewMockHeadServer(oldTime, `"etag-v1"`, oldContent, 0)
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-344-expired-update")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeadConversionTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"

	// 1. 预热建立旧版本缓存
	wPre := httptest.NewRecorder()
	server.ServeHTTP(wPre, httptest.NewRequest("GET", "/"+targetURL, nil))
	if wPre.Result().StatusCode != http.StatusOK {
		t.Fatalf("preheat failed")
	}

	diskFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

	// 2. 将本地文件时间戳倒拨，制造过期
	expiredTime := time.Now().Add(-1 * time.Second)
	_ = os.Chtimes(diskFile, expiredTime, expiredTime)

	// 3. 源站发布新版本
	newTime := time.Now().Truncate(time.Second)
	newContent := "Version: 2.0.0 (Updated Features and Security Patches)"
	mockServer.UpdateContent(newTime, `"etag-v2"`, newContent)

	// 4. 客户端以 HEAD 方法发起过期重验证请求
	wHead := httptest.NewRecorder()
	server.ServeHTTP(wHead, httptest.NewRequest("HEAD", "/"+targetURL, nil))
	respHead := wHead.Result()

	if respHead.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for expired update HEAD, got %d", respHead.StatusCode)
	}
	headBody, _ := io.ReadAll(respHead.Body)
	_ = respHead.Body.Close()
	if len(headBody) != 0 {
		t.Fatalf("expected 0 body bytes for HEAD response, got %d", len(headBody))
	}

	// 5. 核心断言：本地磁盘文件必须被原子更新为新版本的完整数据，而非 0 字节空文件 (等待后台 Leader 异步完成原子替换)
	var newStat os.FileInfo
	for i := 0; i < 50; i++ {
		newStat, err = os.Stat(diskFile)
		if err == nil && newStat.Size() == int64(len(newContent)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to stat disk file: %v", err)
	}
	if newStat.Size() != int64(len(newContent)) {
		t.Fatalf("expected updated disk file size %d, got %d (0-byte corruption!)", len(newContent), newStat.Size())
	}
	diskData, _ := os.ReadFile(diskFile)
	if string(diskData) != newContent {
		t.Fatalf("expected updated content %q, got %q", newContent, string(diskData))
	}

	// 6. 随后的 GET 请求必须读到新版本完整内容
	wGet := httptest.NewRecorder()
	server.ServeHTTP(wGet, httptest.NewRequest("GET", "/"+targetURL, nil))
	bodyGet, _ := io.ReadAll(wGet.Result().Body)
	_ = wGet.Result().Body.Close()
	if string(bodyGet) != newContent {
		t.Fatalf("expected subsequent GET to receive %q, got %q", newContent, string(bodyGet))
	}
}

// 3. 验证过期资源未改动时客户端发 HEAD 请求，源站返回 304，本地文件 mtime 成功续期
func TestExpiredHeadRevalidated304(t *testing.T) {
	past := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	content := "Package: stable-packages"
	mockServer := NewMockHeadServer(past, `"etag-v1"`, content, 0)
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-344-304")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeadConversionTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"
	diskFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))

	// 1. 预热建立缓存
	wPre := httptest.NewRecorder()
	server.ServeHTTP(wPre, httptest.NewRequest("GET", "/"+targetURL, nil))
	if wPre.Result().StatusCode != http.StatusOK {
		t.Fatalf("preheat failed")
	}

	// 2. 模拟本地过期
	expiredTime := time.Now().Add(-2 * time.Second)
	_ = os.Chtimes(diskFile, expiredTime, expiredTime)

	// 3. 客户端发 HEAD 请求（上游内容未改动，将返回 304）
	wHead := httptest.NewRecorder()
	server.ServeHTTP(wHead, httptest.NewRequest("HEAD", "/"+targetURL, nil))
	respHead := wHead.Result()

	if respHead.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for HEAD request (served from revalidated cache), got %d", respHead.StatusCode)
	}
	if status := respHead.Header.Get("X-Cache-Status"); status != "REVALIDATED-304" {
		t.Fatalf("expected REVALIDATED-304 header, got %s", status)
	}
	headBody, _ := io.ReadAll(respHead.Body)
	_ = respHead.Body.Close()
	if len(headBody) != 0 {
		t.Fatalf("expected 0 bytes body, got %d", len(headBody))
	}

	// 4. 验证本地文件 mtime 被刷新
	statAfter, _ := os.Stat(diskFile)
	if !statAfter.ModTime().After(expiredTime.Add(1 * time.Second)) {
		t.Fatalf("expected lease mtime renewal, but modtime was %v", statAfter.ModTime())
	}
}

// 4. 验证并发 HEAD 与 GET 汇聚：HEAD 先到成为 Leader，GET 后到作为 Follower 追读完整流
func TestConcurrentHeadAndGetCoalescing(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	content := "Concurrent Test Payload: 1234567890 ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	// 增加 50ms 延迟确保并发时序稳定命中 singleflight
	mockServer := NewMockHeadServer(now, `"etag-coalesce"`, content, 50*time.Millisecond)
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-344-concurrent")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeadConversionTestProxy(tmpDir)
	targetURL := upstream.URL + "/dists/InRelease"

	var wg sync.WaitGroup
	wg.Add(2)

	var headStatusCode int
	var headBodyBytes []byte
	var getStatusCode int
	var getBodyBytes []byte

	// 协程 1：HEAD 请求 (率先启动)
	go func() {
		defer wg.Done()
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("HEAD", "/"+targetURL, nil))
		resp := w.Result()
		headStatusCode = resp.StatusCode
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		headBodyBytes = b
	}()

	// 微延迟确保 HEAD 率先注册为 Leader
	time.Sleep(5 * time.Millisecond)

	// 协程 2：GET 请求 (作为 Follower 追读流)
	go func() {
		defer wg.Done()
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", "/"+targetURL, nil))
		resp := w.Result()
		getStatusCode = resp.StatusCode
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		getBodyBytes = b
	}()

	wg.Wait()

	// 断言 1：上游仅收到 1 次请求，且为 GET
	if total := mockServer.TotalRequests(); total != 1 {
		t.Fatalf("expected exactly 1 coalesced upstream request, got %d", total)
	}
	if req := mockServer.LastRequest(); req.Method != "GET" {
		t.Fatalf("expected upstream method to be GET, got %s", req.Method)
	}

	// 断言 2：HEAD 客户端收到 200 OK，Body 为 0 字节
	if headStatusCode != http.StatusOK {
		t.Fatalf("expected HEAD status 200, got %d", headStatusCode)
	}
	if len(headBodyBytes) != 0 {
		t.Fatalf("expected HEAD body to be 0 bytes, got %d", len(headBodyBytes))
	}

	// 断言 3：GET 客户端收到 200 OK，Body 完整
	if getStatusCode != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d", getStatusCode)
	}
	if string(getBodyBytes) != content {
		t.Fatalf("expected GET body %q, got %q", content, string(getBodyBytes))
	}

	// 断言 4：本地文件落盘完整
	diskFile := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	stat, err := os.Stat(diskFile)
	if err != nil || stat.Size() != int64(len(content)) {
		t.Fatalf("expected disk cache file size %d, got %v (size: %d)", len(content), err, stat.Size())
	}
}
