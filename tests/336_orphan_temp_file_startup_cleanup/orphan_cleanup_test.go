package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uri-proxy/internal/cache"
)

func TestOrphanTempFileCleanup(t *testing.T) {
	// 1. 场景一：临时文件名特征精准识别验证
	t.Run("Scenario1_TempFilePatternRecognition", func(t *testing.T) {
		tempCases := []string{
			"curl.deb.1728794857123456789",
			"test.whl.1700000000000000000",
			"ubuntu/pool/main/c/curl_7.81.0.deb.1728794857123456789",
			"image.tar.1791896309002385930",
		}
		for _, name := range tempCases {
			base := filepath.Base(name)
			if !cache.IsTempCacheFile(base) {
				t.Errorf("expected %q to be recognized as temp file", name)
			}
		}

		normalCases := []string{
			"curl_7.81.0-1.deb",
			"libssl.so.1.1",
			"Packages.gz",
			"Release.gpg",
			"sha256.12345",
			"node-v20.11.0.tar.gz",
			".1728794857123456789", // 无主文件名
			"file.123",             // 后缀过短
		}
		for _, name := range normalCases {
			base := filepath.Base(name)
			if cache.IsTempCacheFile(base) {
				t.Errorf("expected %q to NOT be recognized as temp file", name)
			}
		}
	})

	// 2. 场景二：过期孤儿临时文件自动清除验证
	t.Run("Scenario2_StaleOrphanCleanup", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "orphan-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		nestedDir := filepath.Join(tmpDir, "nested", "sub")
		_ = os.MkdirAll(nestedDir, 0755)

		// 2.1 创建 2 小时前的根目录过期孤儿文件
		staleFile1 := filepath.Join(tmpDir, "test.whl.1700000000000000000")
		_ = os.WriteFile(staleFile1, []byte("stale1"), 0644)
		twoHoursAgo := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(staleFile1, twoHoursAgo, twoHoursAgo)

		// 2.2 创建 3 小时前的嵌套目录过期孤儿文件
		staleFile2 := filepath.Join(nestedDir, "curl.deb.1700000000000000001")
		_ = os.WriteFile(staleFile2, []byte("stale2"), 0644)
		threeHoursAgo := time.Now().Add(-3 * time.Hour)
		_ = os.Chtimes(staleFile2, threeHoursAgo, threeHoursAgo)

		// 2.3 创建当前时刻的活跃临时文件
		activeFile := filepath.Join(tmpDir, "active.whl.1728794857123456789")
		_ = os.WriteFile(activeFile, []byte("active"), 0644)

		// 2.4 创建合法普通缓存制品文件
		validFile1 := filepath.Join(tmpDir, "valid.whl")
		_ = os.WriteFile(validFile1, []byte("valid1"), 0644)
		validFile2 := filepath.Join(nestedDir, "valid.deb")
		_ = os.WriteFile(validFile2, []byte("valid2"), 0644)

		// 执行清理 (maxAge = 1小时)
		removed, err := cache.CleanOrphanTempFiles(tmpDir, 1*time.Hour)
		if err != nil {
			t.Fatalf("CleanOrphanTempFiles failed: %v", err)
		}
		if removed != 2 {
			t.Errorf("expected 2 stale files removed, got %d", removed)
		}

		// 验证过期孤儿文件已彻底删除
		if _, err := os.Stat(staleFile1); !os.IsNotExist(err) {
			t.Errorf("staleFile1 should be deleted")
		}
		if _, err := os.Stat(staleFile2); !os.IsNotExist(err) {
			t.Errorf("staleFile2 should be deleted")
		}

		// 验证活跃临时文件与正常缓存文件完好无损
		if _, err := os.Stat(activeFile); err != nil {
			t.Errorf("activeFile should remain intact")
		}
		if _, err := os.Stat(validFile1); err != nil {
			t.Errorf("validFile1 should remain intact")
		}
		if _, err := os.Stat(validFile2); err != nil {
			t.Errorf("validFile2 should remain intact")
		}
	})
}

func TestProxyConfig336Syntax(t *testing.T) {
	data, err := os.ReadFile("proxy_test.conf")
	if err != nil {
		t.Fatalf("failed to read proxy_test.conf: %v", err)
	}

	if !strings.Contains(string(data), "forward") {
		t.Errorf("proxy_test.conf missing forward directive")
	}
}