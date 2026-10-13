package proxy

import (
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

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
)

// handleDiskTTL 处理基于磁盘文件与 mtime 的时效缓存 (Feature 332)
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

	// 2. 文件不存在或已超出 TTL：进入请求合并与条件回源管线
	job, isLeader := s.downMgr.GetOrCreate(uri)

	if isLeader {
		log.Printf("[%d] disk ttl job leader: %s", reqID, uri)
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

			log.Printf("[%d] disk ttl upstream fetch: %s", reqID, targetUri)

			ctx, cancel := context.WithCancel(context.Background())
			job.Cancel = cancel
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, r.Method, targetUri, nil)
			if err != nil {
				log.Printf("[%d] disk ttl req create err: %v", reqID, err)
				if fileExists {
					log.Printf("[%d] disk ttl req err, falling back to stale disk file: %s", reqID, uri)
					job.StatusCode = http.StatusOK
					job.Header.Set("X-Cache-Status", "STALE-FALLBACK")
					job.FilePath = name
					close(job.HeaderDone)
					return
				}
				job.Err = err
				close(job.HeaderDone)
				return
			}

			// 转发请求头
			for k, vv := range r.Header {
				for _, v := range vv {
					req.Header.Add(k, v)
				}
			}

			// 若存在旧文件，附加条件重验证头部
			if fileExists {
				req.Header.Set("If-Modified-Since", stat.ModTime().UTC().Format(http.TimeFormat))
			}

			resp, err := s.doRequest(req)
			if err != nil {
				log.Printf("[%d] disk ttl upstream do err: %v", reqID, err)
				if fileExists {
					log.Printf("[%d] disk ttl network err, falling back to stale disk file: %s", reqID, uri)
					job.StatusCode = http.StatusOK
					job.Header.Set("X-Cache-Status", "STALE-FALLBACK")
					job.FilePath = name
					close(job.HeaderDone)
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

			// 2.1 上游返回 5xx 故障，且本地有旧文件：容灾降级
			if resp.StatusCode >= 500 && fileExists {
				log.Printf("[%d] disk ttl upstream returned %d, falling back to stale disk file: %s", reqID, resp.StatusCode, uri)
				job.StatusCode = http.StatusOK
				job.Header.Set("X-Cache-Status", "STALE-FALLBACK")
				job.FilePath = name
				close(job.HeaderDone)
				return
			}

			// 2.2 上游返回 304 Not Modified：刷新文件 mtime
			if resp.StatusCode == http.StatusNotModified && fileExists {
				now := time.Now()
				_ = os.Chtimes(name, now, now)
				log.Printf("[%d] disk ttl 304 revalidated, lease renewed: %s", reqID, uri)
				job.StatusCode = http.StatusOK
				job.Header.Set("X-Cache-Status", "REVALIDATED-304")
				job.Header.Set("Last-Modified", now.UTC().Format(http.TimeFormat))
				job.FilePath = name
				close(job.HeaderDone)
				return
			}

			// 2.3 非 200 响应（如 404/401/403 等）：按 Feature 331 纯流式透传，零落盘
			if resp.StatusCode != http.StatusOK {
				log.Printf("[%d] disk ttl upstream non-200 (%d): %s (skip disk caching)", reqID, resp.StatusCode, uri)
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

			// 2.4 上游返回 200 OK：全新写入或覆盖写入磁盘文件
			job.StatusCode = http.StatusOK
			copyHeader(job.Header, resp.Header)
			job.Header.Set("X-Cache-Status", "MISS-DISK-TTL")

			dir := filepath.Dir(name)
			_ = os.MkdirAll(dir, 0755)

			tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)
			job.FinalName = name
			job.TmpName = tmp

			file, err := os.Create(tmp)
			if err != nil {
				log.Printf("[%d] disk ttl file create err: %v", reqID, err)
				job.Err = err
				close(job.HeaderDone)
				return
			}

			job.FilePath = tmp
			close(job.HeaderDone)

			buf := make([]byte, 32*1024)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					job.Mu.Lock()
					job.LastActive = time.Now()
					job.Mu.Unlock()

					_, wErr := file.Write(buf[:n])
					if wErr != nil {
						log.Printf("[%d] disk ttl file write err: %v", reqID, wErr)
						job.Err = wErr
						break
					}
					job.Cond.L.Lock()
					job.Cond.Broadcast()
					job.Cond.L.Unlock()
				}
				if err != nil {
					if err != io.EOF && err != context.Canceled {
						log.Printf("[%d] disk ttl read err: %v", reqID, err)
						job.Err = err
					}
					break
				}
			}
			_ = file.Close()
			job.TryRename(reqID)
		}()
	} else {
		log.Printf("[%d] disk ttl job follower: %s", reqID, uri)
	}

	<-job.HeaderDone

	if job.Err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// 复制并写入响应头
	copyHeader(w.Header(), job.Header)

	// 非 200 响应流式透传分支
	if job.StatusCode != http.StatusOK {
		w.WriteHeader(job.StatusCode)
		if isLeader {
			s.downMgr.Remove(uri)
			if job.Body != nil {
				_, _ = io.Copy(w, job.Body)
				_ = job.Body.Close()
			}
			close(job.StreamDone)
		}
		return
	}

	// 检查客户端条件请求（针对 REVALIDATED-304 分支）
	if job.Header.Get("X-Cache-Status") == "REVALIDATED-304" {
		if ims := r.Header.Get("If-Modified-Since"); ims != "" {
			if stat != nil {
				if t, parseErr := http.ParseTime(ims); parseErr == nil {
					if !stat.ModTime().After(t.Add(time.Second)) {
						w.WriteHeader(http.StatusNotModified)
						return
					}
				}
			}
		}
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(job.StatusCode)

	if r.Method == http.MethodHead {
		return
	}

	atomic.AddInt32(&job.Readers, 1)

	file, err := os.Open(job.FilePath)
	if err != nil && job.FinalName != "" {
		file, err = os.Open(job.FinalName)
	}
	if err != nil {
		log.Printf("[%d] disk ttl file open err: %v", reqID, err)
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
				return
			}
		}
		if err == io.EOF {
			select {
			case <-job.Done:
				return
			default:
				job.Cond.L.Lock()
				job.Cond.Wait()
				job.Cond.L.Unlock()
			}
		} else if err != nil {
			return
		}
	}
}