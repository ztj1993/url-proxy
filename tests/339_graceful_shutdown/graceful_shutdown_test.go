package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
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

func createTestProxy(tmpDir string) (*proxy.Server, *downloader.Manager, *config.ProxyConfig) {
	cfg := config.NewProxyConfig()
	cfg.NoCacheRules = append(cfg.NoCacheRules, config.NoCacheRule{
		Scope:   "/repo/InRelease",
		Type:    "*",
		Pattern: "*",
		Action:  "bypass",
		TTL:     5 * time.Second,
	})

	downMgr := downloader.NewManager(tmpDir, 10*time.Minute)
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	server := proxy.NewServer(cfg, tmpDir, downMgr, rw, plugins, ":0")
	return server, downMgr, cfg
}

// 1. 场景一：验证在途 HTTP 请求平稳排空且新连接立即拒绝 (HTTP Drain)
func TestGracefulShutdownHTTPDrain(t *testing.T) {
	stats := &MockShutdownStats{}
	mockUpstream := NewMockShutdownHandler(stats)
	mockUpstream.SlowDelay = 30 * time.Millisecond
	mockUpstream.SlowChunks = 6
	upstreamServer := httptest.NewServer(mockUpstream)
	defer upstreamServer.Close()

	tmpDir, err := os.MkdirTemp("", "shutdown-drain-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	proxyServer, downMgr, _ := createTestProxy(tmpDir)
	downMgr.StartWatchdog()
	defer downMgr.StopWatchdog()

	// 启动底层原生 HTTP Listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	httpSrv := &http.Server{
		Handler: proxyServer,
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = httpSrv.Serve(listener)
	}()

	proxyURL := fmt.Sprintf("http://%s", listener.Addr().String())
	slowTargetURL := fmt.Sprintf("%s/%s/slow/large-package.deb", proxyURL, upstreamServer.URL)

	// 发起在途慢速请求
	req, err := http.NewRequest("GET", slowTargetURL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("in-flight request failed to start: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// 触发优雅停机
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	shutdownErr := make(chan error, 1)
	go func() {
		shutdownErr <- httpSrv.Shutdown(shutdownCtx)
	}()

	// 验证停机指令下达后，新连接被立即拒绝
	time.Sleep(50 * time.Millisecond)
	fastTargetURL := fmt.Sprintf("%s/%s/fast/package.deb", proxyURL, upstreamServer.URL)
	_, newConnErr := http.Get(fastTargetURL)
	if newConnErr == nil {
		t.Errorf("expected new connection to be rejected after shutdown, but it succeeded")
	}

	// 验证在途请求仍能顺利、完整读取所有数据
	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("failed to drain in-flight response: %v", readErr)
	}

	expectedLen := mockUpstream.SlowChunks * mockUpstream.ChunkSize
	if len(bodyBytes) != expectedLen {
		t.Fatalf("expected in-flight body length %d, got %d", expectedLen, len(bodyBytes))
	}

	if err := <-shutdownErr; err != nil {
		t.Errorf("http server shutdown returned error: %v", err)
	}
	<-serverDone
}

// 2. 场景二：验证活跃下载任务取消与临时文件清理防污染
func TestGracefulShutdownJobCancellationAndTempFileCleanup(t *testing.T) {
	stats := &MockShutdownStats{}
	mockUpstream := NewMockShutdownHandler(stats)
	mockUpstream.SlowDelay = 100 * time.Millisecond
	mockUpstream.SlowChunks = 10
	upstreamServer := httptest.NewServer(mockUpstream)
	defer upstreamServer.Close()

	tmpDir, err := os.MkdirTemp("", "shutdown-cancel-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	proxyServer, downMgr, _ := createTestProxy(tmpDir)
	downMgr.StartWatchdog()

	httpSrv := httptest.NewServer(proxyServer)
	defer httpSrv.Close()

	slowTargetURL := fmt.Sprintf("%s/%s/slow/large-package.deb", httpSrv.URL, upstreamServer.URL)

	// 启动协程拉取大文件
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, getErr := http.Get(slowTargetURL)
		if getErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()

	// 等待上游开始推流并产生临时文件
	select {
	case <-mockUpstream.StartedSlow:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slow download to start")
	}

	time.Sleep(100 * time.Millisecond)

	// 触发任务管理器停机与任务主动取消
	downMgr.Shutdown()
	wg.Wait()

	// 验证临时文件是否被完全删除，且未被重命名为最终缓存文件
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read cache dir: %v", err)
	}

	for _, entry := range entries {
		if cache.IsTempCacheFile(entry.Name()) {
			t.Errorf("found orphan temp file after shutdown: %s", entry.Name())
		}
	}

	finalFile := filepath.Join(tmpDir, "slow", "large-package.deb")
	if _, err := os.Stat(finalFile); !os.IsNotExist(err) {
		t.Errorf("corrupted partial file was mistakenly renamed to final cache file: %s", finalFile)
	}
}

// 3. 场景三：验证 CONNECT 劫持隧道主动切断与出向连接池释放
func TestGracefulShutdownConnectTunnelCleanup(t *testing.T) {
	// 创建模拟上游 TCP Echo 服务
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start echo listener: %v", err)
	}
	defer echoListener.Close()

	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		for {
			conn, acceptErr := echoListener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, rErr := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if rErr != nil {
						return
					}
				}
			}(conn)
		}
	}()

	tmpDir, err := os.MkdirTemp("", "shutdown-tunnel-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	proxyServer, _, _ := createTestProxy(tmpDir)
	proxyHttpServer := httptest.NewServer(proxyServer)
	defer proxyHttpServer.Close()

	// 建立 CONNECT 隧道
	proxyAddr := proxyHttpServer.Listener.Addr().String()
	destAddr := echoListener.Addr().String()

	clientConn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer clientConn.Close()

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", destAddr, destAddr)
	if _, err := clientConn.Write([]byte(req)); err != nil {
		t.Fatalf("failed to send CONNECT request: %v", err)
	}

	br := bufio.NewReader(clientConn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("failed to read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 Connection Established, got %d", resp.StatusCode)
	}

	// 验证隧道连通性
	testMsg := "PING-PONG-TUNNEL-DATA\n"
	if _, err := clientConn.Write([]byte(testMsg)); err != nil {
		t.Fatalf("failed to write to tunnel: %v", err)
	}
	echoReply, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read from tunnel: %v", err)
	}
	if echoReply != testMsg {
		t.Fatalf("expected %q, got %q", testMsg, echoReply)
	}

	// 触发代理服务器 Close()，主动回收活跃 CONNECT 隧道
	proxyServer.Close()

	// 验证客户端长连接被主动关闭
	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	readBuf := make([]byte, 128)
	_, readAfterCloseErr := clientConn.Read(readBuf)
	if readAfterCloseErr == nil {
		t.Errorf("expected read to fail after proxyServer.Close(), but read succeeded")
	}
}

// 4. 场景四：验证任务管理器与看门狗生命周期操作幂等性
func TestGracefulShutdownManagerIdempotence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shutdown-idempotence-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	downMgr := downloader.NewManager(tmpDir, 50*time.Millisecond)
	downMgr.StartWatchdog()

	// 重复调用 StopWatchdog 不应触发 panic
	downMgr.StopWatchdog()
	downMgr.StopWatchdog()

	// 重复调用 Shutdown 不应触发 panic
	downMgr.Shutdown()
	downMgr.Shutdown()
}
