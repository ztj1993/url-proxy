package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uri-proxy/internal/config"
)

func TestGetSafeUrlPath(t *testing.T) {
	uri := "https://archive.ubuntu.com:8080/ubuntu/dists/jammy/Release?q=1"
	safePath := GetSafeUrlPath(uri)

	if strings.Contains(safePath, ":") {
		t.Errorf("safePath should not contain colon: %s", safePath)
	}
	if !strings.Contains(safePath, "https-archive.ubuntu.com-8080") {
		t.Errorf("safePath missing sanitized host prefix: %s", safePath)
	}
	if !strings.Contains(safePath, "Release%3Fq%3D1") {
		t.Errorf("safePath missing query escaped file: %s", safePath)
	}
}

func TestBypassAndWhitelistPriority(t *testing.T) {
	rules := []config.NoCacheRule{
		{
			Scope:   "/dists/",
			Action:  "bypass",
			Type:    "suffix",
			Pattern: ".deb",
		},
		{
			Scope:   "/dists/",
			Action:  "cache", // 白名单强制落盘
			Type:    "suffix",
			Pattern: ".deb",
		},
	}

	uri := "http://archive.ubuntu.com/dists/jammy/main/binary-amd64/Packages.deb"
	if MatchBypassRule(uri, rules) != nil {
		t.Errorf("expected whitelist rule to take priority and not bypass cache")
	}
}

func TestIsTempCacheFile(t *testing.T) {
	cases := []struct {
		name     string
		expected bool
	}{
		{"curl.deb.1728794857123456789", true},
		{"pkg.tar.gz.1700000000000000000", true},
		{"lib.so.1", false},
		{"curl_7.81.0-1.deb", false},
		{"node-v20.11.0.tar.gz", false},
		{"Packages.bz2", false},
		{".1728794857123456789", false}, // 无主文件名
		{"file.123456", false},           // 时间戳过短
		{"file.abc172879485712345678", false}, // 含非数字
	}

	for _, c := range cases {
		got := IsTempCacheFile(c.name)
		if got != c.expected {
			t.Errorf("IsTempCacheFile(%q) = %v, want %v", c.name, got, c.expected)
		}
	}
}

func TestCleanOrphanTempFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cache-clean-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 创建过期孤儿临时文件 (修改时间设为 2 小时前)
	staleFile := filepath.Join(tmpDir, "stale.deb.1700000000000000000")
	_ = os.WriteFile(staleFile, []byte("stale"), 0644)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(staleFile, twoHoursAgo, twoHoursAgo)

	// 2. 创建近期活跃临时文件 (修改时间设为当前时刻)
	activeFile := filepath.Join(tmpDir, "active.deb.1728794857123456789")
	_ = os.WriteFile(activeFile, []byte("active"), 0644)

	// 3. 创建正常合法缓存文件
	validFile := filepath.Join(tmpDir, "valid.deb")
	_ = os.WriteFile(validFile, []byte("valid"), 0644)

	// 执行清理 (maxAge = 1小时)
	removed, err := CleanOrphanTempFiles(tmpDir, 1*time.Hour)
	if err != nil {
		t.Fatalf("CleanOrphanTempFiles error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 file removed, got %d", removed)
	}

	// 验证 staleFile 已被删除
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("stale file should be deleted")
	}

	// 验证 activeFile 与 validFile 完好无损
	if _, err := os.Stat(activeFile); err != nil {
		t.Errorf("active file should remain untouched")
	}
	if _, err := os.Stat(validFile); err != nil {
		t.Errorf("valid cache file should remain untouched")
	}
}