package proxy

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/rewriter"
)

// Server 统一代理协议服务器
type Server struct {
	cfg           *config.ProxyConfig
	cacheDir      string
	downMgr       *downloader.Manager
	rewriter      *rewriter.BodyRewriter
	plugins       *plugin.Registry
	addr          string
	requestID     uint64
	httpClient    *http.Client
	tunnelTimeout time.Duration
	activeTunnels map[net.Conn]struct{}
	tunnelsMu     sync.Mutex
}

// NewUpstreamTransport 创建针对高并发镜像代理优化的 HTTP Transport (Feature 334)
func NewUpstreamTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewServer 初始化代理服务器实体
func NewServer(
	cfg *config.ProxyConfig,
	cacheDir string,
	downMgr *downloader.Manager,
	rw *rewriter.BodyRewriter,
	plugins *plugin.Registry,
	addr string,
) *Server {
	return &Server{
		cfg:           cfg,
		cacheDir:      cacheDir,
		downMgr:       downMgr,
		rewriter:      rw,
		plugins:       plugins,
		addr:          addr,
		httpClient:    &http.Client{Transport: NewUpstreamTransport()},
		tunnelTimeout: 5 * time.Minute,
		activeTunnels: make(map[net.Conn]struct{}),
	}
}

// Close 释放代理服务器持有的长连接资源与空闲连接池 (Feature 339)
func (s *Server) Close() {
	if s.httpClient != nil {
		if tr, ok := s.httpClient.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
	s.closeTunnels()
}

func (s *Server) trackTunnel(conns ...net.Conn) {
	s.tunnelsMu.Lock()
	defer s.tunnelsMu.Unlock()
	for _, c := range conns {
		if c != nil {
			s.activeTunnels[c] = struct{}{}
		}
	}
}

func (s *Server) untrackTunnel(conns ...net.Conn) {
	s.tunnelsMu.Lock()
	defer s.tunnelsMu.Unlock()
	for _, c := range conns {
		if c != nil {
			delete(s.activeTunnels, c)
		}
	}
}

func (s *Server) closeTunnels() {
	s.tunnelsMu.Lock()
	defer s.tunnelsMu.Unlock()
	for c := range s.activeTunnels {
		_ = c.Close()
	}
	s.activeTunnels = make(map[net.Conn]struct{})
}

// SetHTTPClient 设置出向 HTTP 客户端（主要用于测试中调整超时参数）
func (s *Server) SetHTTPClient(client *http.Client) {
	s.httpClient = client
}

// HTTPClient 获取出向 HTTP 客户端
func (s *Server) HTTPClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

func (s *Server) doRequest(req *http.Request) (*http.Response, error) {
	return s.HTTPClient().Do(req)
}

// SetTunnelTimeout 设置 CONNECT 隧道空闲超时
func (s *Server) SetTunnelTimeout(d time.Duration) {
	s.tunnelTimeout = d
}

// TunnelTimeout 获取 CONNECT 隧道空闲超时
func (s *Server) TunnelTimeout() time.Duration {
	if s.tunnelTimeout <= 0 {
		return 5 * time.Minute
	}
	return s.tunnelTimeout
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

	// 1. 处理 HTTPS CONNECT 隧道代理 (Feature 335)
	if r.Method == http.MethodConnect {
		s.handleConnect(w, r, reqID)
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