package proxy

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"uri-proxy/internal/cache"
)

func (s *Server) handleHead(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
) {
	cachePath := filepath.Join(s.cacheDir, cache.GetSafeUrlPath(uri))
	if stat, err := os.Stat(cachePath); err == nil && !stat.IsDir() {
		log.Printf("[%d] head hit local cache: %s", reqID, uri)
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		return
	}

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

	resp, err := s.doRequest(req)
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
}

func (s *Server) handleRange(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
) {
	cachePath := filepath.Join(s.cacheDir, cache.GetSafeUrlPath(uri))
	if stat, err := os.Stat(cachePath); err == nil && !stat.IsDir() {
		log.Printf("[%d] range hit local cache: %s (%s)", reqID, uri, r.Header.Get("Range"))
		http.ServeFile(w, r, cachePath)
		return
	}

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

	rangeRelayHeaders := []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match", "User-Agent", "Authorization"}
	for _, h := range rangeRelayHeaders {
		if val := r.Header.Get(h); val != "" {
			req.Header.Set(h, val)
		}
	}

	resp, err := s.doRequest(req)
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
}
