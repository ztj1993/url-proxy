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
)

func (s *Server) handleDownload(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
) {
	job, isLeader := s.downMgr.GetOrCreate(uri)

	if isLeader {
		log.Printf("[%d] job leader: %s", reqID, uri)
		go func() {
			defer s.downMgr.Remove(uri)
			defer func() {
				job.Cond.L.Lock()
				job.Cond.Broadcast()
				job.Cond.L.Unlock()
			}()
			defer close(job.Done)

			name := filepath.Join(s.cacheDir, cache.GetSafeUrlPath(uri))
			stat, _ := os.Stat(name)

			targetUri := uri
			if _, ok := forwards[uris.Host]; ok {
				targetUri = forwards[uris.Host] + "/" + uri
			}

			log.Printf("[%d] job download: %s", reqID, targetUri)

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

			jobForwardHeaders := []string{"If-Modified-Since", "If-None-Match", "Authorization", "User-Agent"}
			for _, h := range jobForwardHeaders {
				if val := r.Header.Get(h); val != "" {
					req.Header.Set(h, val)
				}
			}

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

			job.StatusCode = resp.StatusCode
			copyHeader(job.Header, resp.Header)

			if resp.StatusCode == http.StatusNotModified {
				log.Printf("[%d] job not modified (304): %s", reqID, uri)
				close(job.HeaderDone)
				return
			}

			length, _ := strconv.ParseInt(
				resp.Header.Get("Content-Length"), 10, 64,
			)

			if stat != nil && stat.Size() == length {
				log.Printf("[%d] job cached: %s", reqID, uri)
				job.FilePath = name
				close(job.HeaderDone)
				return
			}

			dir := filepath.Dir(name)
			_ = os.MkdirAll(dir, 0755)

			tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)

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
						log.Printf("[%d] file write err: %s", reqID, wErr)
						job.Err = wErr
						break
					}
					job.Cond.L.Lock()
					job.Cond.Broadcast()
					job.Cond.L.Unlock()
				}
				if err != nil {
					if err == context.Canceled {
						log.Printf("[%d] job cancelled due to idle timeout: %s", reqID, uri)
					} else if err != io.EOF {
						log.Printf("[%d] job read err: %s", reqID, err)
						job.Err = err
					}
					break
				}
			}
			log.Printf("[%d] job finished: %s", reqID, uri)
			_ = file.Close()

			job.TryRename(reqID)
		}()
	} else {
		log.Printf("[%d] job follower: %s", reqID, uri)
	}

	<-job.HeaderDone

	if job.Err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	copyHeader(w.Header(), job.Header)
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(job.StatusCode)

	if job.StatusCode == http.StatusNotModified {
		log.Printf("[%d] 304 response sent: %s", reqID, uri)
		return
	}

	atomic.AddInt32(&job.Readers, 1)

	file, err := os.Open(job.FilePath)
	if err != nil {
		log.Printf("[%d] file open err: %s", reqID, err)
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
			log.Printf("[%d] read err: %s", reqID, err)
			return
		}
	}
}
