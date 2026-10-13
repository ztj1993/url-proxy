package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
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
	"uri-proxy/internal/ttl"
)

func createTunnelProxyServer(tunnelTimeout time.Duration) (*proxy.Server, *httptest.Server) {
	cfg := config.NewProxyConfig()
	downMgr := downloader.NewManager(os.TempDir(), time.Minute)
	ttlMgr := ttl.NewManager()
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	proxyServer := proxy.NewServer(cfg, os.TempDir(), downMgr, ttlMgr, rw, plugins, ":8888")
	if tunnelTimeout > 0 {
		proxyServer.SetTunnelTimeout(tunnelTimeout)
	}

	httpServer := httptest.NewServer(proxyServer)
	return proxyServer, httpServer
}

func establishConnectTunnel(proxyAddr, targetAddr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, err
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return conn, nil
}

func TestConnectTunnel(t *testing.T) {
	target, err := StartMockTunnelTarget()
	if err != nil {
		t.Fatalf("failed to start mock target: %v", err)
	}
	defer target.Close()

	targetAddr := target.Listener.Addr().String()

	// 1. 场景一：静默僵尸隧道超时自动回收验证 (100ms 超时)
	t.Run("Scenario1_SilentZombieTunnelReclamation", func(t *testing.T) {
		_, proxyServer := createTunnelProxyServer(100 * time.Millisecond)
		defer proxyServer.Close()

		proxyAddr := proxyServer.Listener.Addr().String()
		conn, err := establishConnectTunnel(proxyAddr, targetAddr)
		if err != nil {
			t.Fatalf("establish tunnel failed: %v", err)
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		start := time.Now()
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, readErr := conn.Read(buf)
		elapsed := time.Since(start)

		if readErr == nil {
			t.Errorf("expected tunnel to be closed by idle timeout, but read succeeded")
		}
		if elapsed > 300*time.Millisecond {
			t.Errorf("idle timeout took too long to trigger: %v", elapsed)
		}
	})

	// 2. 场景二：活跃传输持续续期验证 (150ms 超时，连续每 50ms 发送一次，持续 400ms)
	t.Run("Scenario2_ActiveTransferDeadlineRenewal", func(t *testing.T) {
		_, proxyServer := createTunnelProxyServer(150 * time.Millisecond)
		defer proxyServer.Close()

		proxyAddr := proxyServer.Listener.Addr().String()
		conn, err := establishConnectTunnel(proxyAddr, targetAddr)
		if err != nil {
			t.Fatalf("establish tunnel failed: %v", err)
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		for i := 0; i < 8; i++ {
			time.Sleep(50 * time.Millisecond)
			msg := fmt.Sprintf("ping-%d", i)
			if _, wErr := conn.Write([]byte(msg)); wErr != nil {
				t.Fatalf("write failed at iteration %d: %v", i, wErr)
			}
			_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, rErr := conn.Read(buf)
			if rErr != nil {
				t.Fatalf("read failed at iteration %d: %v", i, rErr)
			}
			if string(buf[:n]) != msg {
				t.Errorf("echo mismatch: got %s, want %s", string(buf[:n]), msg)
			}
		}
	})

	// 3. 场景三：单向半关闭（Half-Close）数据完整性验证
	t.Run("Scenario3_HalfCloseDataIntegrity", func(t *testing.T) {
		_, proxyServer := createTunnelProxyServer(1 * time.Second)
		defer proxyServer.Close()

		proxyAddr := proxyServer.Listener.Addr().String()
		conn, err := establishConnectTunnel(proxyAddr, targetAddr)
		if err != nil {
			t.Fatalf("establish tunnel failed: %v", err)
		}
		defer conn.Close()

		tcpConn, ok := conn.(*net.TCPConn)
		if !ok {
			t.Fatalf("expected *net.TCPConn, got %T", conn)
		}

		if _, wErr := tcpConn.Write([]byte("HALF-CLOSE-TEST")); wErr != nil {
			t.Fatalf("write failed: %v", wErr)
		}

		if err := tcpConn.CloseWrite(); err != nil {
			t.Fatalf("CloseWrite failed: %v", err)
		}

		respData, rErr := io.ReadAll(tcpConn)
		if rErr != nil {
			t.Fatalf("read after CloseWrite failed: %v", rErr)
		}
		if string(respData) != "HALF-CLOSE-RESPONSE" {
			t.Errorf("expected 'HALF-CLOSE-RESPONSE', got %q", string(respData))
		}
	})
}

func TestProxyConfig335Syntax(t *testing.T) {
	data, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	if !strings.Contains(string(data), "forward") {
		t.Errorf("proxy_test.conf missing forward directive")
	}
}