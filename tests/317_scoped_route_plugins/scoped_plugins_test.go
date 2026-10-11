package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestScopedRoutePlugins(t *testing.T) {
	stats := &MockServerStats{}
	mockHandler := NewMockHandler(stats)
	server := httptest.NewServer(mockHandler)
	defer server.Close()

	client := server.Client()

	// 1. 正向测试: 命中挂载了 plugin:docker 的路由
	t.Run("Positive_DockerPluginEndpoints", func(t *testing.T) {
		// 1.1 /v2/ 握手 401 质询
		respProbe, err := client.Get(server.URL + "/docker/v2/")
		if err != nil || respProbe.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Docker probe failed, expected 401, got %v, status: %d", err, respProbe.StatusCode)
		}
		wwwAuth := respProbe.Header.Get("Www-Authenticate")
		if !strings.Contains(wwwAuth, "Bearer realm=") {
			t.Errorf("Docker probe missing Bearer realm in header: %s", wwwAuth)
		}
		_ = respProbe.Body.Close()

		// 1.2 Token 接口
		respToken, err := client.Get(server.URL + "/docker/v2/token")
		if err != nil || respToken.StatusCode != http.StatusOK {
			t.Fatalf("Docker token request failed: %v, status: %d", err, respToken.StatusCode)
		}
		_ = respToken.Body.Close()

		// 1.3 Blob HEAD 探测
		blobURL := server.URL + "/docker/v2/library/test/blobs/" + TestDockerBlobDigest
		respHead, err := client.Head(blobURL)
		if err != nil || respHead.StatusCode != http.StatusOK {
			t.Fatalf("Docker blob HEAD failed: %v, status: %d", err, respHead.StatusCode)
		}
		if respHead.Header.Get("Accept-Ranges") != "bytes" {
			t.Errorf("Docker blob HEAD missing Accept-Ranges: bytes")
		}
		_ = respHead.Body.Close()

		// 1.4 Blob GET 获取
		respBlob, err := client.Get(blobURL)
		if err != nil || respBlob.StatusCode != http.StatusOK {
			t.Fatalf("Docker blob GET failed: %v, status: %d", err, respBlob.StatusCode)
		}
		blobData, _ := io.ReadAll(respBlob.Body)
		_ = respBlob.Body.Close()
		if string(blobData) != TestDockerBlobPayload {
			t.Errorf("Docker blob payload mismatch: got %q, want %q", string(blobData), TestDockerBlobPayload)
		}
	})

	// 2. 负向隔离测试: 未挂载 plugin:docker 的普通路由，即使包含 /blobs/sha256: 路径，也绝不走 Docker 插件
	t.Run("Negative_NonDockerScopeIsolation", func(t *testing.T) {
		plainBlobURL := server.URL + "/plain/repo/blobs/" + TestDockerBlobDigest
		respPlain, err := client.Get(plainBlobURL)
		if err != nil || respPlain.StatusCode != http.StatusOK {
			t.Fatalf("Plain blob request failed: %v, status: %d", err, respPlain.StatusCode)
		}
		plainData, _ := io.ReadAll(respPlain.Body)
		_ = respPlain.Body.Close()

		// 必须是 plain 上游返回的内容，绝不能被 Docker 插件误拦截直出
		if string(plainData) != TestPlainBlobPayload {
			t.Errorf("Plain blob returned unexpected payload (isolation violated): %q", string(plainData))
		}
	})

	// 3. Stats 校验: 确保上游计数完全符合预期
	t.Run("Stats_Verification", func(t *testing.T) {
		respStats, err := client.Get(server.URL + "/stats")
		if err != nil || respStats.StatusCode != http.StatusOK {
			t.Fatalf("Stats request failed: %v, status: %d", err, respStats.StatusCode)
		}
		statsBody, _ := io.ReadAll(respStats.Body)
		_ = respStats.Body.Close()
		statsStr := string(statsBody)

		if !strings.Contains(statsStr, "docker_probe: 1") ||
			!strings.Contains(statsStr, "docker_token: 1") ||
			!strings.Contains(statsStr, "docker_blob: 2") ||
			!strings.Contains(statsStr, "plain_blob: 1") {
			t.Errorf("Stats counts mismatch: %s", statsStr)
		}
	})
}

func TestProxyConfigDirectiveSyntax(t *testing.T) {
	// 读取 proxy_test.conf 并校验语法解析
	confData, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	lines := strings.Split(string(confData), "\n")
	aliasWithPlugin := 0
	aliasWithoutPlugin := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "alias") {
			if strings.Contains(trimmed, "plugin:docker") {
				aliasWithPlugin++
			} else {
				aliasWithoutPlugin++
			}
		}
	}

	if aliasWithPlugin < 2 {
		t.Errorf("expected at least 2 alias with plugin:docker, got %d", aliasWithPlugin)
	}
	if aliasWithoutPlugin < 2 {
		t.Errorf("expected at least 2 alias without plugin, got %d", aliasWithoutPlugin)
	}
}
