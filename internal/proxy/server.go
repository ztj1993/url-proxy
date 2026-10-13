package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/rewriter"
	"uri-proxy/internal/ttl"
)

// Server 统一代理协议服务器
type Server struct {
	cfg       *config.ProxyConfig
	cacheDir  string
	downMgr   *downloader.Manager
	ttlMgr    *ttl.TTLManager
	rewriter  *rewriter.BodyRewriter
	plugins   *plugin.Registry
	addr      string
	requestID uint64
}

// NewServer 初始化代理服务器实体
func NewServer(
	cfg *config.ProxyConfig,
	cacheDir string,
	downMgr *downloader.Manager,
	ttlMgr *ttl.TTLManager,
	rw *rewriter.BodyRewriter,
	plugins *plugin.Registry,
	addr string,
) *Server {
	return &Server{
		cfg:      cfg,
		cacheDir: cacheDir,
		downMgr:  downMgr,
		ttlMgr:   ttlMgr,
		rewriter: rw,
		plugins:  plugins,
		addr:     addr,
	}
}

func copyHeader(dst http.Header, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func in(target string, array []string) bool {
	for _, element := range array {
		if target == element {
			return true
		}
	}
	return false
}

// ServeHTTP 实现 http.Handler 接口的主请求分发逻辑
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := atomic.AddUint64(&s.requestID, 1)

	var activePluginName string
	var aliases map[string]string
	var forwards map[string]string
	var nocacheRules []config.NoCacheRule
	if s.cfg != nil {
		aliases = s.cfg.Aliases
		forwards = s.cfg.Forwards
		nocacheRules = s.cfg.NoCacheRules
	}

	// 1. 处理 HTTPS CONNECT 隧道代理 (用于标准正向代理下的 HTTPS 透传)
	if r.Method == http.MethodConnect {
		log.Printf("[%d] connect tunnel: %s", reqID, r.Host)
		destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
		if err != nil {
			log.Printf("[%d] connect dial err: %v", reqID, err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			log.Printf("[%d] connect hijack not supported", reqID)
			http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
			_ = destConn.Close()
			return
		}
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			log.Printf("[%d] connect hijack err: %v", reqID, err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			_ = destConn.Close()
			return
		}
		_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() {
			defer destConn.Close()
			defer clientConn.Close()
			_, _ = io.Copy(destConn, clientConn)
		}()
		go func() {
			defer destConn.Close()
			defer clientConn.Close()
			_, _ = io.Copy(clientConn, destConn)
		}()
		return
	}

	// 2. 提取目标 URI：兼容标准 HTTP 正向代理模式与路径前缀代理模式
	var uri string
	if r.URL.IsAbs() || (r.URL.Host != "" && r.URL.Scheme != "") {
		uri = r.URL.String()
	} else if r.URL.Host != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		uri = fmt.Sprintf("%s://%s%s", scheme, r.URL.Host, r.URL.RequestURI())
	} else if len(r.URL.Path) > 1 {
		uri = r.URL.Path[1:]
		if r.URL.RawQuery != "" {
			uri += "?" + r.URL.RawQuery
		}
	}

	log.Printf("[%d] req: %s", reqID, uri)
	uris, err := url.Parse(uri)
	if err != nil {
		log.Printf("[%d] uri err: %s", reqID, err)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// 3. 判断 URI 是否符合指定的模式，若不符合则尝试匹配路径短别名路由 (Feature 306)
	prefixes := []string{"ftp", "http", "https"}
	if !in(uris.Scheme, prefixes) {
		pathOnly := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(pathOnly, "/", 2)
		aliasKey := parts[0]

		if targetBase, ok := aliases[aliasKey]; ok && aliasKey != "" {
			if s.cfg != nil && s.cfg.AliasPlugins != nil {
				activePluginName = s.cfg.AliasPlugins[aliasKey]
			}
			rewrittenUri := strings.TrimRight(targetBase, "/")
			if len(parts) > 1 && parts[1] != "" {
				rewrittenUri += "/" + strings.TrimLeft(parts[1], "/")
			} else if strings.HasSuffix(r.URL.Path, "/") {
				rewrittenUri += "/"
			}
			if r.URL.RawQuery != "" {
				rewrittenUri += "?" + r.URL.RawQuery
			}

			newUris, err := url.Parse(rewrittenUri)
			if err != nil {
				log.Printf("[%d] alias rewrite url parse err: %v", reqID, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if !in(newUris.Scheme, prefixes) {
				log.Printf("[%d] alias rewritten scheme invalid: %s", reqID, rewrittenUri)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			log.Printf("[%d] alias route rewrite: %s -> %s", reqID, uri, rewrittenUri)
			uri = rewrittenUri
			uris = newUris
		} else {
			log.Printf("[%d] uri scheme invalid or alias not found: %s", reqID, uri)
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}

	hasRewriteMatch := s.rewriter != nil && s.rewriter.MightMatchRequest(r.URL.Path, uri)
	matchedBypass := cache.MatchBypassRule(uri, nocacheRules, r.URL.Path)

	// Feature 332: 检查是否匹配带 TTL 的磁盘时效缓存规则
	if matchedBypass != nil && matchedBypass.TTL > 0 && !hasRewriteMatch && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.handleDiskTTL(w, r, reqID, uris, uri, forwards, matchedBypass)
		return
	}

	// 4. 检查是否命中动态免缓存规则或响应体重写规则 (免缓存与重写资源直接纯流式透传，严禁落盘)
	if matchedBypass != nil || hasRewriteMatch {
		s.handleBypass(w, r, reqID, uris, uri, forwards, matchedBypass, hasRewriteMatch)
		return
	}

	// 5. 若当前路由作用域绑定了协议插件，优先执行插件专属的高速直出 (Feature 317)
	if activePluginName != "" && s.plugins != nil {
		if p, ok := s.plugins.Get(activePluginName); ok {
			if p.FastPathServe(w, r, uri, s.cacheDir) {
				log.Printf("[%d] [%s] plugin fast path direct serve: %s", reqID, p.Name(), uri)
				return
			}
		}
	}

	// 6. 处理 HTTP HEAD 请求 (特别是 Docker 镜像分层探测与常规元数据查询)
	if r.Method == http.MethodHead {
		s.handleHead(w, r, reqID, uris, uri, forwards)
		return
	}

	// 7. 处理 HTTP Range 分片请求与 206 断点续传
	if r.Header.Get("Range") != "" {
		s.handleRange(w, r, reqID, uris, uri, forwards)
		return
	}

	// 8. 获取任务 (Request Coalescing Download)
	s.handleDownload(w, r, reqID, uris, uri, forwards)
}