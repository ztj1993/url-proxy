package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/rewriter"
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
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")

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
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")

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

func TestProxy_Non200NoCachePersistence(t *testing.T) {
	var serverFail atomic.Bool
	serverFail.Store(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repo/not-found.deb" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<html>404 Not Found</html>")
			return
		}
		if r.URL.Path == "/repo/pkg.deb" {
			if serverFail.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, "500 Internal Server Error")
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "valid-deb-package-content")
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	tmpDir, err := os.MkdirTemp("", "proxy-non200-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.NewProxyConfig()
	downMgr := downloader.NewManager(tmpDir, time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()
	server := NewServer(cfg, tmpDir, downMgr, rw, plugins, ":8888")

	// 1. 请求 404 资源：透传 404，绝不创建文件
	req404 := httptest.NewRequest("GET", "/"+upstream.URL+"/repo/not-found.deb", nil)
	w404 := httptest.NewRecorder()
	server.ServeHTTP(w404, req404)

	resp404 := w404.Result()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", resp404.StatusCode)
	}
	body404 := w404.Body.String()
	if body404 != "<html>404 Not Found</html>" {
		t.Fatalf("expected 404 body, got '%s'", body404)
	}

	// 验证磁盘无任何文件残留
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Fatalf("expected 0 files in cache dir after 404, got %d", len(entries))
	}

	// 2. 请求 500 资源：透传 500，绝不创建文件
	req500 := httptest.NewRequest("GET", "/"+upstream.URL+"/repo/pkg.deb", nil)
	w500 := httptest.NewRecorder()
	server.ServeHTTP(w500, req500)

	resp500 := w500.Result()
	if resp500.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d", resp500.StatusCode)
	}
	body500 := w500.Body.String()
	if body500 != "500 Internal Server Error" {
		t.Fatalf("expected 500 body, got '%s'", body500)
	}

	entries, _ = os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Fatalf("expected 0 files in cache dir after 500, got %d", len(entries))
	}

	// 3. 上游恢复 200 OK：立即可以正常下载并落盘
	serverFail.Store(false)
	req200 := httptest.NewRequest("GET", "/"+upstream.URL+"/repo/pkg.deb", nil)
	w200 := httptest.NewRecorder()
	server.ServeHTTP(w200, req200)

	resp200 := w200.Result()
	if resp200.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after recovery, got %d", resp200.StatusCode)
	}
	body200 := w200.Body.String()
	if body200 != "valid-deb-package-content" {
		t.Fatalf("expected valid body after recovery, got '%s'", body200)
	}

	// 验证磁盘已正常持久化缓存
	cachedPath := filepath.Join(tmpDir, cache.GetSafeUrlPath(upstream.URL+"/repo/pkg.deb"))
	data, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatalf("expected cached file %s to exist: %v", cachedPath, err)
	}
	if string(data) != "valid-deb-package-content" {
		t.Fatalf("cached file content mismatch: '%s'", string(data))
	}
}