package proxy

import (
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"uri-proxy/internal/config"
)

func (s *Server) handleBypass(
	w http.ResponseWriter,
	r *http.Request,
	reqID uint64,
	uris *url.URL,
	uri string,
	forwards map[string]string,
	matchedBypass *config.NoCacheRule,
	hasRewriteMatch bool,
) {
	if hasRewriteMatch {
		log.Printf("[%d] dynamic body rewrite target: %s", reqID, uri)
	} else if matchedBypass != nil && matchedBypass.TTL > 0 {
		log.Printf("[%d] cache bypass with soft ttl (%v): %s", reqID, matchedBypass.TTL, uri)
	} else {
		log.Printf("[%d] cache bypass (dynamic resource): %s", reqID, uri)
	}

	// Feature 318: 软过期与 SWR 缓存调度
	if matchedBypass != nil && matchedBypass.TTL > 0 && !hasRewriteMatch && (r.Method == http.MethodGet || r.Method == http.MethodHead) && s.ttlMgr != nil {
		cachedItem, fresh := s.ttlMgr.Get(uri)
		if fresh && cachedItem != nil {
			copyHeader(w.Header(), cachedItem.Header)
			w.Header().Set("X-Cache-Status", "HIT-SOFT-TTL")
			w.WriteHeader(cachedItem.StatusCode)
			if r.Method != http.MethodHead {
				_, _ = w.Write(cachedItem.Data)
			}
			log.Printf("[%d] soft ttl hit (%s remaining): %s", reqID, time.Until(cachedItem.ExpiresAt).Round(time.Millisecond), uri)
			return
		}

		targetUri := uri
		if _, ok := forwards[uris.Host]; ok {
			targetUri = forwards[uris.Host] + "/" + uri
		}

		req, err := http.NewRequestWithContext(r.Context(), r.Method, targetUri, nil)
		if err != nil {
			log.Printf("[%d] soft ttl req create err: %v", reqID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		for k, vv := range r.Header {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}

		if cachedItem != nil {
			if cachedItem.ETag != "" && req.Header.Get("If-None-Match") == "" {
				req.Header.Set("If-None-Match", cachedItem.ETag)
			}
			if cachedItem.LastModified != "" && req.Header.Get("If-Modified-Since") == "" {
				req.Header.Set("If-Modified-Since", cachedItem.LastModified)
			}
		}

		resp, err := s.doRequest(req)
		if err != nil {
			log.Printf("[%d] soft ttl upstream fetch err: %v", reqID, err)
			if cachedItem != nil {
				copyHeader(w.Header(), cachedItem.Header)
				w.Header().Set("X-Cache-Status", "STALE-FALLBACK")
				w.WriteHeader(cachedItem.StatusCode)
				if r.Method != http.MethodHead {
					_, _ = w.Write(cachedItem.Data)
				}
				return
			}
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotModified && cachedItem != nil {
			s.ttlMgr.Refresh(uri, matchedBypass.TTL)
			copyHeader(w.Header(), cachedItem.Header)
			w.Header().Set("X-Cache-Status", "REVALIDATED-304")

			if r.Header.Get("If-None-Match") == cachedItem.ETag || (r.Header.Get("If-Modified-Since") != "" && r.Header.Get("If-Modified-Since") == cachedItem.LastModified) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.WriteHeader(cachedItem.StatusCode)
			if r.Method != http.MethodHead {
				_, _ = w.Write(cachedItem.Data)
			}
			log.Printf("[%d] soft ttl 304 revalidated (lease extended %v): %s", reqID, matchedBypass.TTL, uri)
			return
		}

		if resp.StatusCode == http.StatusOK {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err == nil {
				s.ttlMgr.Set(uri, bodyBytes, resp.Header, resp.StatusCode, matchedBypass.TTL)
				copyHeader(w.Header(), resp.Header)
				w.Header().Set("X-Cache-Status", "MISS-SOFT-TTL")
				w.WriteHeader(resp.StatusCode)
				if r.Method != http.MethodHead {
					_, _ = w.Write(bodyBytes)
				}
				log.Printf("[%d] soft ttl cached 200 (ttl=%v): %s", reqID, matchedBypass.TTL, uri)
				return
			}
		}

		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
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

	bypassForwardHeaders := []string{"If-Modified-Since", "If-None-Match", "Range", "If-Range", "Cache-Control", "User-Agent", "Accept", "Accept-Language", "Authorization"}
	for _, h := range bypassForwardHeaders {
		if val := r.Header.Get(h); val != "" {
			req.Header.Set(h, val)
		}
	}

	if hasRewriteMatch {
		req.Header.Set("Accept-Encoding", "identity")
	}

	resp, err := s.doRequest(req)
	if err != nil {
		log.Printf("[%d] upstream bypass get err: %s", reqID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	copyHeader(w.Header(), resp.Header)

	if s.rewriter != nil && s.rewriter.ShouldRewrite(r.URL.Path, uri, resp.Header.Get("Content-Type")) && resp.StatusCode == http.StatusOK {
		clientHost := r.Host
		if clientHost == "" {
			clientHost = r.URL.Host
		}
		if clientHost == "" {
			clientHost = s.addr
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

		rewrittenBody := s.rewriter.Apply(clientHost, r.URL.Path, uri, resp.Header.Get("Content-Type"), rawBody)

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
}
