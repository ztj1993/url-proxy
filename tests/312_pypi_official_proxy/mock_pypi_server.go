package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

func main() {
	mux := http.NewServeMux()

	// 1. PyPI Simple Index (支持 PEP 503 HTML 与 PEP 691 JSON 协议)
	mux.HandleFunc("/pypi/simple/requests/", func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "application/vnd.pypi.simple.v1+json") {
			// PEP 691 JSON 响应
			w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
			w.WriteHeader(http.StatusOK)
			jsonResp := `{
  "meta": {"api-version": "1.0"},
  "name": "requests",
  "files": [
    {
      "filename": "requests-2.31.0-py3-none-any.whl",
      "url": "https://files.pythonhosted.org/packages/a/b/c/requests-2.31.0-py3-none-any.whl",
      "hashes": {"sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
      "requires-python": ">=3.8"
    },
    {
      "filename": "requests-2.31.0.tar.gz",
      "url": "https://files.pythonhosted.org/packages/d/e/f/requests-2.31.0.tar.gz",
      "hashes": {"sha256": "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"}
    }
  ]
}`
			fmt.Fprint(w, jsonResp)
			log.Printf("[mock-pypi] served PEP 691 JSON for %s", r.URL.Path)
			return
		}

		// PEP 503 HTML 响应
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		htmlResp := `<!DOCTYPE html>
<html>
  <head><title>Links for requests</title></head>
  <body>
    <h1>Links for requests</h1>
    <a href="https://files.pythonhosted.org/packages/a/b/c/requests-2.31.0-py3-none-any.whl#sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" data-requires-python="&gt;=3.8">requests-2.31.0-py3-none-any.whl</a><br/>
    <a href="https://files.pythonhosted.org/packages/d/e/f/requests-2.31.0.tar.gz#sha256=ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb">requests-2.31.0.tar.gz</a>
  </body>
</html>`
		fmt.Fprint(w, htmlResp)
		log.Printf("[mock-pypi] served PEP 503 HTML for %s", r.URL.Path)
	})

	// 2. 尾部斜杠重定向兼容
	mux.HandleFunc("/pypi/simple/requests", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pypi/simple/requests/", http.StatusMovedPermanently)
		log.Printf("[mock-pypi] redirect 301 for %s -> /pypi/simple/requests/", r.URL.Path)
	})

	// 3. 模拟 files.pythonhosted.org 二进制 Wheel 包下载
	mux.HandleFunc("/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"wheel-requests-2.31.0-hash"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "BINARY_WHEEL_PAYLOAD_FOR_REQUESTS_2.31.0_MOCK")
		log.Printf("[mock-cdn] served Wheel binary for %s", r.URL.Path)
	})

	// 4. 模拟 files.pythonhosted.org 源码包下载
	mux.HandleFunc("/pythonhosted/packages/d/e/f/requests-2.31.0.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Header().Set("ETag", `"sdist-requests-2.31.0-hash"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "SOURCE_ARCHIVE_TAR_GZ_PAYLOAD_FOR_REQUESTS_2.31.0_MOCK")
		log.Printf("[mock-cdn] served Sdist tarball for %s", r.URL.Path)
	})

	addr := ":9997"
	log.Printf("Mock PyPI and pythonhosted CDN server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
