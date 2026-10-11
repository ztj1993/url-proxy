package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSoftTTLMockEndpoints(t *testing.T) {
	stats := &MockServerStats{}
	pypiContent := PyPIVersion1
	pypiEtag := `"pypi-v1.0"`
	mockHandler := NewMockHandler(stats, &pypiContent, &pypiEtag)
	server := httptest.NewServer(mockHandler)
	defer server.Close()

	client := server.Client()

	// 1. PyPI Simple 端点测试 (200 响应与 304 条件协商)
	t.Run("PyPI_Endpoint_And_304", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/pypi/simple/pkg/")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("PyPI get failed: %v, status: %d", err, resp.StatusCode)
		}
		etag := resp.Header.Get("ETag")
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != PyPIVersion1 {
			t.Errorf("PyPI body mismatch: %s", string(body))
		}

		// 发起条件请求
		req304, _ := http.NewRequest("GET", server.URL+"/pypi/simple/pkg/", nil)
		req304.Header.Set("If-None-Match", etag)
		resp304, err := client.Do(req304)
		if err != nil || resp304.StatusCode != http.StatusNotModified {
			t.Errorf("PyPI 304 revalidation failed: %v, status: %d", err, resp304.StatusCode)
		}
		_ = resp304.Body.Close()

		if hits := atomic.LoadUint64(&stats.PypiHits); hits != 2 {
			t.Errorf("expected 2 pypi hits, got %d", hits)
		}
	})

	// 2. npm 端点测试
	t.Run("Npm_Endpoint_And_304", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/npm/express")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("npm get failed: %v, status: %d", err, resp.StatusCode)
		}
		etag := resp.Header.Get("ETag")
		_ = resp.Body.Close()

		req304, _ := http.NewRequest("GET", server.URL+"/npm/express", nil)
		req304.Header.Set("If-None-Match", etag)
		resp304, err := client.Do(req304)
		if err != nil || resp304.StatusCode != http.StatusNotModified {
			t.Errorf("npm 304 failed: %v, status: %d", err, resp304.StatusCode)
		}
		_ = resp304.Body.Close()

		if hits := atomic.LoadUint64(&stats.NpmHits); hits != 2 {
			t.Errorf("expected 2 npm hits, got %d", hits)
		}
	})

	// 3. APT InRelease 测试
	t.Run("Apt_InRelease_And_Packages", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/apt/InRelease")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("apt InRelease failed: %v, status: %d", err, resp.StatusCode)
		}
		_ = resp.Body.Close()

		respPkg, err := client.Get(server.URL + "/apt/Packages")
		if err != nil || respPkg.StatusCode != http.StatusOK {
			t.Fatalf("apt Packages failed: %v, status: %d", err, respPkg.StatusCode)
		}
		_ = respPkg.Body.Close()

		if hits := atomic.LoadUint64(&stats.AptInReleaseHits); hits != 1 {
			t.Errorf("expected 1 inrelease hit, got %d", hits)
		}
		if hits := atomic.LoadUint64(&stats.AptPackagesHits); hits != 1 {
			t.Errorf("expected 1 packages hit, got %d", hits)
		}
	})

	// 4. Stats 校验
	t.Run("Stats_Verification", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/stats")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("stats failed: %v, status: %d", err, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s := string(body)
		if !strings.Contains(s, "pypi: 2") || !strings.Contains(s, "npm: 2") || !strings.Contains(s, "apt_inrelease: 1") || !strings.Contains(s, "apt_packages: 1") {
			t.Errorf("unexpected stats output: %s", s)
		}
	})
}

func TestProxyConfigTTLModifierSyntax(t *testing.T) {
	data, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	ttlRulesCount := 0
	normalBypassCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "bypass") {
			if strings.Contains(trimmed, "ttl:") {
				ttlRulesCount++
			} else {
				normalBypassCount++
			}
		}
	}

	if ttlRulesCount < 3 {
		t.Errorf("expected at least 3 bypass rules with ttl:, got %d", ttlRulesCount)
	}
	if normalBypassCount < 1 {
		t.Errorf("expected at least 1 normal bypass rule, got %d", normalBypassCount)
	}
}
