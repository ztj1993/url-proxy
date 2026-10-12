package cache

import (
	"strings"
	"testing"

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
	if ShouldBypassCache(uri, rules) {
		t.Errorf("expected whitelist rule to take priority and not bypass cache")
	}
}
