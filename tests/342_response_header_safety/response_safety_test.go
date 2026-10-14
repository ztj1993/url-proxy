package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

func createResponseSafetyProxy(tmpDir string) *proxy.Server {
	cfg := config.NewProxyConfig()

	// 文本重写规则
	cfg.RewriteRules = append(cfg.RewriteRules, config.RewriteRule{
		Scope:       "/simple/",
		TypeSpec:    "mime:text/html",
		Pattern:     "http://pypi.org/simple/",
		Replacement: "http://127.0.0.1:8888/pypi.org/simple/",
	})

	// 动态免缓存规则
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/api/",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	return proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")
}

// 1. 验证入向响应中的静态与动态逐跳标头剥离 (RFC 9110)
func TestResponseHopByHopStripping(t *testing.T) {
	mockServer := NewMockResponseServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-342-hop")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createResponseSafetyProxy(tmpDir)
	targetURL := upstream.URL + "/api/hop-by-hop-test"

	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// 验证端到端标头完整保留
	if resp.Header.Get("Server") != "mock-nginx/1.24" {
		t.Errorf("expected Server header preserved, got: %s", resp.Header.Get("Server"))
	}
	if resp.Header.Get("X-Custom-Echo") != "custom-header-value" {
		t.Errorf("expected X-Custom-Echo preserved, got: %s", resp.Header.Get("X-Custom-Echo"))
	}

	// 验证静态逐跳标头被彻底剔除 (防 HTTP/2 PROTOCOL_ERROR 与非预期断连)
	staticHops := []string{"Transfer-Encoding", "Keep-Alive", "Upgrade", "Proxy-Connection", "Connection"}
	for _, hop := range staticHops {
		if val := resp.Header.Get(hop); val != "" {
			t.Errorf("expected hop-by-hop header %s to be stripped, got: %s", hop, val)
		}
	}

	// 验证 RelayResponseHeaders 深度动态逐跳标头剥离能力
	rawUpstreamHeader := make(http.Header)
	rawUpstreamHeader.Set("Server", "mock-nginx/1.24")
	rawUpstreamHeader.Set("X-Custom-Echo", "custom-header-value")
	rawUpstreamHeader.Set("Transfer-Encoding", "chunked")
	rawUpstreamHeader.Set("Keep-Alive", "timeout=10")
	rawUpstreamHeader.Set("Upgrade", "TLS/1.0")
	rawUpstreamHeader.Set("Connection", "close, X-Dynamic-Upstream-Hop")
	rawUpstreamHeader.Set("X-Dynamic-Upstream-Hop", "must-be-stripped")

	clientDstHeader := make(http.Header)
	proxy.RelayResponseHeaders(clientDstHeader, rawUpstreamHeader)

	if val := clientDstHeader.Get("X-Dynamic-Upstream-Hop"); val != "" {
		t.Errorf("expected dynamic hop-by-hop header X-Dynamic-Upstream-Hop to be stripped, got: %s", val)
	}
	if val := clientDstHeader.Get("Connection"); val != "" {
		t.Errorf("expected Connection header stripped, got: %s", val)
	}
	if val := clientDstHeader.Get("Transfer-Encoding"); val != "" {
		t.Errorf("expected Transfer-Encoding header stripped, got: %s", val)
	}
	if clientDstHeader.Get("Server") != "mock-nginx/1.24" {
		t.Errorf("expected Server header preserved in RelayResponseHeaders")
	}
}

// 2. 验证响应体重写时强力标头清洗 (ETag, Content-MD5, Digest, Accept-Ranges, Content-Encoding 清除及 Content-Length 修正)
func TestRewriterResponseHeaderSanitization(t *testing.T) {
	mockServer := NewMockResponseServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-342-rewrite")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createResponseSafetyProxy(tmpDir)
	targetURL := upstream.URL + "/simple/pkg"

	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// 验证重写生效
	if !strings.Contains(bodyStr, "http://127.0.0.1:8888/pypi.org/simple/package-1.0.tar.gz") {
		t.Fatalf("expected rewritten URL in body, got: %s", bodyStr)
	}

	// 验证失效校验和与分片标头被清洗删除
	invalidHeaders := []string{"ETag", "Content-MD5", "Digest", "Accept-Ranges", "Content-Encoding"}
	for _, h := range invalidHeaders {
		if val := resp.Header.Get(h); val != "" {
			t.Errorf("expected header %s to be deleted after rewrite, got: %s", h, val)
		}
	}

	// 验证 Content-Length 与实际重写后字节长度严格一致
	expectedLen := strconv.Itoa(len(body))
	if cl := resp.Header.Get("Content-Length"); cl != expectedLen {
		t.Errorf("expected Content-Length to be %s, got: %s", expectedLen, cl)
	}
}

