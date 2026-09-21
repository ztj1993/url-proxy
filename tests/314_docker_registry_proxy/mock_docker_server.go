package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

var (
	v2ProbeRequests   uint64
	manifestRequests  uint64
	blobRequests      uint64
	cdnBlobRequests   uint64
	headBlobRequests  uint64
)

const (
	blobDigest  = "sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678"
	blobContent = "docker-registry-v2-mock-layer-blob-binary-payload-data-9876543210"
)

func main() {
	mux := http.NewServeMux()

	// 1. /v2/ 探测与 401 认证握手挑战
	handleV2Probe := func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimRight(r.URL.Path, "/")
		if clean == "/v2" || clean == "/docker/v2" {
			atomic.AddUint64(&v2ProbeRequests, 1)
			w.Header().Set("Www-Authenticate", `Bearer realm="http://127.0.0.1:9995/token",service="127.0.0.1:9995"`)
			w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)
			log.Printf("[mock-docker] 401 challenge sent for %s", r.URL.Path)
			return
		}

		// Manifest 清单
		if strings.Contains(r.URL.Path, "/manifests/") {
			atomic.AddUint64(&manifestRequests, 1)
			auth := r.Header.Get("Authorization")
			if auth != "Bearer mock-docker-token" {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED","message":"invalid token"}]}`)
				return
			}
			if r.Header.Get("If-None-Match") == `"ubuntu-2204-etag"` {
				w.WriteHeader(http.StatusNotModified)
				log.Printf("[mock-docker] 304 not modified for %s", r.URL.Path)
				return
			}

			manifestJson := fmt.Sprintf(`{
  "schemaVersion": 2,
  "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
  "config": {
    "mediaType": "application/vnd.docker.container.image.v1+json",
    "size": 1470,
    "digest": "sha256:mockconfigdigest"
  },
  "layers": [
    {
      "mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
      "size": %d,
      "digest": "%s"
    }
  ]
}`, len(blobContent), blobDigest)

			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.Header().Set("ETag", `"ubuntu-2204-etag"`)
			w.Header().Set("Docker-Content-Digest", "sha256:manifestdigest")
			w.Header().Set("Content-Length", strconv.Itoa(len(manifestJson)))
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, manifestJson)
			log.Printf("[mock-docker] manifest served for %s", r.URL.Path)
			return
		}

		// Blobs 分层
		if strings.Contains(r.URL.Path, "/blobs/") {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer mock-docker-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if r.Method == http.MethodHead {
				atomic.AddUint64(&headBlobRequests, 1)
				w.Header().Set("Content-Length", strconv.Itoa(len(blobContent)))
				w.Header().Set("Accept-Ranges", "bytes")
				w.WriteHeader(http.StatusOK)
				log.Printf("[mock-docker] HEAD blob served for %s", r.URL.Path)
				return
			}

			atomic.AddUint64(&blobRequests, 1)
			// 307 重定向到 CDN
			redirectUrl := "/cdn/blobs/" + blobDigest
			http.Redirect(w, r, redirectUrl, http.StatusTemporaryRedirect)
			log.Printf("[mock-docker] 307 redirect blob -> %s", redirectUrl)
			return
		}

		http.NotFound(w, r)
	}

	mux.HandleFunc("/v2/", handleV2Probe)
	mux.HandleFunc("/v2", handleV2Probe)
	mux.HandleFunc("/docker/v2/", handleV2Probe)
	mux.HandleFunc("/docker/v2", handleV2Probe)

	// 2. Auth 服务 Token 颁发接口
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"token":"mock-docker-token","access_token":"mock-docker-token","expires_in":3600}`)
		log.Printf("[mock-docker] token issued to client")
	})

	// 3. CDN 分层数据下载接口
	mux.HandleFunc("/cdn/blobs/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&cdnBlobRequests, 1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(blobContent)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, blobContent)
		log.Printf("[mock-docker] CDN served blob %s", r.URL.Path)
	})

	// 4. 统计接口
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "v2_probe_requests: %d\n", atomic.LoadUint64(&v2ProbeRequests))
		fmt.Fprintf(w, "manifest_requests: %d\n", atomic.LoadUint64(&manifestRequests))
		fmt.Fprintf(w, "blob_requests: %d\n", atomic.LoadUint64(&blobRequests))
		fmt.Fprintf(w, "cdn_blob_requests: %d\n", atomic.LoadUint64(&cdnBlobRequests))
		fmt.Fprintf(w, "head_blob_requests: %d\n", atomic.LoadUint64(&headBlobRequests))
	})

	port := ":9995"
	log.Printf("Mock Docker Registry v2 running on port %s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
