package proxy

import (
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"

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

	relayRequestHeaders(req, r)
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
		w.Header().Del("Digest")
		w.Header().Del("Accept-Ranges")
		w.Header().Del("Content-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(len(rewrittenBody)))

		log.Printf("[%d] body rewritten (%d -> %d bytes): %s", reqID, len(rawBody), len(rewrittenBody), uri)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(rewrittenBody)
		return
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
