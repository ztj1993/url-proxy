package rewriter

import (
	"bytes"
	"path/filepath"
	"strings"

	"uri-proxy/internal/config"
)

// IsNumericOrVersion 检查字符串是否为版本号格式 (如 "4.17.21", "v20.11.0")，避免将版本号中的点误判为文件后缀
func IsNumericOrVersion(s string) bool {
	if len(s) == 0 {
		return false
	}
	trimmed := strings.TrimPrefix(s, "v")
	if len(trimmed) == 0 || trimmed[0] < '0' || trimmed[0] > '9' {
		return false
	}
	hasDot := false
	for _, r := range trimmed {
		if r == '.' {
			hasDot = true
		} else if (r < '0' || r > '9') && r != '-' && r != '+' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return hasDot
}

// IsKnownBinaryPath 检查 URL/路径是否显式为已知二进制安装包或压缩归档
func IsKnownBinaryPath(path string) bool {
	cleanPath := strings.ToLower(path)
	if idx := strings.IndexAny(cleanPath, "?#"); idx != -1 {
		cleanPath = cleanPath[:idx]
	}
	binaryExts := []string{
		".whl", ".tar.gz", ".tgz", ".tar.xz", ".tar.bz2",
		".deb", ".udeb", ".rpm", ".apk", ".crate",
		".zip", ".7z", ".iso", ".img", ".bin", ".exe", ".msi",
		".jar", ".war", ".aar",
		".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg", ".webp",
	}
	for _, ext := range binaryExts {
		if strings.HasSuffix(cleanPath, ext) {
			return true
		}
	}
	return false
}

// IsBinaryMimeType 检查 Content-Type 是否属于二进制类型 (严禁重写)
func IsBinaryMimeType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "application/octet-stream") ||
		strings.Contains(ct, "application/zip") ||
		strings.Contains(ct, "application/x-gzip") ||
		strings.Contains(ct, "application/gzip") ||
		strings.Contains(ct, "application/x-debian-package") ||
		strings.Contains(ct, "application/x-redhat-package-manager") ||
		strings.Contains(ct, "application/x-tar") ||
		strings.HasPrefix(ct, "image/") ||
		strings.HasPrefix(ct, "audio/") ||
		strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "font/")
}

// IsTextualMimeType 判断 Content-Type 是否为可安全重写的文本类型 (避免误改二进制流)
func IsTextualMimeType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "text/plain") ||
		strings.Contains(ct, "text/xml") ||
		strings.Contains(ct, "text/css") ||
		strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "application/xhtml") ||
		strings.Contains(ct, "application/vnd.pypi.simple") ||
		strings.Contains(ct, "application/vnd.npm") ||
		strings.Contains(ct, "application/javascript") ||
		strings.Contains(ct, "application/x-javascript") ||
		strings.Contains(ct, "text/javascript")
}

// BodyRewriter 响应体动态流式/内存重写器 (Feature 311)
type BodyRewriter struct {
	Rules []config.RewriteRule
	Types map[string]config.TypeDefinition
}

// NewBodyRewriter 创建重写器实例
func NewBodyRewriter(rules []config.RewriteRule, types map[string]config.TypeDefinition) *BodyRewriter {
	return &BodyRewriter{
		Rules: rules,
		Types: types,
	}
}

// MatchRules 匹配适用于当前请求路径的重写规则作用域
func (rw *BodyRewriter) MatchRules(reqPath, uri string) []config.RewriteRule {
	if rw == nil || len(rw.Rules) == 0 {
		return nil
	}
	normPath := reqPath
	if !strings.HasPrefix(normPath, "/") {
		normPath = "/" + normPath
	}
	trimmedNormPath := strings.TrimRight(normPath, "/")
	var matched []config.RewriteRule
	for _, r := range rw.Rules {
		scope := r.Scope
		normScope := scope
		if !strings.HasPrefix(normScope, "/") {
			normScope = "/" + normScope
		}
		trimmedNormScope := strings.TrimRight(normScope, "/")
		if strings.HasPrefix(normPath, normScope) ||
			strings.HasPrefix(reqPath, scope) ||
			strings.HasPrefix(uri, scope) ||
			strings.Contains(uri, scope) ||
			trimmedNormPath == trimmedNormScope {
			matched = append(matched, r)
		}
	}
	return matched
}

