package main

import (
	"crypto/sha256"
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

func setupTestProxy(t *testing.T) (*proxy.Server, string, func()) {
	tmpDir, err := os.MkdirTemp("", "proxy-331-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cfg := config.NewProxyConfig()
	downMgr := downloader.NewManager(tmpDir, time.Minute)
	ttlMgr := ttl.NewManager()
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := proxy.NewServer(cfg, tmpDir, downMgr, ttlMgr, rw, plugins, ":8888")

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	return server, tmpDir, cleanup
}

func countFilesInDir(dir string) (int, []string) {
	var files []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return len(files), files
}

func TestNon200AntiPollution(t *testing.T) {
	stats := &MockServerStats{}
	var recoverableFail atomic.Bool
	recoverableFail.Store(true)

	mockHandler := NewMockHandler(stats, &recoverableFail)
	upstream := httptest.NewServer(mockHandler)
	defer upstream.Close()

	// 1. 场景一：404 响应零落盘与纯流式透传验证
	t.Run("Scenario1_NotFound_ZeroDiskPersistence", func(t *testing.T) {
		server, cacheDir, cleanup := setupTestProxy(t)
		defer cleanup()

		targetURL := upstream.URL + "/repo/not-found.deb"
		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", resp.StatusCode)
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if string(bodyBytes) != NotFoundPayload {
			t.Fatalf("body mismatch: expected %q, got %q", NotFoundPayload, string(bodyBytes))
		}

		// 检查本地缓存目录：严禁创建任何制品文件与临时文件
		fileCount, fileList := countFilesInDir(cacheDir)
		if fileCount != 0 {
			t.Fatalf("expected 0 files in cache dir, got %d: %v", fileCount, fileList)
		}
	})

	// 2. 场景二：500/502 服务端故障零落盘与源站恢复验证
	t.Run("Scenario2_ServerFault_Recovery", func(t *testing.T) {
		server, cacheDir, cleanup := setupTestProxy(t)
		defer cleanup()

		targetURL := upstream.URL + "/repo/recoverable.deb"

		// 2.1 模拟源站 500 故障
		recoverableFail.Store(true)
		reqFail := httptest.NewRequest("GET", "/"+targetURL, nil)
		wFail := httptest.NewRecorder()
		server.ServeHTTP(wFail, reqFail)

		respFail := wFail.Result()
		if respFail.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500 Internal Server Error, got %d", respFail.StatusCode)
		}
		bodyFail, _ := io.ReadAll(respFail.Body)
		_ = respFail.Body.Close()
		if string(bodyFail) != ServerErrorPayload {
			t.Fatalf("500 body mismatch: got %q", string(bodyFail))
		}

		// 验证 500 期间零文件落盘
		fileCount, fileList := countFilesInDir(cacheDir)
		if fileCount != 0 {
			t.Fatalf("expected 0 files after 500 error, got %d: %v", fileCount, fileList)
		}

		// 2.2 模拟源站恢复为 200 OK
		recoverableFail.Store(false)
		reqOk := httptest.NewRequest("GET", "/"+targetURL, nil)
		wOk := httptest.NewRecorder()
		server.ServeHTTP(wOk, reqOk)

		respOk := wOk.Result()
		if respOk.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK after origin recovery, got %d", respOk.StatusCode)
		}
		bodyOk, _ := io.ReadAll(respOk.Body)
		_ = respOk.Body.Close()
		if string(bodyOk) != ValidDebPayload {
			t.Fatalf("recovered 200 body mismatch: got %q", string(bodyOk))
		}

		// 验证恢复后制品已正确持久化缓存
		cachedPath := filepath.Join(cacheDir, cache.GetSafeUrlPath(targetURL))
		cachedData, err := os.ReadFile(cachedPath)
		if err != nil {
			t.Fatalf("expected cached file to exist at %s: %v", cachedPath, err)
		}
		if string(cachedData) != ValidDebPayload {
			t.Fatalf("cached file content mismatch: got %q", string(cachedData))
		}

		// 2.3 验证后续请求直接命中本地缓存
		reqCached := httptest.NewRequest("GET", "/"+targetURL, nil)
		wCached := httptest.NewRecorder()
		server.ServeHTTP(wCached, reqCached)
		respCached := wCached.Result()
		if respCached.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK from local cache, got %d", respCached.StatusCode)
		}
		bodyCached, _ := io.ReadAll(respCached.Body)
		_ = respCached.Body.Close()
		if string(bodyCached) != ValidDebPayload {
			t.Fatalf("cached response mismatch")
		}
	})

	// 3. 场景三：200 OK 正常制品下载与落盘无回归验证
	t.Run("Scenario3_ValidArtifact_NormalCaching", func(t *testing.T) {
		server, cacheDir, cleanup := setupTestProxy(t)
		defer cleanup()

		targetURL := upstream.URL + "/repo/valid-pkg.deb"
		req := httptest.NewRequest("GET", "/"+targetURL, nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		expectedHash := sha256.Sum256([]byte(ValidDebPayload))
		actualHash := sha256.Sum256(bodyBytes)
		if expectedHash != actualHash {
			t.Fatalf("checksum mismatch: expected %x, got %x", expectedHash, actualHash)
		}

		cachedPath := filepath.Join(cacheDir, cache.GetSafeUrlPath(targetURL))
		cachedData, err := os.ReadFile(cachedPath)
		if err != nil {
			t.Fatalf("expected cached file %s to exist: %v", cachedPath, err)
		}
		if string(cachedData) != ValidDebPayload {
			t.Fatalf("cached file content mismatch")
		}

		// 确保没有残留 .tmp 临时文件
		_, files := countFilesInDir(cacheDir)
		for _, f := range files {
			if strings.Contains(filepath.Base(f), ".tmp") {
				t.Errorf("found lingering temp file: %s", f)
			}
		}
	})

	// 4. 场景四：并发请求下的错误透传安全性验证 (10 并发)
	t.Run("Scenario4_Concurrent404_Safety", func(t *testing.T) {
		server, cacheDir, cleanup := setupTestProxy(t)
		defer cleanup()

		targetURL := upstream.URL + "/repo/not-found.deb"
		concurrency := 10
		var wg sync.WaitGroup
		wg.Add(concurrency)

		statusCodes := make([]int, concurrency)
		bodyLengths := make([]int, concurrency)
		errCh := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/"+targetURL, nil)
				w := httptest.NewRecorder()

				server.ServeHTTP(w, req)
				resp := w.Result()
				statusCodes[idx] = resp.StatusCode

				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				bodyLengths[idx] = len(body)

				if resp.StatusCode != http.StatusNotFound {
					errCh <- fmt.Errorf("routine %d expected 404, got %d", idx, resp.StatusCode)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("concurrent error: %v", err)
		}

		hasBodyCount := 0
		for i, code := range statusCodes {
			if code != http.StatusNotFound {
				t.Errorf("worker %d returned status %d, expected 404", i, code)
			}
			if bodyLengths[i] > 0 {
				hasBodyCount++
			}
		}

		if hasBodyCount < 1 {
			t.Errorf("expected at least 1 request (Leader) to receive 404 body, got %d", hasBodyCount)
		}

		// 检查磁盘：高并发 404 下零落盘，无残留临时文件
		fileCount, fileList := countFilesInDir(cacheDir)
		if fileCount != 0 {
			t.Fatalf("expected 0 files after concurrent 404 requests, got %d: %v", fileCount, fileList)
		}
	})
}

func TestProxyConfig331Syntax(t *testing.T) {
	data, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "forward") {
		t.Errorf("proxy_test.conf missing forward directive")
	}
}