package cache

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"uri-proxy/internal/config"
)

// GetSafeUrlPath 根据 URL 生成安全的本地缓存相对路径
// 规则: 协议-域名-端口/请求路径/url编码后的(文件名?参数)
func GetSafeUrlPath(rawUri string) string {
	u, err := url.Parse(rawUri)
	if err != nil {
		return url.QueryEscape(rawUri)
	}

	// 1. 协议-域名-端口 (替换 : 为 - 防止 Windows 路径错误)
	hostStr := u.Host
	hostStr = strings.ReplaceAll(hostStr, ":", "-")

	schemeHostDir := u.Scheme + "-" + hostStr
	if u.Scheme == "" {
		schemeHostDir = hostStr
	}

	// 2. 请求路径
	pDir := path.Dir(u.Path)
	pBase := path.Base(u.Path)

	if strings.HasSuffix(u.Path, "/") {
		pDir = u.Path
		pBase = "index"
	}

	if pDir == "/" || pDir == "." {
		pDir = ""
	}
	if pBase == "/" || pBase == "." {
		pBase = "index"
	}

	// 3. 文件和参数拼接后编码当文件
	fileAndQuery := pBase
	if u.RawQuery != "" {
		fileAndQuery += "?" + u.RawQuery
	}

	encodedFileName := url.QueryEscape(fileAndQuery)
	return filepath.Join(schemeHostDir, filepath.FromSlash(pDir), encodedFileName)
}

// EnsureCacheDir 校验并创建缓存根目录，若未配置则回退到系统临时目录
func EnsureCacheDir(customDir string) (string, error) {
	dir := customDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// MatchBypassRule 按照规则链匹配免缓存判定 (Feature 305)
func MatchBypassRule(rawUri string, rules []config.NoCacheRule, clientPaths ...string) *config.NoCacheRule {
	if len(rules) == 0 {
		return nil
	}
	u, err := url.Parse(rawUri)
	if err != nil {
		return nil
	}
	reqHost := u.Host
	reqPath := u.Path
	baseName := path.Base(reqPath)

	clientPath := ""
	if len(clientPaths) > 0 {
		clientPath = clientPaths[0]
	}

	var matchedBypass *config.NoCacheRule

	for i := range rules {
		rule := &rules[i]
		scopeMatched := rule.MatchScope(reqHost, reqPath)
		if !scopeMatched && clientPath != "" {
			scopeMatched = rule.MatchScope(reqHost, clientPath)
		}
		if !scopeMatched {
			continue
		}
		if !rule.MatchPattern(reqPath, baseName) && (clientPath == "" || !rule.MatchPattern(clientPath, path.Base(clientPath))) {
			continue
		}

		// 白名单规则绝对优先：若匹配到声明为 "cache" 的规则，立即判定为强制缓存落盘，绝不免缓存
		if rule.Action == "cache" {
			return nil
		}

		if rule.Action == "bypass" {
			matchedBypass = rule
		}
	}

	return matchedBypass
}

// ShouldBypassCache 判定是否应免除落盘缓存
func ShouldBypassCache(rawUri string, rules []config.NoCacheRule, clientPaths ...string) bool {
	return MatchBypassRule(rawUri, rules, clientPaths...) != nil
}
