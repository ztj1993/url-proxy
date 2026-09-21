package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	addr      = flag.String("addr", ":8888", "http server addr")
	cache     = flag.String("cache", "", "cache directory")
	conf      = flag.String("conf", "proxy.conf", "path to unified proxy configuration file")
	idleTime  = flag.Duration("idle-time", 10*time.Minute, "idle timeout for hanging downloads (default 10m)")
	jobs      = make(map[string]*DownloadJob)
	jobsMu    sync.Mutex
	requestID uint64
)

//DownloadJob 下载任务状态管理
type DownloadJob struct {
	Uri        string
	FilePath   string
	Header     http.Header
	StatusCode int
	HeaderDone chan struct{} // Header 准备好信号
	Done       chan struct{} // 下载完成信号
	Err        error
	Cond       *sync.Cond         // 用于广播新数据写入
	Mu         sync.Mutex         // 配合 Cond 使用
	Readers    int32              // 当前正在读取 Followers 的数量
	RenameMu   sync.Mutex         // 用于保护 Rename 操作
	FinalName  string             // 最终缓存文件名
	TmpName    string             // 临时文件名
	LastActive time.Time          // 最后一次成功读取数据的时间戳
	Cancel     context.CancelFunc // 用于取消底层 HTTP 请求
}

//getDownloadJob 获取或创建下载任务
//返回 job 和 isLeader (是否为下载负责人)
func getDownloadJob(uri string) (*DownloadJob, bool) {
	jobsMu.Lock()
	defer jobsMu.Unlock()

	if job, ok := jobs[uri]; ok {
		return job, false
	}

	job := &DownloadJob{
		Uri:        uri,
		HeaderDone: make(chan struct{}),
		Done:       make(chan struct{}),
		Header:     make(http.Header),
		LastActive: time.Now(),
	}
	job.Cond = sync.NewCond(&job.Mu)
	jobs[uri] = job
	return job, true
}

//removeJob 移除下载任务
func removeJob(uri string) {
	jobsMu.Lock()
	delete(jobs, uri)
	jobsMu.Unlock()
}

//tryRename 尝试重命名文件
func (job *DownloadJob) tryRename(reqID uint64) {
	job.RenameMu.Lock()
	defer job.RenameMu.Unlock()

	// 如果已经重命名成功，或者临时文件不存在，则直接返回
	if _, err := os.Stat(job.FinalName); err == nil {
		log.Printf("[%d] rename skipped: final file already exists %s", reqID, job.FinalName)
		return
	}
	if _, err := os.Stat(job.TmpName); os.IsNotExist(err) {
		log.Printf("[%d] rename skipped: tmp file does not exist %s", reqID, job.TmpName)
		return
	}

	// 只有当没有 Readers 时才尝试重命名 (主要针对 Windows)
	readers := atomic.LoadInt32(&job.Readers)
	if readers == 0 {
		// 如果任务发生错误，则删除临时文件，不进行重命名
		if job.Err != nil {
			log.Printf("[%d] job has error (%v), removing tmp file: %s", reqID, job.Err, job.TmpName)
			_ = os.Remove(job.TmpName)
			return
		}

		log.Printf("[%d] attempting rename: %s -> %s", reqID, job.TmpName, job.FinalName)
		err := os.Rename(job.TmpName, job.FinalName)
		if err == nil {
			job.FilePath = job.FinalName
			log.Printf("[%d] job renamed successfully: %s", reqID, job.FinalName)
		} else {
			log.Printf("[%d] job rename failed: %v", reqID, err)
		}
	} else {
		log.Printf("[%d] rename skipped: file is still being read by %d followers", reqID, readers)
	}
}

//in 判断字符串是否存在于数组中
func in(target string, array []string) bool {
	for _, element := range array {
		if target == element {
			return true
		}
	}
	return false
}

