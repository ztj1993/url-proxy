package proxy

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/downloader"
)

// staticDownloadPolicy 常规静态持久化制品下载策略 (Feature 338)
type staticDownloadPolicy struct{}

func (p *staticDownloadPolicy) prepareRequest(ctx context.Context, r *http.Request, targetUri string, stat os.FileInfo, fileExists bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetUri, nil)
	if err != nil {
		return nil, err
	}
	jobForwardHeaders := []string{"If-Modified-Since", "If-None-Match", "Authorization", "User-Agent"}
	for _, h := range jobForwardHeaders {
		if val := r.Header.Get(h); val != "" {
			req.Header.Set(h, val)
		}
	}
	return req, nil
}

func (p *staticDownloadPolicy) handleRequestError(reqID uint64, uri string, err error, fileExists bool, name string, job *downloader.DownloadJob) bool {
	return false
}

func (p *staticDownloadPolicy) handleNetworkError(reqID uint64, uri string, err error, fileExists bool, name string, job *downloader.DownloadJob) bool {
	return false
}

func (p *staticDownloadPolicy) handleResponse(reqID uint64, uri string, name string, resp *http.Response, stat os.FileInfo, fileExists bool, job *downloader.DownloadJob) bool {
	job.StatusCode = resp.StatusCode
	copyHeader(job.Header, resp.Header)

	if resp.StatusCode == http.StatusNotModified {
		_ = resp.Body.Close()
		log.Printf("[%d] job not modified (304): %s", reqID, uri)
		close(job.HeaderDone)
		return true
	}

	if resp.StatusCode != http.StatusOK {
		return false
	}

	length, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if stat != nil && stat.Size() == length {
		log.Printf("[%d] job cached: %s", reqID, uri)
		job.FilePath = name
		close(job.HeaderDone)
		return true
	}

	return false
}

func (p *staticDownloadPolicy) onOKResponse(job *downloader.DownloadJob) {}

func (p *staticDownloadPolicy) checkClientConditional(w http.ResponseWriter, r *http.Request, stat os.FileInfo, job *downloader.DownloadJob) bool {
	return false
}

// handleDownload 处理请求合并与并发静态资源下载 (Feature 338 统合管线)
func (s *Server) handleDownload(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
) {
	name := filepath.Join(s.cacheDir, cache.GetSafeUrlPath(uri))
	stat, err := os.Stat(name)
	fileExists := err == nil && !stat.IsDir()

	s.executeCoalescedPipeline(w, r, reqID, uris, uri, forwards, name, stat, fileExists, &staticDownloadPolicy{})
}
