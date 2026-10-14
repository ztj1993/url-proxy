package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

func createHeaderTestProxy(tmpDir string) *proxy.Server {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/repo/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     5 * time.Second,
	})
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

// 1. 测试 Bypass 分支中的多值标头保留、认证透传、逐跳标头剥离及 Host 隔离
func TestBypassHeaderRelay_MultiValueAndDynamicHops(t *testing.T) {
	mockServer := NewMockHeaderServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-341-bypass")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeaderTestProxy(tmpDir)

	upstreamURL, _ := url.Parse(upstream.URL)
	targetURL := upstream.URL + "/api/v1/info"

	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	// 设置多值标头
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Accept", "text/plain")
	req.Header.Add("X-Custom-List", "item1")
	req.Header.Add("X-Custom-List", "item2")

	// 凭证与鉴权
	req.Header.Set("Authorization", "Bearer test-secret-token")
	req.Header.Set("Cookie", "session_id=abc123xyz")

	// 静态逐跳标头
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Proxy-Connection", "keep-alive")

	// 动态逐跳标头：通过 Connection 声明的字段
	req.Header.Set("Connection", "keep-alive, X-Ephemeral-Hop, X-Dynamic-Header")
	req.Header.Set("X-Ephemeral-Hop", "strip-me")
	req.Header.Set("X-Dynamic-Header", "strip-me-too")

	// 客户端自定义 Host
	req.Host = "proxy.local.corp:8888"

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d, body: %s", resp.StatusCode, string(body))
	}

	lastReq := mockServer.LastRequest()
	if lastReq == nil {
		t.Fatalf("upstream received no request")
	}

	// 验证 Host 重写为上游真实主机
	if lastReq.Host != upstreamURL.Host {
		t.Errorf("expected Host header to be %s, got %s", upstreamURL.Host, lastReq.Host)
	}

	// 验证多值标头
	acceptVals := lastReq.Header.Values("Accept")
	if len(acceptVals) < 2 {
		t.Errorf("expected at least 2 Accept values, got: %v", acceptVals)
	}
	customVals := lastReq.Header.Values("X-Custom-List")
	if len(customVals) != 2 || customVals[0] != "item1" || customVals[1] != "item2" {
		t.Errorf("expected X-Custom-List [item1, item2], got: %v", customVals)
	}

	// 验证凭证保留
	if lastReq.Header.Get("Authorization") != "Bearer test-secret-token" {
		t.Errorf("expected Authorization header preserved, got: %s", lastReq.Header.Get("Authorization"))
	}
	if lastReq.Header.Get("Cookie") != "session_id=abc123xyz" {
		t.Errorf("expected Cookie header preserved, got: %s", lastReq.Header.Get("Cookie"))
	}

	// 验证静态逐跳标头剥离
	for _, hop := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Upgrade"} {
		if val := lastReq.Header.Get(hop); val != "" {
			t.Errorf("expected static hop-by-hop header %s to be stripped, got: %s", hop, val)
		}
	}

	// 验证动态逐跳标头剥离
	for _, dynHop := range []string{"X-Ephemeral-Hop", "X-Dynamic-Header"} {
		if val := lastReq.Header.Get(dynHop); val != "" {
			t.Errorf("expected dynamic hop-by-hop header %s to be stripped, got: %s", dynHop, val)
		}
	}
}

// 2. 测试静态下载分支防落盘压缩与未缓存时的条件标头剥离
func TestStaticDownloadHeaderRelay_CompressionAndConditionalGuard(t *testing.T) {
	mockServer := NewMockHeaderServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-341-static")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeaderTestProxy(tmpDir)
	targetURL := upstream.URL + "/pool/main/app.deb"

	req := httptest.NewRequest("GET", "/"+targetURL, nil)
	// 客户端请求 gzip 压缩
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	// 客户端发送条件请求（但是代理本地此时没有任何缓存）
	req.Header.Set("If-None-Match", `"w/deadbeef"`)
	req.Header.Set("If-Modified-Since", "Wed, 21 Oct 2025 07:28:00 GMT")
	req.Header.Set("User-Agent", "apt/2.4.12 (amd64)")

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d, body: %s", resp.StatusCode, string(body))
	}

	lastReq := mockServer.LastRequest()
	if lastReq == nil {
		t.Fatalf("upstream received no request")
	}

	// 验证 Accept-Encoding 被剥离（或者为 Go Transport 默认追加）
	// 注意：Go Transport 自动处理压缩时会在底层发送 Accept-Encoding: gzip，
	// 但在我们的 prepareRequest 阶段已显式删除客户端指定的 Accept-Encoding。
	// 更关键的是：未缓存时，If-None-Match 与 If-Modified-Since 必须被剥离
	if val := lastReq.Header.Get("If-None-Match"); val != "" {
		t.Errorf("expected If-None-Match to be stripped on non-cached static download, got: %s", val)
	}
	if val := lastReq.Header.Get("If-Modified-Since"); val != "" {
		t.Errorf("expected If-Modified-Since to be stripped on non-cached static download, got: %s", val)
	}

	// 验证应用层业务头（User-Agent）正确透传
	if lastReq.Header.Get("User-Agent") != "apt/2.4.12 (amd64)" {
		t.Errorf("expected User-Agent preserved, got: %s", lastReq.Header.Get("User-Agent"))
	}
}

// 3. 测试 Range 与 HEAD 分支标头透传
func TestHeadAndRangeHeaderRelay(t *testing.T) {
	mockServer := NewMockHeaderServer()
	upstream := httptest.NewServer(mockServer)
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-341-range")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := createHeaderTestProxy(tmpDir)
	targetURL := upstream.URL + "/pool/main/app.deb"

	// 测试 HEAD
	headReq := httptest.NewRequest("HEAD", "/"+targetURL, nil)
	headReq.Header.Set("X-Test-Head", "head-val")
	headReq.Header.Set("Connection", "close")
	headW := httptest.NewRecorder()
	server.ServeHTTP(headW, headReq)

	headResp := headW.Result()
	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD request failed: %d", headResp.StatusCode)
	}
	lastReq := mockServer.LastRequest()
	if lastReq.Header.Get("X-Test-Head") != "head-val" {
		t.Errorf("expected X-Test-Head relayed in HEAD, got: %s", lastReq.Header.Get("X-Test-Head"))
	}
	if lastReq.Header.Get("Connection") != "" {
		t.Errorf("expected Connection stripped in HEAD, got: %s", lastReq.Header.Get("Connection"))
	}

	// 测试 Range
	rangeReq := httptest.NewRequest("GET", "/"+targetURL, nil)
	rangeReq.Header.Set("Range", "bytes=0-5")
	rangeReq.Header.Set("X-Test-Range", "range-val")
	rangeW := httptest.NewRecorder()
	server.ServeHTTP(rangeW, rangeReq)

	rangeResp := rangeW.Result()
	if rangeResp.StatusCode != http.StatusOK {
		t.Fatalf("Range request failed: %d", rangeResp.StatusCode)
	}
	lastReq = mockServer.LastRequest()
	if lastReq.Header.Get("Range") != "bytes=0-5" {
		t.Errorf("expected Range header relayed, got: %s", lastReq.Header.Get("Range"))
	}
	if lastReq.Header.Get("X-Test-Range") != "range-val" {
		t.Errorf("expected X-Test-Range relayed in Range, got: %s", lastReq.Header.Get("X-Test-Range"))
	}
}
