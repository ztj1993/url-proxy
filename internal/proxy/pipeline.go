package proxy

import (
	"compress/gzip"
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"uri-proxy/internal/downloader"
)

// fetchPolicy 定义不同回源与缓存场景的策略接口 (Feature 338)
type fetchPolicy interface {
	// prepareRequest 在 Leader 构建上游 HTTP 请求时定制 Method 与 Headers
	prepareRequest(ctx context.Context, r *http.Request, targetUri string, stat os.FileInfo, fileExists bool) (*http.Request, error)
	// handleRequestError 处理请求创建失败，返回 true 表示已处理并短路
	handleRequestError(reqID uint64, uri string, err error, fileExists bool, name string, job *downloader.DownloadJob) bool
	// handleNetworkError 处理网络回源失败，返回 true 表示已处理并短路
	handleNetworkError(reqID uint64, uri string, err error, fileExists bool, name string, job *downloader.DownloadJob) bool
	// handleResponse 处理上游初始响应（304、5xx 降级、已有静态文件复用等），返回 true 表示已处理并短路
	handleResponse(reqID uint64, uri string, name string, resp *http.Response, stat os.FileInfo, fileExists bool, job *downloader.DownloadJob) bool
	// onOKResponse 在上游 200 OK 且确认落盘前修饰 Header（如注入 X-Cache-Status）
	onOKResponse(job *downloader.DownloadJob)
	// checkClientConditional 向客户端回写前检查客户端条件请求（如 304），返回 true 表示已短路响应
	checkClientConditional(w http.ResponseWriter, r *http.Request, stat os.FileInfo, job *downloader.DownloadJob) bool
}