//copyHeader 复制响应头
func copyHeader(dst http.Header, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

//getSafeUrlPath 根据 URL 生成安全的本地缓存相对路径
//规则: 协议-域名-端口/请求路径/url编码后的(文件名?参数)
func getSafeUrlPath(rawUri string) string {
	u, err := url.Parse(rawUri)
	if err != nil {
		// 解析失败回退为完全编码
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

	// 如果是以 / 结尾，说明全是目录，文件名为 index
	if strings.HasSuffix(u.Path, "/") {
		pDir = u.Path
		pBase = "index"
	}

	// 根目录处理
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

// MatchScope 检查请求 Host 与 Path 是否匹配规则作用域
func (r *NoCacheRule) MatchScope(reqHost, reqPath string) bool {
	if r.Scope == "" || r.Scope == "*" {
		return true
	}

	scopeLower := strings.ToLower(r.Scope)
	reqHostLower := strings.ToLower(reqHost)
	reqPathLower := strings.ToLower(reqPath)

	// 路径前缀作用域: 以 "/" 开头，例如 "/dists/"
	if strings.HasPrefix(scopeLower, "/") {
		return strings.Contains(reqPathLower, scopeLower)
	}

	// 域名或 域名+路径 复合作用域: 例如 "archive.ubuntu.com/dists/"
	hostAndPath := reqHostLower + reqPathLower
	if strings.Contains(scopeLower, "/") {
		return strings.Contains(hostAndPath, scopeLower)
	}

	// 纯域名作用域: 例如 "archive.ubuntu.com"
	return reqHostLower == scopeLower || strings.HasPrefix(reqHostLower, scopeLower+":")
}

// MatchPattern 检查 URL 路径及文件名是否符合匹配模式
func (r *NoCacheRule) MatchPattern(urlPath, baseName string) bool {
	if r.Type == "*" || r.Type == "all" || r.Pattern == "*" {
		return true
	}
	baseLower := strings.ToLower(baseName)
	pathLower := strings.ToLower(urlPath)
	patternLower := strings.ToLower(r.Pattern)

	switch r.Type {
	case "exact":
		return baseLower == patternLower || strings.Trim(pathLower, "/") == strings.Trim(patternLower, "/")
	case "prefix":
		return strings.HasPrefix(baseLower, patternLower) || strings.HasPrefix(pathLower, patternLower)
	case "suffix":
		return strings.HasSuffix(baseLower, patternLower) || strings.HasSuffix(pathLower, patternLower)
	case "contains":
		return strings.Contains(pathLower, patternLower) || strings.Contains(baseLower, patternLower)
	case "regex":
		if r.Regexp != nil {
			return r.Regexp.MatchString(urlPath) || r.Regexp.MatchString(baseName)
		}
	}
	return false
}

// shouldBypassCache 根据规则判定请求是否应当免除本地落盘缓存
// 规则完全由配置文件驱动，无任何代码硬编码
func shouldBypassCache(rawUri string, rules []NoCacheRule) bool {
	if len(rules) == 0 {
		return false
	}
	u, err := url.Parse(rawUri)
	if err != nil {
		return false
	}
	reqHost := u.Host
	reqPath := u.Path
	baseName := path.Base(reqPath)

	var hasBypassMatch bool

	for _, rule := range rules {
		if !rule.MatchScope(reqHost, reqPath) {
			continue
		}
		if !rule.MatchPattern(reqPath, baseName) {
			continue
		}

		// 白名单规则绝对优先：若匹配到声明为 "cache" 的规则，立即判定为强制缓存落盘，绝不免缓存
		if rule.Action == "cache" {
			return false
		}

		if rule.Action == "bypass" {
			hasBypassMatch = true
		}
	}

	return hasBypassMatch
}

//Handler 请求处理程序
func Handler(w http.ResponseWriter, r *http.Request, config *ProxyConfig) {
	reqID := atomic.AddUint64(&requestID, 1)

	var aliases map[string]string
	var forwards map[string]string
	var nocacheRules []NoCacheRule
	var rewriter *BodyRewriter
	if config != nil {
		aliases = config.Aliases
		forwards = config.Forwards
		nocacheRules = config.NoCacheRules
		rewriter = config.Rewriter
	}

	// 处理 HTTPS CONNECT 隧道代理 (用于标准正向代理下的 HTTPS 透传)
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

	// 提取目标 URI：兼容标准 HTTP 正向代理模式与路径前缀代理模式
	var uri string
	if r.URL.IsAbs() || (r.URL.Host != "" && r.URL.Scheme != "") {
		// 标准正向代理模式: GET http://example.com/path HTTP/1.1
		uri = r.URL.String()
	} else if r.URL.Host != "" {
		// 部分正向代理客户端可能 Scheme 为空但 Host 存在，默认使用 http
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		uri = fmt.Sprintf("%s://%s%s", scheme, r.URL.Host, r.URL.RequestURI())
	} else if len(r.URL.Path) > 1 {
		// 路径前缀代理模式: GET /http://example.com/path HTTP/1.1
		uri = r.URL.Path[1:]
		if r.URL.RawQuery != "" {
			uri += "?" + r.URL.RawQuery
		}
	}

	log.Printf("[%d] req: %s", reqID, uri)
	//获取 URI 并且必须大于 6 个字符
	uris, err := url.Parse(uri)
	if err != nil {
		log.Printf("[%d] uri err: %s", reqID, err)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	//判断 URI 是否符合指定的模式，若不符合则尝试匹配路径短别名路由 (Feature 306)
	prefixes := []string{"ftp", "http", "https"}
	if !in(uris.Scheme, prefixes) {
		pathOnly := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(pathOnly, "/", 2)
		aliasKey := parts[0]

		if targetBase, ok := aliases[aliasKey]; ok && aliasKey != "" {
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

	hasRewriteMatch := rewriter != nil && rewriter.MightMatchRequest(r.URL.Path, uri)

	// 检查是否命中动态免缓存规则或响应体重写规则 (免缓存与重写资源直接纯流式透传，严禁落盘)
	if shouldBypassCache(uri, nocacheRules) || hasRewriteMatch {
		if hasRewriteMatch {
			log.Printf("[%d] dynamic body rewrite target: %s", reqID, uri)
		} else {
			log.Printf("[%d] cache bypass (dynamic resource): %s", reqID, uri)
		}
		targetUri := uri
		if _, ok := forwards[uris.Host]; ok {
			targetUri = forwards[uris.Host] + "/" + uri
		}

		req, err := http.NewRequestWithContext(r.Context(), r.Method, targetUri, nil)
		if err != nil {
			log.Printf("[%d] req create err: %s", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// 透传条件协商、Range与客户端特征头 (包括 PEP 691 内容协商所需 Accept 与 Docker 鉴权所需的 Authorization)
		bypassForwardHeaders := []string{"If-Modified-Since", "If-None-Match", "Range", "If-Range", "Cache-Control", "User-Agent", "Accept", "Accept-Language", "Authorization"}
		for _, h := range bypassForwardHeaders {
			if val := r.Header.Get(h); val != "" {
				req.Header.Set(h, val)
			}
		}

		// 如果匹配重写规则，注入 Accept-Encoding: identity 确保拉取未压缩的明文字符流
		if hasRewriteMatch {
			req.Header.Set("Accept-Encoding", "identity")
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[%d] upstream bypass get err: %s", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		copyHeader(w.Header(), resp.Header)

		// 检查是否应当执行响应体动态重写
		if rewriter != nil && rewriter.ShouldRewrite(r.URL.Path, uri, resp.Header.Get("Content-Type")) && resp.StatusCode == http.StatusOK {
			clientHost := r.Host
			if clientHost == "" {
				clientHost = r.URL.Host
			}
			if clientHost == "" {
				clientHost = *addr
			}

			var reader io.Reader = resp.Body
			if resp.Header.Get("Content-Encoding") == "gzip" {
				if gz, err := gzip.NewReader(resp.Body); err == nil {
					defer gz.Close()
					reader = gz
					w.Header().Del("Content-Encoding")
				}
			}

			rawBody, err := io.ReadAll(reader)
			if err != nil {
				log.Printf("[%d] read body err: %v", reqID, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			rewrittenBody := rewriter.Apply(clientHost, r.URL.Path, uri, resp.Header.Get("Content-Type"), rawBody)

			// 剥离原 ETag 与 Content-MD5，更新 Content-Length
			w.Header().Del("ETag")
			w.Header().Del("Content-MD5")
			w.Header().Set("Content-Length", strconv.Itoa(len(rewrittenBody)))

			log.Printf("[%d] body rewritten (%d -> %d bytes): %s", reqID, len(rawBody), len(rewrittenBody), uri)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(rewrittenBody)
			return
		}

		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	// 针对内容寻址的不可变分层 Blob (/blobs/sha256:)，若本地已有完整缓存，直接高速直出，免除上游回源
	if strings.Contains(uri, "/blobs/sha256:") {
		cachePath := filepath.Join(*cache, getSafeUrlPath(uri))
		if stat, err := os.Stat(cachePath); err == nil && !stat.IsDir() && stat.Size() > 0 {
			log.Printf("[%d] blob hit local cache (direct serve): %s", reqID, uri)
			w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Type", "application/octet-stream")
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			http.ServeFile(w, r, cachePath)
			return
		}
	}

	// 处理 HTTP HEAD 请求 (特别是 Docker 镜像分层探测与常规元数据查询)
	if r.Method == http.MethodHead {
		cachePath := filepath.Join(*cache, getSafeUrlPath(uri))
		if stat, err := os.Stat(cachePath); err == nil && !stat.IsDir() {
			log.Printf("[%d] head hit local cache: %s", reqID, uri)
			w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}

		// 本地无缓存，直接向上游发起透传 HEAD 请求
		log.Printf("[%d] head relay to upstream: %s", reqID, uri)
		targetUri := uri
		if _, ok := forwards[uris.Host]; ok {
			targetUri = forwards[uris.Host] + "/" + uri
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodHead, targetUri, nil)
		if err != nil {
			log.Printf("[%d] req create err: %s", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		headForwardHeaders := []string{"Authorization", "User-Agent", "Accept", "If-Modified-Since", "If-None-Match"}
		for _, h := range headForwardHeaders {
			if val := r.Header.Get(h); val != "" {
				req.Header.Set(h, val)
			}
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[%d] upstream head err: %s", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		return
	}

	// 处理 HTTP Range 分片请求与 206 断点续传
	if r.Header.Get("Range") != "" {
		cachePath := filepath.Join(*cache, getSafeUrlPath(uri))
		if stat, err := os.Stat(cachePath); err == nil && !stat.IsDir() {
			// 本地存在已下载完成的完整缓存，直接通过 http.ServeFile 高效处理 Range 分片
			log.Printf("[%d] range hit local cache: %s (%s)", reqID, uri, r.Header.Get("Range"))
			http.ServeFile(w, r, cachePath)
			return
		}

		// 本地无完整缓存，直接向上游发起透传 Range 请求，纯流式返回，不落盘（避免局部脏切片污染完整文件缓存）
		log.Printf("[%d] range relay to upstream: %s (%s)", reqID, uri, r.Header.Get("Range"))
		targetUri := uri
		if _, ok := forwards[uris.Host]; ok {
			targetUri = forwards[uris.Host] + "/" + uri
		}

		req, err := http.NewRequestWithContext(r.Context(), "GET", targetUri, nil)
		if err != nil {
			log.Printf("[%d] req create err: %s", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// 透传 Range、If-Range、条件协商与客户端特征头 (包括 Docker 鉴权所需的 Authorization)
		rangeRelayHeaders := []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match", "User-Agent", "Authorization"}
		for _, h := range rangeRelayHeaders {
			if val := r.Header.Get(h); val != "" {
				req.Header.Set(h, val)
			}
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[%d] upstream range get err: %s", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	//获取任务 (Request Coalescing)
	job, isLeader := getDownloadJob(uri)

	// Leader 负责下载数据
	if isLeader {
		log.Printf("[%d] job leader: %s", reqID, uri)
		go func() {
			defer removeJob(uri)
			// 确保结束时唤醒所有等待者
			defer func() {
				job.Cond.L.Lock()
				job.Cond.Broadcast()
				job.Cond.L.Unlock()
			}()
			defer close(job.Done)

			//缓存文件路径
			name := filepath.Join(*cache, getSafeUrlPath(uri))
			stat, _ := os.Stat(name)

			//转发处理
			targetUri := uri
			if _, ok := forwards[uris.Host]; ok {
				targetUri = forwards[uris.Host] + "/" + uri
			}

			log.Printf("[%d] job download: %s", reqID, targetUri)

			// 创建带有 Cancel 的 Context，用于空闲超时控制
			ctx, cancel := context.WithCancel(context.Background())
			job.Cancel = cancel
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "GET", targetUri, nil)
			if err != nil {
				log.Printf("[%d] req create err: %s", reqID, err)
				job.Err = err
				close(job.HeaderDone)
				return
			}

			// 透传客户端的条件请求头 (用于 304 缓存协商) 与鉴权/特征头 (用于 Docker 等源)
			jobForwardHeaders := []string{"If-Modified-Since", "If-None-Match", "Authorization", "User-Agent"}
			for _, h := range jobForwardHeaders {
				if val := r.Header.Get(h); val != "" {
					req.Header.Set(h, val)
				}
			}

			//请求远端文件
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Printf("[%d] get err: %s", reqID, err)
				job.Err = err
				close(job.HeaderDone)
				return
			}
			defer func(Body io.ReadCloser) {
				_ = Body.Close()
			}(resp.Body)

			//设置 Job 信息
			job.StatusCode = resp.StatusCode
			copyHeader(job.Header, resp.Header)

			// 如果上游返回 304 Not Modified，无需拉取 Body，直接短路通知并返回
			if resp.StatusCode == http.StatusNotModified {
				log.Printf("[%d] job not modified (304): %s", reqID, uri)
				close(job.HeaderDone)
				return
			}

			length, _ := strconv.ParseInt(
				resp.Header.Get("Content-Length"), 10, 64,
			)

			//如果缓存已存在且完整，直接使用
			if stat != nil && stat.Size() == length {
				log.Printf("[%d] job cached: %s", reqID, uri)
				job.FilePath = name
				close(job.HeaderDone)
				return
			}

			//准备写入
			dir := filepath.Dir(name)
			_ = os.MkdirAll(dir, 0755)

			tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)

			// 记录文件名，供 tryRename 使用
			job.FinalName = name
			job.TmpName = tmp

			file, err := os.Create(tmp)
			if err != nil {
				log.Printf("[%d] file create err: %s", reqID, err)
				job.Err = err
				close(job.HeaderDone)
				return
			}

			job.FilePath = tmp
			close(job.HeaderDone) // 通知 Followers 可以开始读了

			// 写入循环
			buf := make([]byte, 32*1024)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					// 更新最后活跃时间
					job.Mu.Lock()
					job.LastActive = time.Now()
					job.Mu.Unlock()

					_, wErr := file.Write(buf[:n])
					if wErr != nil {
						log.Printf("[%d] file write err: %s", reqID, wErr)
						job.Err = wErr
						break
					}
					// 广播：有新数据了
					job.Cond.L.Lock()
					job.Cond.Broadcast()
					job.Cond.L.Unlock()
				}
				if err != nil {
					if err != io.EOF {
						job.Err = err
					}
					// 检查是否是因为超时被 Cancel 掉的
					if err == context.Canceled {
						log.Printf("[%d] job cancelled due to idle timeout: %s", reqID, uri)
					} else if err != io.EOF {
						log.Printf("[%d] job read err: %s", reqID, err)
					}
					break
				}
			}
			log.Printf("[%d] job finished: %s", reqID, uri)

			_ = file.Close()

			// Leader 下载完成后，尝试重命名
			job.tryRename(reqID)
		}()
	} else {
		log.Printf("[%d] job follower: %s", reqID, uri)
	}

	// 等待 Header 可用
	<-job.HeaderDone

	if job.Err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// 发送响应头
	copyHeader(w.Header(), job.Header)
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(job.StatusCode)

	// 如果是 304 Not Modified，无需读取和传输 Body，直接返回
	if job.StatusCode == http.StatusNotModified {
		log.Printf("[%d] 304 response sent: %s", reqID, uri)
		return
	}

	// 增加 Reader 计数
	atomic.AddInt32(&job.Readers, 1)

	// 打开文件进行读取
	// 注意：如果是 Follower，这里打开的可能是正在写入的 tmp 文件
	file, err := os.Open(job.FilePath)
	if err != nil {
		log.Printf("[%d] file open err: %s", reqID, err)
		atomic.AddInt32(&job.Readers, -1) // 失败时减少计数
		return
	}
	defer func() {
		_ = file.Close()
		// Reader 退出时减少计数
		if atomic.AddInt32(&job.Readers, -1) == 0 {
			// 如果我是最后一个 Reader，且 Leader 已经完成下载，尝试重命名
			select {
			case <-job.Done:
				job.tryRename(reqID)
			default:
			}
		}
	}()

	// 流式发送数据
	buf := make([]byte, 32*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, wErr := w.Write(buf[:n]); wErr != nil {
				log.Printf("[%d] client write err (cancelled)", reqID)
				return
			}
		}
		if err == io.EOF {
			// 读到末尾了，检查下载是否完成
			select {
			case <-job.Done:
				// 下载已完成，且读到了 EOF，说明真的结束了
				if job.Err != nil {
					log.Printf("[%d] job done with error: %v", reqID, job.Err)
				} else {
					log.Printf("[%d] job done: %s", reqID, uri)
				}
				return
			default:
				// 下载未完成，等待新数据
				job.Cond.L.Lock()
				job.Cond.Wait()
				job.Cond.L.Unlock()
			}
		} else if err != nil {
			log.Printf("[%d] read err: %s", reqID, err)
			return
		}
	}
}

// NoCacheRule 免缓存与缓存保护规则 (用于 Feature 305)
type NoCacheRule struct {
	Scope   string         // 作用域: "*", "/dists/", "archive.ubuntu.com", "archive.ubuntu.com/dists/"
	Action  string         // 动作: "bypass" (免缓存) 或 "cache" (白名单例外，强制缓存)
	Type    string         // 匹配类型: "exact", "prefix", "suffix", "regex"
	Pattern string         // 匹配模式串
	Regexp  *regexp.Regexp // 当 Type 为 "regex" 时的预编译正则表达式
}

// TypeDefinition 内容类型定义 (Feature 311)
type TypeDefinition struct {
	Name     string   // 类型别名 (如 "html", "json", "pypi", "text")
	Mimes    []string // 匹配的 Content-Type 列表 (如 "text/html", "application/json", "application/vnd.pypi.simple*")
	Suffixes []string // 匹配的 URL/文件名后缀列表 (如 ".html", ".htm", ".json")
}

// defaultTypeDefinitions 获取系统内置默认内容类型
func defaultTypeDefinitions() map[string]TypeDefinition {
	return map[string]TypeDefinition{
		"html": {
			Name:     "html",
			Mimes:    []string{"text/html", "application/xhtml+xml"},
			Suffixes: []string{".html", ".htm", ".xhtml"},
		},
		"json": {
			Name:     "json",
			Mimes:    []string{"application/json", "application/vnd.npm.install-v1+json"},
			Suffixes: []string{".json"},
		},
		"xml": {
			Name:     "xml",
			Mimes:    []string{"text/xml", "application/xml"},
			Suffixes: []string{".xml"},
		},
		"pypi": {
			Name:     "pypi",
			Mimes:    []string{"text/html", "application/vnd.pypi.simple"},
			Suffixes: []string{".html"},
		},
		"npm": {
			Name:     "npm",
			Mimes:    []string{"application/json", "application/vnd.npm"},
			Suffixes: []string{".json"},
		},
		"text": {
			Name:     "text",
			Mimes:    []string{"text/"},
			Suffixes: []string{".txt", ".text"},
		},
	}
}

// isNumericOrVersion 检查字符串是否为版本号格式 (如 "4.17.21", "v20.11.0")，避免将版本号中的点误判为文件后缀
func isNumericOrVersion(s string) bool {
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

// RewriteRule 响应体动态重写规则 (Feature 311)
type RewriteRule struct {
	Scope       string // 生效的作用域路径前缀 (如 "/pypi/simple/", "/npm/")
	TypeSpec    string // 内容类型规格 (如 "type:json", "type:pypi", "mime:text/html", "suffix:.json", "*" 或空)
	Pattern     string // 待匹配的目标字符串 (如 "https://files.pythonhosted.org/")
	Replacement string // 替换目标字符串模板，支持 {PROXY_HOST} 宏变量
}

// BodyRewriter 响应体动态流式/内存重写器 (Feature 311)
type BodyRewriter struct {
	Rules []RewriteRule
	Types map[string]TypeDefinition
}

// NewBodyRewriter 创建重写器实例
func NewBodyRewriter(rules []RewriteRule, types map[string]TypeDefinition) *BodyRewriter {
	return &BodyRewriter{
		Rules: rules,
		Types: types,
	}
}

// MatchRules 匹配适用于当前请求路径的重写规则作用域
func (rw *BodyRewriter) MatchRules(reqPath, uri string) []RewriteRule {
	if rw == nil || len(rw.Rules) == 0 {
		return nil
	}
	normPath := reqPath
	if !strings.HasPrefix(normPath, "/") {
		normPath = "/" + normPath
	}
	trimmedNormPath := strings.TrimRight(normPath, "/")
	var matched []RewriteRule
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

// isKnownBinaryPath 检查 URL/路径是否显式为已知二进制安装包或压缩归档
func isKnownBinaryPath(path string) bool {
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

// isBinaryMimeType 检查 Content-Type 是否属于二进制类型 (严禁重写)
func isBinaryMimeType(contentType string) bool {
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

// isTextualMimeType 判断 Content-Type 是否为可安全重写的文本类型 (避免误改二进制流)
func isTextualMimeType(contentType string) bool {
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
		// 如果请求是已知二进制包 (如 .tgz, .whl, .deb)
		if isKnownBinaryPath(cleanPath) {
			// 仅当规则显式声明了该后缀时才可能匹配
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
					// 兼容 npm 生态包名点号 (socket.io, highlight.js) 以及语义化版本号 (4.17.21)
					if typeName == "npm" || isNumericOrVersion(base) {
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
func (rw *BodyRewriter) RuleMatchesResponse(r RewriteRule, reqPath, uri, contentType string) bool {
	ct := strings.ToLower(contentType)
	cleanPath := strings.ToLower(reqPath)
	if idx := strings.IndexAny(cleanPath, "?#"); idx != -1 {
		cleanPath = cleanPath[:idx]
	}

	// 二进制保护：若响应为二进制 MIME，除非显式配置，否则直接拒绝
	if isBinaryMimeType(ct) {
		return false
	}

	spec := strings.ToLower(strings.TrimSpace(r.TypeSpec))

	// 缺省或通配符：匹配所有安全文本 MIME 类型
	if spec == "" || spec == "*" || spec == "all" {
		return isTextualMimeType(contentType)
	}

	// 1. type:<name> (类型别名)
	if strings.HasPrefix(spec, "type:") {
		typeName := strings.TrimPrefix(spec, "type:")
		tDef, ok := rw.Types[typeName]
		if !ok {
			return isTextualMimeType(contentType)
		}
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

	// 2. mime:<pattern> (直接匹配 Content-Type)
	if strings.HasPrefix(spec, "mime:") {
		pattern := strings.TrimPrefix(spec, "mime:")
		if strings.HasSuffix(pattern, "*") {
			return strings.HasPrefix(ct, strings.TrimSuffix(pattern, "*"))
		}
		return strings.Contains(ct, pattern)
	}

	// 3. suffix:<ext> (直接匹配文件后缀)
	if strings.HasPrefix(spec, "suffix:") {
		ext := strings.TrimPrefix(spec, "suffix:")
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		return strings.HasSuffix(cleanPath, ext)
	}

	// 容错：若未带 type: 但命中已声明类型别名
	if tDef, ok := rw.Types[spec]; ok {
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

	return isTextualMimeType(contentType)
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

// ProxyConfig 统一代理配置结构
type ProxyConfig struct {
	Aliases      map[string]string         // 短别名映射 (Feature 306): short_alias -> upstream_base_url
	Forwards     map[string]string         // 域名转发映射 (Feature 103): source_host -> gateway_base_url
	NoCacheRules []NoCacheRule             // 免缓存与白名单例外规则 (Feature 305)
	Types        map[string]TypeDefinition // 内容类型定义 (Feature 311): type_name -> TypeDefinition
	RewriteRules []RewriteRule             // 响应体动态重写规则 (Feature 311)
	Rewriter     *BodyRewriter             // 重写器实例 (Feature 311)
	SourceFile   string                    // 实际生效的配置文件路径或说明
}

// NewProxyConfig 初始化统一配置对象
func NewProxyConfig() *ProxyConfig {
	cfg := &ProxyConfig{
		Aliases:      make(map[string]string),
		Forwards:     make(map[string]string),
		NoCacheRules: make([]NoCacheRule, 0),
		Types:        defaultTypeDefinitions(),
		RewriteRules: make([]RewriteRule, 0),
	}
	cfg.Rewriter = NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	return cfg
}

// parseProxyConfig 解析 proxy.conf 统一配置流
func parseProxyConfig(r io.Reader) (*ProxyConfig, error) {
	cfg := NewProxyConfig()
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		directive := strings.ToLower(fields[0])
		switch directive {
		case "alias":
			if len(fields) >= 3 {
				name := strings.TrimPrefix(fields[1], "/")
				cfg.Aliases[name] = fields[2]
			} else {
				log.Printf("invalid alias directive (expected 'alias <name> <upstream_url>'): %s", line)
			}
		case "forward":
			if len(fields) >= 3 {
				cfg.Forwards[fields[1]] = fields[2]
			} else {
				log.Printf("invalid forward directive (expected 'forward <host> <gateway_url>'): %s", line)
			}
		case "bypass", "nocache":
			if len(fields) >= 4 {
				rule := NoCacheRule{
					Scope:   fields[1],
					Action:  "bypass",
					Type:    strings.ToLower(fields[2]),
					Pattern: strings.Join(fields[3:], " "),
				}
				if rule.Type == "regex" {
					re, err := regexp.Compile(rule.Pattern)
					if err != nil {
						log.Printf("invalid regex in bypass rule: %s: %v", rule.Pattern, err)
						continue
					}
					rule.Regexp = re
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else if len(fields) == 3 && fields[2] == "*" {
				rule := NoCacheRule{
					Scope:   fields[1],
					Action:  "bypass",
					Type:    "*",
					Pattern: "*",
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else {
				log.Printf("invalid bypass directive: %s", line)
			}
		case "cache":
			if len(fields) >= 4 {
				rule := NoCacheRule{
					Scope:   fields[1],
					Action:  "cache",
					Type:    strings.ToLower(fields[2]),
					Pattern: strings.Join(fields[3:], " "),
				}
				if rule.Type == "regex" {
					re, err := regexp.Compile(rule.Pattern)
					if err != nil {
						log.Printf("invalid regex in cache rule: %s: %v", rule.Pattern, err)
						continue
					}
					rule.Regexp = re
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else if len(fields) == 3 && fields[2] == "*" {
				rule := NoCacheRule{
					Scope:   fields[1],
					Action:  "cache",
					Type:    "*",
					Pattern: "*",
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else {
				log.Printf("invalid cache directive: %s", line)
			}
		case "type":
			if len(fields) >= 2 {
				name := strings.ToLower(fields[1])
				tDef, exists := cfg.Types[name]
				if !exists {
					tDef = TypeDefinition{Name: name}
				}
				for _, spec := range fields[2:] {
					lowerSpec := strings.ToLower(spec)
					if strings.HasPrefix(lowerSpec, "mime:") {
						val := spec[5:]
						tDef.Mimes = append(tDef.Mimes, strings.ToLower(val))
					} else if strings.HasPrefix(lowerSpec, "suffix:") {
						ext := spec[7:]
						if !strings.HasPrefix(ext, ".") {
							ext = "." + ext
						}
						tDef.Suffixes = append(tDef.Suffixes, strings.ToLower(ext))
					} else {
						if strings.Contains(spec, "/") {
							tDef.Mimes = append(tDef.Mimes, strings.ToLower(spec))
						} else {
							ext := spec
							if !strings.HasPrefix(ext, ".") {
								ext = "." + ext
							}
							tDef.Suffixes = append(tDef.Suffixes, strings.ToLower(ext))
						}
					}
				}
				cfg.Types[name] = tDef
			} else {
				log.Printf("invalid type directive (expected 'type <name> <matcher>...'): %s", line)
			}
		case "rewrite":
			if len(fields) >= 5 {
				rule := RewriteRule{
					Scope:       fields[1],
					TypeSpec:    fields[2],
					Pattern:     fields[3],
					Replacement: fields[4],
				}
				cfg.RewriteRules = append(cfg.RewriteRules, rule)
			} else if len(fields) == 4 {
				rule := RewriteRule{
					Scope:       fields[1],
					TypeSpec:    "*",
					Pattern:     fields[2],
					Replacement: fields[3],
				}
				cfg.RewriteRules = append(cfg.RewriteRules, rule)
			} else {
				log.Printf("invalid rewrite directive (expected 'rewrite <scope> [type_spec] <pattern> <replacement>'): %s", line)
			}
		default:
			log.Printf("unrecognized config directive: %s", line)
		}
	}
	cfg.Rewriter = NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	return cfg, scanner.Err()
}

// logProxyConfigSummary 打印统一配置加载汇总日志
func logProxyConfigSummary(cfg *ProxyConfig) {
	log.Printf("proxy configuration summary [%s]: %d aliases, %d forwards, %d cache/bypass rules, %d types, %d rewrite rules",
		cfg.SourceFile, len(cfg.Aliases), len(cfg.Forwards), len(cfg.NoCacheRules), len(cfg.Types), len(cfg.RewriteRules))
}

// loadProxyConfig 加载统一代理配置，支持多级自动探测机制
func loadProxyConfig(confPath string) *ProxyConfig {
	// 1. 若用户在命令行显式指定了自定义路径（且不是默认未指定的 "proxy.conf"）
	if confPath != "" && confPath != "proxy.conf" {
		if f, err := os.Open(confPath); err == nil {
			defer f.Close()
			cfg, err := parseProxyConfig(f)
			if err == nil {
				cfg.SourceFile = confPath
				log.Printf("proxy config loaded from explicit CLI argument: %s", confPath)
				logProxyConfigSummary(cfg)
				return cfg
			}
			log.Printf("failed to parse config file %s: %v", confPath, err)
		} else {
			log.Printf("specified config file not found: %s: %v", confPath, err)
		}
	}

	// 2. 自动探测 proxy.conf (优先当前目录，次选二进制所在目录)
	searchPaths := []string{"proxy.conf"}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		searchPaths = append(searchPaths, filepath.Join(exeDir, "proxy.conf"))
	}

	for _, p := range searchPaths {
		if f, err := os.Open(p); err == nil {
			defer f.Close()
			cfg, err := parseProxyConfig(f)
			if err == nil {
				cfg.SourceFile = p
				log.Printf("proxy config file loaded: %s", p)
				logProxyConfigSummary(cfg)
				return cfg
			}
			log.Printf("failed to parse config file %s: %v", p, err)
		}
	}

	// 3. 未找到配置文件，使用默认空配置
	cfg := NewProxyConfig()
	cfg.SourceFile = "(none)"
	log.Printf("no proxy configuration found, using empty default configuration")
	return cfg
}

func cacheConfig() {
	//默认使用临时目录
	if cache == nil || *cache == "" {
		*cache = os.TempDir()
	}

	//打印缓存目录
	log.Printf("cache dir: %s", *cache)

	//缓存目录是否存在
	_, err := os.Stat(*cache)
	if err != nil {
		log.Fatal("cache directory does not exist")
	}
}

//initWatchdog 初始化后台巡检，清理超时挂起的任务
func initWatchdog() {
	if *idleTime <= 0 {
		*idleTime = 10 * time.Minute
	}

	// 内部自动推导巡检时间：为超时时间的 1/3，以保证及时发现且不占用过多资源
	checkTime := *idleTime / 3

	log.Printf("watchdog started: idle-time=%v, internal-check-time=%v", *idleTime, checkTime)

	go func() {
		ticker := time.NewTicker(checkTime)
		defer ticker.Stop()
		for range ticker.C {
			jobsMu.Lock()
			now := time.Now()
			for uri, job := range jobs {
				job.Mu.Lock()
				// 如果超过设定的空闲时间没有数据读写，并且请求还未完成
				if now.Sub(job.LastActive) > *idleTime {
					if job.Cancel != nil {
						log.Printf("watchdog: killing idle job: %s, inactive for %v", uri, now.Sub(job.LastActive))
						job.Cancel()
						// 注意：不要在这里直接 delete(jobs, uri)
						// Cancel() 会触发 Leader 的 Read 返回 context.Canceled 错误
						// 随后 Leader 会退出循环并执行其 defer 块中的 removeJob 和资源清理
					}
				}
				job.Mu.Unlock()
			}
			jobsMu.Unlock()
		}
	}()
}

//main 程序入口
func main() {
	//解析命令行
	flag.Parse()

	//缓存目录
	cacheConfig()

	//加载统一代理配置
	proxyConfig := loadProxyConfig(*conf)

	// 启动后台巡检
	initWatchdog()

	//HTTP Server
	server := &http.Server{
		Addr: *addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Handler(w, r, proxyConfig)
		}),
	}
	log.Printf("http server: %s", *addr)
	log.Fatal(server.ListenAndServe())
}
