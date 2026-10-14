package proxy

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
)

// diskTTLPolicy 磁盘时效缓存回源与容灾策略 (Feature 338)
type diskTTLPolicy struct{}

func (p *diskTTLPolicy) prepareRequest(ctx context.Context, r *http.Request, targetUri string, stat os.FileInfo, fileExists bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, r.Method, targetUri, nil)
	if err != nil {
		return nil, err
	}
	relayRequestHeaders(req, r)

	// 1. 落盘防压缩守卫：显式剥离客户端 Accept-Encoding，启用 Transport 自动透明解压 (Feature 343)
	req.Header.Del("Accept-Encoding")

	// 2. 剥离客户端 ETag：防止客户端异构 If-None-Match 绑架协商导致 304 退化为 200 全量拉取 (Feature 343)
	req.Header.Del("If-None-Match")

	// 3. 隔离客户端修改时间：本地有缓存时以本地真实 mtime 发起增量续期；本地无缓存时强制剥离以获取 200 OK 建仓 (Feature 343)
	if fileExists {
		req.Header.Set("If-Modified-Since", stat.ModTime().UTC().Format(http.TimeFormat))
	} else {
		req.Header.Del("If-Modified-Since")
	}
	return req, nil
}

func (p *diskTTLPolicy) handleRequestError(reqID uint64, uri string, err error, fileExists bool, name string, job *downloader.DownloadJob) bool {
	if fileExists {
		log.Printf("[%d] disk ttl req err, falling back to stale disk file: %s", reqID, uri)
		job.StatusCode = http.StatusOK
		job.Header.Set("X-Cache-Status", "STALE-FALLBACK")
		job.FilePath = name
		close(job.HeaderDone)
		return true
	}
	return false
}

func (p *diskTTLPolicy) handleNetworkError(reqID uint64, uri string, err error, fileExists bool, name string, job *downloader.DownloadJob) bool {
	if fileExists {
		log.Printf("[%d] disk ttl network err, falling back to stale disk file: %s", reqID, uri)
		job.StatusCode = http.StatusOK
		job.Header.Set("X-Cache-Status", "STALE-FALLBACK")
		job.FilePath = name
		close(job.HeaderDone)
		return true
	}
	return false
}

func (p *diskTTLPolicy) handleResponse(reqID uint64, uri string, name string, resp *http.Response, stat os.FileInfo, fileExists bool, job *downloader.DownloadJob) bool {
	if resp.StatusCode >= 500 && fileExists {
		log.Printf("[%d] disk ttl upstream returned %d, falling back to stale disk file: %s", reqID, resp.StatusCode, uri)
		job.StatusCode = http.StatusOK
		job.Header.Set("X-Cache-Status", "STALE-FALLBACK")
		job.FilePath = name
		close(job.HeaderDone)
		return true
	}

	if resp.StatusCode == http.StatusNotModified && fileExists {
		now := time.Now()
		_ = os.Chtimes(name, now, now)
		log.Printf("[%d] disk ttl 304 revalidated, lease renewed: %s", reqID, uri)
		job.StatusCode = http.StatusOK
		job.Header.Set("X-Cache-Status", "REVALIDATED-304")
		job.Header.Set("Last-Modified", now.UTC().Format(http.TimeFormat))
		job.FilePath = name
		close(job.HeaderDone)
		return true
	}

	if resp.StatusCode != http.StatusOK {
		return false
	}

	return false
}

func (p *diskTTLPolicy) onOKResponse(job *downloader.DownloadJob) {
	job.Header.Set("X-Cache-Status", "MISS-DISK-TTL")
}

func (p *diskTTLPolicy) checkClientConditional(w http.ResponseWriter, r *http.Request, stat os.FileInfo, job *downloader.DownloadJob) bool {
	if job.Header.Get("X-Cache-Status") == "REVALIDATED-304" {
		if ims := r.Header.Get("If-Modified-Since"); ims != "" {
			if stat != nil {
				if t, parseErr := http.ParseTime(ims); parseErr == nil {
					if !stat.ModTime().After(t.Add(time.Second)) {
						w.WriteHeader(http.StatusNotModified)
						return true
					}
				}
			}
		}
	}
	return false
}

// handleDiskTTL 处理基于磁盘文件与 mtime 的时效缓存 (Feature 332 & 338 统合管线)
func (s *Server) handleDiskTTL(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
	matchedBypass *config.NoCacheRule,
) {
	name := filepath.Join(s.cacheDir, cache.GetSafeUrlPath(uri))
	stat, err := os.Stat(name)
	fileExists := err == nil && !stat.IsDir()

	// 1. 保鲜期命中 (HIT-DISK-TTL)：文件存在且仍在 TTL 内
	if fileExists && time.Since(stat.ModTime()) < matchedBypass.TTL {
		log.Printf("[%d] disk ttl fresh hit (%s remaining): %s", reqID, (matchedBypass.TTL - time.Since(stat.ModTime())).Round(time.Millisecond), uri)
		w.Header().Set("X-Cache-Status", "HIT-DISK-TTL")
		w.Header().Set("Last-Modified", stat.ModTime().UTC().Format(http.TimeFormat))
		w.Header().Set("Accept-Ranges", "bytes")

		if ims := r.Header.Get("If-Modified-Since"); ims != "" {
			if t, parseErr := http.ParseTime(ims); parseErr == nil {
				if !stat.ModTime().After(t.Add(time.Second)) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		http.ServeFile(w, r, name)
		return
	}

	// 2. 超出 TTL 或文件不存在：进入统合并发合并与广播管线 (Feature 338)
	s.executeCoalescedPipeline(w, r, reqID, uris, uri, forwards, name, stat, fileExists, &diskTTLPolicy{})
}
