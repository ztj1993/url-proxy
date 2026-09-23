package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// MultiClusterStats 记录 8 大生态所有上游请求的命中计数
type MultiClusterStats struct {
	AptInRelease   uint64
	AptPackages    uint64
	AptDeb         uint64
	AlpineIndex    uint64
	AlpineApk      uint64
	GoList         uint64
	GoMod          uint64
	GoInfo         uint64
	GoZip          uint64
	MavenMetadata  uint64
	MavenPom       uint64
	MavenJar       uint64
	PyPIIndex      uint64
	PyPIWhl        uint64
	NpmMeta        uint64
	NpmTarball     uint64
	DockerProbe    uint64
	DockerToken    uint64
	DockerManifest uint64
	DockerBlob     uint64
	CargoConfig    uint64
	CargoIndex     uint64
	CargoCrate     uint64
}

// 预定义测试二进制与文本内容载荷
const (
	AptDebPayload     = "mock-ubuntu-deb-binary-payload-hello-2.10.deb"
	AlpineApkPayload   = "mock-alpine-apk-binary-payload-curl-8.5.0.apk"
	GoZipPayload       = "mock-goproxy-gin-v1.9.1-zip-archive-payload"
	MavenJarPayload    = "mock-maven-demo-1.0.0-jar-archive-binary-payload"
	PyPIWhlPayload     = "mock-pypi-wheel-requests-2.31.0-py3-none-any.whl"
	NpmTarballPayload  = "mock-npm-tarball-express-4.18.2.tgz-binary-payload"
	DockerBlobPayload  = "mock-docker-layer-blob-sha256-payload-ubuntu-jammy"
	CargoCratePayload  = "mock-cargo-crate-serde-1.0.197.crate-binary-payload"
	DockerBlobDigest   = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// serveRangeBytes 支持 HTTP Range 206 分片及完整 200 直出
func serveRangeBytes(w http.ResponseWriter, r *http.Request, contentType string, data []byte) {
	reader := bytes.NewReader(data)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// 简易 Range 解析: bytes=start-end
	if strings.HasPrefix(rangeHeader, "bytes=") {
		parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
		start, err1 := strconv.ParseInt(parts[0], 10, 64)
		if err1 == nil && start < int64(len(data)) {
			var end int64 = int64(len(data)) - 1
			if len(parts) > 1 && parts[1] != "" {
				if parsedEnd, err2 := strconv.ParseInt(parts[1], 10, 64); err2 == nil && parsedEnd < int64(len(data)) {
					end = parsedEnd
				}
			}
			chunkLen := end - start + 1
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.Header().Set("Content-Length", strconv.FormatInt(chunkLen, 10))
			w.WriteHeader(http.StatusPartialContent)
			if r.Method != http.MethodHead {
				_, _ = reader.Seek(start, io.SeekStart)
				_, _ = io.CopyN(w, reader, chunkLen)
			}
			return
		}
	}

	http.ServeContent(w, r, "blob.bin", time.Time{}, reader)
}

// NewMultiClusterHandler 创建承载 8 大生态的统一 HTTP 请求处理器
func NewMultiClusterHandler(stats *MultiClusterStats) http.Handler {
	mux := http.NewServeMux()

	// -------------------------------------------------------------------------
	// 1. APT (Debian / Ubuntu)
	// -------------------------------------------------------------------------
	mux.HandleFunc("/ubuntu/dists/jammy/InRelease", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AptInRelease, 1)
		if r.Header.Get("If-None-Match") == `"mock-apt-inrelease-etag"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"mock-apt-inrelease-etag"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Suite: jammy\nCodename: jammy\nArchitectures: amd64\nComponents: main\n")
	})

	mux.HandleFunc("/ubuntu/dists/jammy/main/binary-amd64/Packages.gz", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AptPackages, 1)
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Package: hello\nVersion: 2.10-2\nArchitecture: amd64\nFilename: pool/main/h/hello/hello_2.10-2_amd64.deb\n\n")
	})

	mux.HandleFunc("/ubuntu/pool/main/h/hello/hello_2.10-2_amd64.deb", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AptDeb, 1)
		serveRangeBytes(w, r, "application/vnd.debian.binary-package", []byte(AptDebPayload))
	})

	// -------------------------------------------------------------------------
	// 2. Alpine Linux
	// -------------------------------------------------------------------------
	mux.HandleFunc("/alpine/v3.19/main/x86_64/APKINDEX.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AlpineIndex, 1)
		w.Header().Set("Content-Type", "application/x-tar")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "mock-alpine-apkindex-archive-payload")
	})

	mux.HandleFunc("/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.AlpineApk, 1)
		serveRangeBytes(w, r, "application/octet-stream", []byte(AlpineApkPayload))
	})

	// -------------------------------------------------------------------------
	// 3. Go Modules
	// -------------------------------------------------------------------------
	mux.HandleFunc("/goproxy/github.com/gin-gonic/gin/@v/list", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.GoList, 1)
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "v1.9.0\nv1.9.1\n")
	})

	mux.HandleFunc("/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.mod", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.GoMod, 1)
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "module github.com/gin-gonic/gin\n\ngo 1.20\n")
	})

	mux.HandleFunc("/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.info", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.GoInfo, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"Version":"v1.9.1","Time":"2023-03-01T00:00:00Z"}`)
	})

	mux.HandleFunc("/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.zip", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.GoZip, 1)
		serveRangeBytes(w, r, "application/zip", []byte(GoZipPayload))
	})

	// -------------------------------------------------------------------------
	// 4. Maven
	// -------------------------------------------------------------------------
	mux.HandleFunc("/maven/org/example/demo/1.0.0/maven-metadata.xml", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.MavenMetadata, 1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<metadata><groupId>org.example</groupId><artifactId>demo</artifactId><version>1.0.0</version></metadata>`)
	})

	mux.HandleFunc("/maven/org/example/demo/1.0.0/demo-1.0.0.pom", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.MavenPom, 1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<project><modelVersion>4.0.0</modelVersion><groupId>org.example</groupId><artifactId>demo</artifactId><version>1.0.0</version></project>`)
	})

	mux.HandleFunc("/maven/org/example/demo/1.0.0/demo-1.0.0.jar", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.MavenJar, 1)
		serveRangeBytes(w, r, "application/java-archive", []byte(MavenJarPayload))
	})

	// -------------------------------------------------------------------------
	// 5. PyPI (Python)
	// -------------------------------------------------------------------------
	mux.HandleFunc("/pypi/simple/requests/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.PyPIIndex, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<!DOCTYPE html><html><body><a href="https://files.pythonhosted.org/packages/requests-2.31.0-py3-none-any.whl">requests-2.31.0-py3-none-any.whl</a></body></html>`)
	})

	mux.HandleFunc("/pythonhosted/packages/requests-2.31.0-py3-none-any.whl", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.PyPIWhl, 1)
		serveRangeBytes(w, r, "application/octet-stream", []byte(PyPIWhlPayload))
	})

	// -------------------------------------------------------------------------
	// 6. npm (Node.js)
	// -------------------------------------------------------------------------
	mux.HandleFunc("/npm/express", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.NpmMeta, 1)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"name":"express","versions":{"4.18.2":{"name":"express","version":"4.18.2","dist":{"tarball":"https://registry.npmjs.org/express/-/express-4.18.2.tgz"}}}}`)
	})

	mux.HandleFunc("/npm/express/-/express-4.18.2.tgz", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.NpmTarball, 1)
		serveRangeBytes(w, r, "application/octet-stream", []byte(NpmTarballPayload))
	})

	// -------------------------------------------------------------------------
	// 7. Docker / OCI Registry v2
	// -------------------------------------------------------------------------
	mux.HandleFunc("/docker/v2/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 握手探测 (/docker/v2/)
		if path == "/docker/v2/" || path == "/docker/v2" {
			atomic.AddUint64(&stats.DockerProbe, 1)
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				realm := fmt.Sprintf("http://%s/docker/v2/token", r.Host)
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s",service="registry.docker.io"`, realm))
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{}`)
			return
		}

		// Token 获取端点
		if path == "/docker/v2/token" {
			atomic.AddUint64(&stats.DockerToken, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"token":"mock-docker-cluster-token"}`)
			return
		}

		// Manifests 端点
		if strings.Contains(path, "/manifests/") {
			atomic.AddUint64(&stats.DockerManifest, 1)
			if r.Header.Get("If-None-Match") == `"mock-manifest-etag"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.Header().Set("ETag", `"mock-manifest-etag"`)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":50,"digest":"`+DockerBlobDigest+`"}]}`)
			return
		}

		// Blobs 端点
		if strings.Contains(path, "/blobs/") {
			atomic.AddUint64(&stats.DockerBlob, 1)
			serveRangeBytes(w, r, "application/octet-stream", []byte(DockerBlobPayload))
			return
		}

		http.NotFound(w, r)
	})

	// -------------------------------------------------------------------------
	// 8. Rust Cargo
	// -------------------------------------------------------------------------
	mux.HandleFunc("/crates-index/config.json", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.CargoConfig, 1)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("ETag", `"mock-cargo-config-etag"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"dl":"https://static.crates.io/crates","api":"https://crates.io"}`)
	})

	mux.HandleFunc("/crates-index/se/rd/serde", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.CargoIndex, 1)
		if r.Header.Get("If-None-Match") == `"mock-cargo-index-etag"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", `"mock-cargo-index-etag"`)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"name":"serde","vers":"1.0.197","deps":[],"cksum":"mock-serde-cksum","yanked":false}`)
	})

	mux.HandleFunc("/crates-static/crates/serde/serde-1.0.197.crate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&stats.CargoCrate, 1)
		serveRangeBytes(w, r, "application/x-tar", []byte(CargoCratePayload))
	})

	// -------------------------------------------------------------------------
	// 统计端点 (/stats)
	// -------------------------------------------------------------------------
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "apt_inrelease: %d\n", atomic.LoadUint64(&stats.AptInRelease))
		fmt.Fprintf(w, "apt_packages: %d\n", atomic.LoadUint64(&stats.AptPackages))
		fmt.Fprintf(w, "apt_deb: %d\n", atomic.LoadUint64(&stats.AptDeb))
		fmt.Fprintf(w, "alpine_index: %d\n", atomic.LoadUint64(&stats.AlpineIndex))
		fmt.Fprintf(w, "alpine_apk: %d\n", atomic.LoadUint64(&stats.AlpineApk))
		fmt.Fprintf(w, "go_list: %d\n", atomic.LoadUint64(&stats.GoList))
		fmt.Fprintf(w, "go_mod: %d\n", atomic.LoadUint64(&stats.GoMod))
		fmt.Fprintf(w, "go_info: %d\n", atomic.LoadUint64(&stats.GoInfo))
		fmt.Fprintf(w, "go_zip: %d\n", atomic.LoadUint64(&stats.GoZip))
		fmt.Fprintf(w, "maven_meta: %d\n", atomic.LoadUint64(&stats.MavenMetadata))
		fmt.Fprintf(w, "maven_pom: %d\n", atomic.LoadUint64(&stats.MavenPom))
		fmt.Fprintf(w, "maven_jar: %d\n", atomic.LoadUint64(&stats.MavenJar))
		fmt.Fprintf(w, "pypi_index: %d\n", atomic.LoadUint64(&stats.PyPIIndex))
		fmt.Fprintf(w, "pypi_whl: %d\n", atomic.LoadUint64(&stats.PyPIWhl))
		fmt.Fprintf(w, "npm_meta: %d\n", atomic.LoadUint64(&stats.NpmMeta))
		fmt.Fprintf(w, "npm_tarball: %d\n", atomic.LoadUint64(&stats.NpmTarball))
		fmt.Fprintf(w, "docker_probe: %d\n", atomic.LoadUint64(&stats.DockerProbe))
		fmt.Fprintf(w, "docker_token: %d\n", atomic.LoadUint64(&stats.DockerToken))
		fmt.Fprintf(w, "docker_manifest: %d\n", atomic.LoadUint64(&stats.DockerManifest))
		fmt.Fprintf(w, "docker_blob: %d\n", atomic.LoadUint64(&stats.DockerBlob))
		fmt.Fprintf(w, "cargo_config: %d\n", atomic.LoadUint64(&stats.CargoConfig))
		fmt.Fprintf(w, "cargo_index: %d\n", atomic.LoadUint64(&stats.CargoIndex))
		fmt.Fprintf(w, "cargo_crate: %d\n", atomic.LoadUint64(&stats.CargoCrate))
	})

	return mux
}

func main() {
	port := flag.Int("port", 9990, "port to listen on")
	flag.Parse()

	stats := &MultiClusterStats{}
	handler := NewMultiClusterHandler(stats)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("High-Fidelity Multi-Ecosystem Mock Cluster running on %s", addr)
	log.Printf("Endpoints simulated: APT, Alpine, Go, Maven, PyPI, npm, Docker, Cargo")
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
