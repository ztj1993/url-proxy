package proxy

import (
	"fmt"
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
	"uri-proxy/internal/rewriter"
	"uri-proxy/internal/ttl"
)

func TestProxy_AliasRewriteAndServe(t *testing.T) {
	// 启动模拟上游服务器
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ubuntu/dists/Release" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "Origin: Ubuntu")
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.NewProxyConfig()
	cfg.Aliases["ubuntu"] = upstream.URL + "/ubuntu"
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/ubuntu/",
		Action:  "bypass",
		Type:    "*",
		Pattern: "*",
	})

	downMgr := downloader.NewManager(tmpDir, time.Minute)
	ttlMgr := ttl.NewManager()
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := NewServer(cfg, tmpDir, downMgr, ttlMgr, rw, plugins, ":8888")

	req := httptest.NewRequest("GET", "/ubuntu/dists/Release", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	body := w.Body.String()
	if body != "Origin: Ubuntu" {
		t.Errorf("expected 'Origin: Ubuntu', got '%s'", body)
	}
}

func TestProxy_LocalRangeServe(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "proxy-range-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	uri := "http://example.com/cached.dat"
	cachedPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(uri))
	_ = os.MkdirAll(filepath.Dir(cachedPath), 0755)
	_ = os.WriteFile(cachedPath, []byte("0123456789"), 0644)

	cfg := config.NewProxyConfig()
	downMgr := downloader.NewManager(tmpDir, time.Minute)
	ttlMgr := ttl.NewManager()
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := NewServer(cfg, tmpDir, downMgr, ttlMgr, rw, plugins, ":8888")

	// Range request: bytes=0-3
	req := httptest.NewRequest("GET", "/"+uri, nil)
	req.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", resp.StatusCode)
	}
	body := w.Body.String()
	if body != "0123" {
		t.Errorf("expected '0123', got '%s'", body)
	}
}