// MightMatchRequest 在向上游发起请求前，检查请求是否可能是重写目标 (用于决定是否走直通流与 identity 编码)
func (rw *BodyRewriter) MightMatchRequest(reqPath, uri string) bool {
	if rw == nil || len(rw.Rules) == 0 {
		return false
	}
	rules := rw.MatchRules(reqPath, uri)
	if len(rules) == 0 {
		return false
	}

	cleanPath := strings.ToLower(reqPath)
	if idx := strings.IndexAny(cleanPath, "?#"); idx != -1 {
		cleanPath = cleanPath[:idx]
	}

	for _, r := range rules {
		if IsKnownBinaryPath(cleanPath) {
			if strings.HasPrefix(strings.ToLower(r.TypeSpec), "suffix:") {
				ext := strings.TrimPrefix(strings.ToLower(r.TypeSpec), "suffix:")
				if !strings.HasPrefix(ext, ".") {
					ext = "." + ext
				}
				if strings.HasSuffix(cleanPath, ext) {
					return true
				}
			}
			continue
		}

		spec := strings.ToLower(strings.TrimSpace(r.TypeSpec))
		if spec == "" || spec == "*" || spec == "all" {
			return true
		}

		if strings.HasPrefix(spec, "suffix:") {
			ext := strings.TrimPrefix(spec, "suffix:")
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			if strings.HasSuffix(cleanPath, ext) {
				return true
			}
			continue
		}

		if strings.HasPrefix(spec, "type:") {
			typeName := strings.TrimPrefix(spec, "type:")
			if tDef, ok := rw.Types[typeName]; ok {
				base := filepath.Base(cleanPath)
				hasDot := strings.Contains(base, ".")
				if hasDot && len(tDef.Suffixes) > 0 {
					if typeName == "npm" || IsNumericOrVersion(base) {
						return true
					}
					for _, s := range tDef.Suffixes {
						if strings.HasSuffix(cleanPath, s) {
							return true
						}
					}
				} else {
					return true
				}
			} else {
				return true
			}
			continue
		}

		return true
	}

	return false
}

// RuleMatchesResponse 检查单条重写规则是否匹配实际响应头 Content-Type 及路径
func (rw *BodyRewriter) RuleMatchesResponse(r config.RewriteRule, reqPath, uri, contentType string) bool {
	ct := strings.ToLower(contentType)
	cleanPath := strings.ToLower(reqPath)
	if idx := strings.IndexAny(cleanPath, "?#"); idx != -1 {
		cleanPath = cleanPath[:idx]
	}

	spec := strings.ToLower(strings.TrimSpace(r.TypeSpec))

	if spec == "" || spec == "*" || spec == "all" {
		if IsBinaryMimeType(ct) {
			return false
		}
		return IsTextualMimeType(contentType)
	}

	if strings.HasPrefix(spec, "type:") {
		typeName := strings.TrimPrefix(spec, "type:")
		tDef, ok := rw.Types[typeName]
		if !ok {
			return IsTextualMimeType(contentType)
		}
		return rw.matchTypeDefinition(tDef, ct, cleanPath)
	}

	if strings.HasPrefix(spec, "mime:") {
		pattern := strings.TrimPrefix(spec, "mime:")
		if strings.HasSuffix(pattern, "*") {
			return strings.HasPrefix(ct, strings.TrimSuffix(pattern, "*"))
		}
		return strings.Contains(ct, pattern)
	}

	if strings.HasPrefix(spec, "suffix:") {
		ext := strings.TrimPrefix(spec, "suffix:")
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		return strings.HasSuffix(cleanPath, ext)
	}

	if tDef, ok := rw.Types[spec]; ok {
		return rw.matchTypeDefinition(tDef, ct, cleanPath)
	}

	if IsBinaryMimeType(ct) {
		return false
	}
	return IsTextualMimeType(contentType)
}

// ShouldRewrite 判定当前请求和响应是否应当触发响应体重写
func (rw *BodyRewriter) ShouldRewrite(reqPath, uri, contentType string) bool {
	if rw == nil || len(rw.Rules) == 0 {
		return false
	}
	rules := rw.MatchRules(reqPath, uri)
	for _, r := range rules {
		if rw.RuleMatchesResponse(r, reqPath, uri, contentType) {
			return true
		}
	}
	return false
}

// Apply 执行重写替换
func (rw *BodyRewriter) Apply(clientHost string, reqPath, uri, contentType string, body []byte) []byte {
	rules := rw.MatchRules(reqPath, uri)
	if len(rules) == 0 {
		return body
	}
	scheme := "http"
	res := body
	for _, r := range rules {
		if rw.RuleMatchesResponse(r, reqPath, uri, contentType) {
			target := strings.ReplaceAll(r.Replacement, "{PROXY_HOST}", clientHost)
			target = strings.ReplaceAll(target, "{PROXY_SCHEME}", scheme)
			res = bytes.ReplaceAll(res, []byte(r.Pattern), []byte(target))
		}
	}
	return res
}

func (rw *BodyRewriter) matchTypeDefinition(tDef config.TypeDefinition, ct, cleanPath string) bool {
	for _, m := range tDef.Mimes {
		if strings.HasSuffix(m, "*") {
			if strings.HasPrefix(ct, strings.TrimSuffix(m, "*")) {
				return true
			}
		} else {
			if strings.Contains(ct, m) {
				return true
			}
		}
	}
	for _, s := range tDef.Suffixes {
		if strings.HasSuffix(cleanPath, s) {
			return true
		}
	}
	return false
}
