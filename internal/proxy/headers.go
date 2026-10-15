package proxy

import (
	"net/http"
	"strings"
)

// hopByHopRequestHeaders 定义 RFC 9110 Section 7.6.1 标准逐跳标头与受保护标头
var hopByHopRequestHeaders = map[string]bool{
	"Connection":          true,
	"Proxy-Connection":    true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
	"Host":                true, // 由 http.Request.Host 独立管理
}

// relayRequestHeaders 统一全量透传客户端端到端请求头至出向请求，保留多值并严格过滤逐跳标头 (Feature 341)
func relayRequestHeaders(dst *http.Request, src *http.Request) {
	if src == nil || dst == nil {
		return
	}

	// 1. 解析 Connection 标头中动态声明的逐跳字段
	var dynamicHops map[string]bool
	if connVal := src.Header.Get("Connection"); connVal != "" {
		parts := strings.Split(connVal, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				if dynamicHops == nil {
					dynamicHops = make(map[string]bool)
				}
				dynamicHops[http.CanonicalHeaderKey(trimmed)] = true
			}
		}
	}

	// 2. 遍历客户端所有请求头，过滤逐跳标头并保留全部多值
	for k, vv := range src.Header {
		canonicalKey := http.CanonicalHeaderKey(k)
		if hopByHopRequestHeaders[canonicalKey] || dynamicHops[canonicalKey] {
			continue
		}
		for _, v := range vv {
			dst.Header.Add(k, v)
		}
	}
}

// hopByHopResponseHeaders 定义 RFC 9110 Section 7.6.1 标准响应逐跳标头 (Feature 342)
var hopByHopResponseHeaders = map[string]bool{
	"Connection":        true,
	"Proxy-Connection":  true,
	"Keep-Alive":        true,
	"Transfer-Encoding": true,
	"Trailer":           true,
	"Upgrade":           true,
}

// RelayResponseHeaders 过滤逐跳响应标头并安全透传端到端响应头 (Feature 342)
func RelayResponseHeaders(dst http.Header, src http.Header) {
	if dst == nil || src == nil {
		return
	}

	// 1. 解析 Connection 标头中动态声明的逐跳字段
	var dynamicHops map[string]bool
	if connVal := src.Get("Connection"); connVal != "" {
		parts := strings.Split(connVal, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				if dynamicHops == nil {
					dynamicHops = make(map[string]bool)
				}
				dynamicHops[http.CanonicalHeaderKey(trimmed)] = true
			}
		}
	}

	// 2. 遍历源响应头，剥离静态与动态逐跳标头，忠实保留多值
	for k, vv := range src {
		canonicalKey := http.CanonicalHeaderKey(k)
		if hopByHopResponseHeaders[canonicalKey] || dynamicHops[canonicalKey] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

