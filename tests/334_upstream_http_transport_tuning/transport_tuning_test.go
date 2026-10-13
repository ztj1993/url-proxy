package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

func createServerWithTimeout(t *testing.T, tmpDir string, headerTimeout time.Duration) *proxy.Server {
	cfg := config.NewProxyConfig()
	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")

	if headerTimeout > 0 {
		tunedTransport := proxy.NewUpstreamTransport()
		tunedTransport.ResponseHeaderTimeout = headerTimeout
		server.SetHTTPClient(&http.Client{Transport: tunedTransport})
	}

	return server
}

func TestUpstreamTransportTuning(t *testing.T) {
	// 1. 场景一：传输层连接池参数初始化验证
	t.Run("Scenario1_TransportParametersVerification", func(t *testing.T) {
		tr := proxy.NewUpstreamTransport()
		if tr.MaxIdleConns != 1000 {
			t.Errorf("expected MaxIdleConns=1000, got %d", tr.MaxIdleConns)
		}
		if tr.MaxIdleConnsPerHost != 100 {
			t.Errorf("expected MaxIdleConnsPerHost=100, got %d", tr.MaxIdleConnsPerHost)
		}
		if tr.IdleConnTimeout != 90*time.Second {
			t.Errorf("expected IdleConnTimeout=90s, got %v", tr.IdleConnTimeout)
		}
		if tr.TLSHandshakeTimeout != 10*time.Second {
			t.Errorf("expected TLSHandshakeTimeout=10s, got %v", tr.TLSHandshakeTimeout)
		}
		if tr.ResponseHeaderTimeout != 30*time.Second {
			t.Errorf("expected ResponseHeaderTimeout=30s, got %v", tr.ResponseHeaderTimeout)
		}
		if !tr.ForceAttemptHTTP2 {
			t.Errorf("expected ForceAttemptHTTP2=true")
		}

		tmpDir, err := os.MkdirTemp("", "proxy-334-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		server := createServerWithTimeout(t, tmpDir, 0)
		client := server.HTTPClient()
		serverTr, ok := client.Transport.(*http.Transport)
		if !ok || serverTr.MaxIdleConnsPerHost != 100 {
			t.Errorf("server default transport not tuned properly")
		}
	})

	mockHandler := &MockTuningHandler{}
	upstream := httptest.NewServer(mockHandler)
	defer upstream.Close()

	// 2. 场景二：上游响应头挂死超时中断验证 (100ms 超时)
	t.Run("Scenario2_ResponseHeaderTimeoutAbort", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "proxy-334-timeout")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		server := createServerWithTimeout(t, tmpDir, 100*time.Millisecond)

		targetURL := upstream.URL + "/slow-header"
		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()

		start := time.Now()
		server.ServeHTTP(w, req)
		elapsed := time.Since(start)

		resp := w.Result()
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 500 InternalServerError on header timeout, got %d", resp.StatusCode)
		}

		if elapsed > 400*time.Millisecond {
			t.Errorf("request did not abort promptly upon ResponseHeaderTimeout, elapsed: %v", elapsed)
		}
	})

	// 3. 场景三：大体积/分块流式传输不受 ResponseHeaderTimeout 误杀验证
	t.Run("Scenario3_StreamingBodyNotKilledByHeaderTimeout", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "proxy-334-streaming")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		server := createServerWithTimeout(t, tmpDir, 100*time.Millisecond)

		targetURL := upstream.URL + "/slow-body"
		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()

		start := time.Now()
		server.ServeHTTP(w, req)
		elapsed := time.Since(start)

		resp := w.Result()
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK for streaming body, got %d", resp.StatusCode)
		}

		bodyStr := string(body)
		if !strings.Contains(bodyStr, "chunk-0") || !strings.Contains(bodyStr, "chunk-4") {
			t.Errorf("streaming body truncated or incomplete: %s", bodyStr)
		}

		if elapsed < 200*time.Millisecond {
			t.Logf("streaming elapsed time: %v", elapsed)
		}
	})
}

func TestProxyConfig334Syntax(t *testing.T) {
	data, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	if !strings.Contains(string(data), "forward") {
		t.Errorf("proxy_test.conf missing forward directive")
	}
}