// executeCoalescedPipeline 统合并发请求合并、Leader 回源落盘与多路广播追读 (Feature 338)
func (s *Server) executeCoalescedPipeline(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
	name string,
	stat os.FileInfo,
	fileExists bool,
	policy fetchPolicy,
) {
	job, isLeader := s.downMgr.GetOrCreate(uri)

	if isLeader {
		log.Printf("[%d] coalesced job leader: %s", reqID, uri)
		go func() {
			defer s.downMgr.Remove(uri)
			defer func() {
				job.Cond.L.Lock()
				job.Cond.Broadcast()
				job.Cond.L.Unlock()
			}()
			defer close(job.Done)

			targetUri := uri
			if _, ok := forwards[uris.Host]; ok {
				targetUri = forwards[uris.Host] + "/" + uri
			}

			log.Printf("[%d] upstream fetch: %s", reqID, targetUri)

			ctx, cancel := context.WithCancel(context.Background())
			job.Cancel = cancel
			defer cancel()

			req, err := policy.prepareRequest(ctx, r, targetUri, stat, fileExists)
			if err != nil {
				log.Printf("[%d] req create err: %v", reqID, err)
				if policy.handleRequestError(reqID, uri, err, fileExists, name, job) {
					return
				}
				job.Err = err
				close(job.HeaderDone)
				return
			}

			resp, err := s.doRequest(req)
			if err != nil {
				log.Printf("[%d] upstream do err: %v", reqID, err)
				if policy.handleNetworkError(reqID, uri, err, fileExists, name, job) {
					return
				}
				job.Err = err
				close(job.HeaderDone)
				return
			}
			defer func() {
				if resp.Body != nil {
					_ = resp.Body.Close()
				}
			}()

			// 策略拦截与决策 (304 续期、5xx 旧文件降级、已有完整文件跳过等)
			if policy.handleResponse(reqID, uri, name, resp, stat, fileExists, job) {
				return
			}

			// 非 200 响应按 Feature 331 纯流式透传，零落盘
			if resp.StatusCode != http.StatusOK {
				log.Printf("[%d] upstream non-200 (%d): %s (skip disk caching)", reqID, resp.StatusCode, uri)
				s.downMgr.Remove(uri)
				job.StatusCode = resp.StatusCode
				copyHeader(job.Header, resp.Header)
				job.Body = resp.Body
				close(job.HeaderDone)
				select {
				case <-job.StreamDone:
				case <-ctx.Done():
				}
				return
			}

			// 200 OK: 策略修饰并启动流式落盘与广播泵
			job.StatusCode = http.StatusOK
			copyHeader(job.Header, resp.Header)
			policy.onOKResponse(job)

			s.streamUpstreamToFileAndBroadcast(reqID, uri, resp, name, job)
		}()
	} else {
		log.Printf("[%d] coalesced job follower: %s", reqID, uri)
	}

	<-job.HeaderDone

	if job.Err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	copyHeader(w.Header(), job.Header)

	// 上游 304 且无文件返回短路 (静态下载场景)
	if job.StatusCode == http.StatusNotModified {
		w.WriteHeader(http.StatusNotModified)
		log.Printf("[%d] 304 response sent: %s", reqID, uri)
		return
	}

	// 非 200 响应流式透传分支 (零落盘)
	if job.StatusCode != http.StatusOK {
		w.WriteHeader(job.StatusCode)
		if isLeader {
			s.downMgr.Remove(uri)
			if job.Body != nil {
				_, _ = io.Copy(w, job.Body)
				_ = job.Body.Close()
			}
			close(job.StreamDone)
			log.Printf("[%d] non-200 stream forwarded (%d): %s", reqID, job.StatusCode, uri)
		} else {
			log.Printf("[%d] follower non-200 response (%d) sent: %s", reqID, job.StatusCode, uri)
		}
		return
	}

	// 检查客户端条件请求
	if policy.checkClientConditional(w, r, stat, job) {
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(job.StatusCode)

	if r.Method == http.MethodHead {
		return
	}

	// 启动统一客户端流式追读消费泵
	s.pumpFileToClient(w, reqID, uri, job)
}

// streamUpstreamToFileAndBroadcast 负责 32KB 循环将上游数据边写临时文件边广播 (Feature 338)
func (s *Server) streamUpstreamToFileAndBroadcast(
	reqID uint64,
	uri string,
	resp *http.Response,
	name string,
	job *downloader.DownloadJob,
) {
	dir := filepath.Dir(name)
	_ = os.MkdirAll(dir, 0755)

	tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)
	job.FinalName = name
	job.TmpName = tmp

	file, err := os.Create(tmp)
	if err != nil {
		log.Printf("[%d] file create err: %v", reqID, err)
		job.Err = err
		close(job.HeaderDone)
		return
	}

	var bodyReader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		log.Printf("[%d] upstream returned forced gzip encoding for static download, decompressing before caching: %s", reqID, uri)
		gz, gzErr := gzip.NewReader(resp.Body)
		if gzErr == nil {
			defer gz.Close()
			bodyReader = gz
			job.Header.Del("Content-Encoding")
			job.Header.Del("Content-Length")
		} else {
			log.Printf("[%d] failed to initialize gzip reader for forced gzip response: %v", reqID, gzErr)
		}
	}

	job.FilePath = tmp
	close(job.HeaderDone)

	buf := make([]byte, 32*1024)
	for {
		n, err := bodyReader.Read(buf)
		if n > 0 {
			job.Mu.Lock()
			job.LastActive = time.Now()
			job.Mu.Unlock()

			_, wErr := file.Write(buf[:n])
			if wErr != nil {
				log.Printf("[%d] file write err: %v", reqID, wErr)
				job.Err = wErr
				break
			}
			job.Cond.L.Lock()
			job.Cond.Broadcast()
			job.Cond.L.Unlock()
		}
		if err != nil {
			if err == context.Canceled {
				log.Printf("[%d] job cancelled: %s", reqID, uri)
				job.Err = err
			} else if err != io.EOF {
				log.Printf("[%d] read err: %v", reqID, err)
				job.Err = err
			}
			break
		}
	}
	_ = file.Close()
	job.TryRename(reqID)
}

// pumpFileToClient 负责客户端 Readers 计数追踪、32KB 追读消费与退出协同 (Feature 338)
func (s *Server) pumpFileToClient(
	w http.ResponseWriter,
	reqID uint64,
	uri string,
	job *downloader.DownloadJob,
) {
	atomic.AddInt32(&job.Readers, 1)

	file, err := os.Open(job.FilePath)
	if err != nil && job.FinalName != "" {
		file, err = os.Open(job.FinalName)
	}
	if err != nil {
		log.Printf("[%d] file open err: %v", reqID, err)
		atomic.AddInt32(&job.Readers, -1)
		return
	}
	defer func() {
		_ = file.Close()
		if atomic.AddInt32(&job.Readers, -1) == 0 {
			select {
			case <-job.Done:
				job.TryRename(reqID)
			default:
			}
		}
	}()

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
			select {
			case <-job.Done:
				if job.Err != nil {
					log.Printf("[%d] job done with error: %v", reqID, job.Err)
				} else {
					log.Printf("[%d] job done: %s", reqID, uri)
				}
				return
			default:
				job.Cond.L.Lock()
				job.Cond.Wait()
				job.Cond.L.Unlock()
			}
		} else if err != nil {
			log.Printf("[%d] read err: %v", reqID, err)
			return
		}
	}
}