// 3. 验证非标源站强行返回 Content-Encoding: gzip 时的落盘透明解压与标头清洗
func TestForcedGzipStaticDownloadDecompression(t *testing.T) {
	mockServer := NewMockResponseServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-342-gzip")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createResponseSafetyProxy(tmpDir)
	targetURL := upstream.URL + "/pool/forced-gzip.deb"

	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// 验证响应标头中的 Content-Encoding 已被剔除，防止客户端二次解压报错
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Errorf("expected Content-Encoding header stripped for decompressed stream, got: %s", ce)
	}

	clientBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	expectedContent := "original-uncompressed-debian-binary-package-content"
	if string(clientBody) != expectedContent {
		t.Errorf("expected client to receive uncompressed body '%s', got '%s'", expectedContent, string(clientBody))
	}

	// 关键验证：直接检查磁盘上持久化存储的制品，确认落盘的是原始未压缩内容，而非 gzip 压缩包
	safeCachePath := filepath.Join(tmpDir, cache.GetSafeUrlPath(targetURL))
	diskBytes, err := os.ReadFile(safeCachePath)
	if err != nil {
		t.Fatalf("failed to read cached file on disk: %v", err)
	}

	if string(diskBytes) != expectedContent {
		t.Errorf("expected disk cache to hold raw uncompressed content '%s', got '%s'", expectedContent, string(diskBytes))
	}

	// 确认磁盘文件不是 gzip magic number (0x1f, 0x8b)
	if bytes.HasPrefix(diskBytes, []byte{0x1f, 0x8b}) {
		t.Errorf("disk file is still compressed with gzip! Cache pollution detected.")
	}
}

// 4. 验证生态强校验标头原样透传与全链路 X-Cache-Status 可观测性
func TestEcosystemDigestAndCacheStatusObservability(t *testing.T) {
	mockServer := NewMockResponseServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-342-digest")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createResponseSafetyProxy(tmpDir)

	// 4.1 验证 Docker 镜像强哈希原样透传
	dockerURL := upstream.URL + "/v2/library/ubuntu/manifests/latest"
	dockerReq := httptest.NewRequest("GET", "/"+dockerURL, nil)
	dockerW := httptest.NewRecorder()
	server.ServeHTTP(dockerW, dockerReq)

	dockerResp := dockerW.Result()
	if dockerResp.StatusCode != http.StatusOK {
		t.Fatalf("docker manifest request failed: %d", dockerResp.StatusCode)
	}
	expectedDigest := "sha256:7245b0a2c070ce5f4e1f7484df6f3b55c1e956"
	if digest := dockerResp.Header.Get("Docker-Content-Digest"); digest != expectedDigest {
		t.Errorf("expected Docker-Content-Digest '%s', got: '%s'", expectedDigest, digest)
	}

	// 4.2 验证静态下载首次回源与缓存命中的 X-Cache-Status 标头
	staticURL := upstream.URL + "/pool/main/app.deb"

	// 首次回源拉取 -> 期望 MISS
	req1 := httptest.NewRequest("GET", "/"+staticURL, nil)
	w1 := httptest.NewRecorder()
	server.ServeHTTP(w1, req1)
	resp1 := w1.Result()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first static request failed: %d", resp1.StatusCode)
	}
	if cacheStatus := resp1.Header.Get("X-Cache-Status"); cacheStatus != "MISS" {
		t.Errorf("expected first static download X-Cache-Status to be MISS, got: %s", cacheStatus)
	}

	// 再次拉取 -> 期望 HIT (本地文件已存在且大小匹配)
	req2 := httptest.NewRequest("GET", "/"+staticURL, nil)
	w2 := httptest.NewRecorder()
	server.ServeHTTP(w2, req2)
	resp2 := w2.Result()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second static request failed: %d", resp2.StatusCode)
	}
	if cacheStatus := resp2.Header.Get("X-Cache-Status"); cacheStatus != "HIT" {
		t.Errorf("expected cached static download X-Cache-Status to be HIT, got: %s", cacheStatus)
	}
}
