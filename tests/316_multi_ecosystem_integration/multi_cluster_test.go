package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMultiClusterEndpoints(t *testing.T) {
	stats := &MultiClusterStats{}
	handler := NewMultiClusterHandler(stats)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// 1. APT 测试
	t.Run("APT Endpoints", func(t *testing.T) {
		// InRelease 首次拉取与 304
		resp, err := client.Get(server.URL + "/ubuntu/dists/jammy/InRelease")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("APT InRelease failed: %v, status: %d", err, resp.StatusCode)
		}
		etag := resp.Header.Get("ETag")
		_ = resp.Body.Close()

		req304, _ := http.NewRequest("GET", server.URL+"/ubuntu/dists/jammy/InRelease", nil)
		req304.Header.Set("If-None-Match", etag)
		resp304, err := client.Do(req304)
		if err != nil || resp304.StatusCode != http.StatusNotModified {
			t.Errorf("APT InRelease 304 failed: %v, status: %d", err, resp304.StatusCode)
		}
		_ = resp304.Body.Close()

		// Packages.gz
		respPkg, err := client.Get(server.URL + "/ubuntu/dists/jammy/main/binary-amd64/Packages.gz")
		if err != nil || respPkg.StatusCode != http.StatusOK {
			t.Errorf("APT Packages.gz failed: %v, status: %d", err, respPkg.StatusCode)
		}
		_ = respPkg.Body.Close()

		// .deb Range 206
		reqRange, _ := http.NewRequest("GET", server.URL+"/ubuntu/pool/main/h/hello/hello_2.10-2_amd64.deb", nil)
		reqRange.Header.Set("Range", "bytes=0-15")
		respRange, err := client.Do(reqRange)
		if err != nil || respRange.StatusCode != http.StatusPartialContent {
			t.Errorf("APT deb Range 206 failed: %v, status: %d", err, respRange.StatusCode)
		}
		body, _ := io.ReadAll(respRange.Body)
		_ = respRange.Body.Close()
		if string(body) != AptDebPayload[:16] {
			t.Errorf("APT deb Range content mismatch: %q", string(body))
		}
	})

	// 2. Alpine 测试
	t.Run("Alpine Endpoints", func(t *testing.T) {
		respIndex, err := client.Get(server.URL + "/alpine/v3.19/main/x86_64/APKINDEX.tar.gz")
		if err != nil || respIndex.StatusCode != http.StatusOK {
			t.Errorf("Alpine APKINDEX failed: %v, status: %d", err, respIndex.StatusCode)
		}
		_ = respIndex.Body.Close()

		respApk, err := client.Get(server.URL + "/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk")
		if err != nil || respApk.StatusCode != http.StatusOK {
			t.Errorf("Alpine apk failed: %v, status: %d", err, respApk.StatusCode)
		}
		body, _ := io.ReadAll(respApk.Body)
		_ = respApk.Body.Close()
		if string(body) != AlpineApkPayload {
			t.Errorf("Alpine apk content mismatch: %q", string(body))
		}
	})

	// 3. Go Modules 测试
	t.Run("Go Modules Endpoints", func(t *testing.T) {
		respList, err := client.Get(server.URL + "/goproxy/github.com/gin-gonic/gin/@v/list")
		if err != nil || respList.StatusCode != http.StatusOK {
			t.Errorf("Go list failed: %v, status: %d", err, respList.StatusCode)
		}
		_ = respList.Body.Close()

		respMod, err := client.Get(server.URL + "/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.mod")
		if err != nil || respMod.StatusCode != http.StatusOK {
			t.Errorf("Go mod failed: %v, status: %d", err, respMod.StatusCode)
		}
		_ = respMod.Body.Close()

		respInfo, err := client.Get(server.URL + "/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.info")
		if err != nil || respInfo.StatusCode != http.StatusOK {
			t.Errorf("Go info failed: %v, status: %d", err, respInfo.StatusCode)
		}
		_ = respInfo.Body.Close()

		respZip, err := client.Get(server.URL + "/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.zip")
		if err != nil || respZip.StatusCode != http.StatusOK {
			t.Errorf("Go zip failed: %v, status: %d", err, respZip.StatusCode)
		}
		body, _ := io.ReadAll(respZip.Body)
		_ = respZip.Body.Close()
		if string(body) != GoZipPayload {
			t.Errorf("Go zip content mismatch: %q", string(body))
		}
	})

	// 4. Maven 测试
	t.Run("Maven Endpoints", func(t *testing.T) {
		respMeta, err := client.Get(server.URL + "/maven/org/example/demo/1.0.0/maven-metadata.xml")
		if err != nil || respMeta.StatusCode != http.StatusOK {
			t.Errorf("Maven metadata failed: %v, status: %d", err, respMeta.StatusCode)
		}
		_ = respMeta.Body.Close()

		respPom, err := client.Get(server.URL + "/maven/org/example/demo/1.0.0/demo-1.0.0.pom")
		if err != nil || respPom.StatusCode != http.StatusOK {
			t.Errorf("Maven pom failed: %v, status: %d", err, respPom.StatusCode)
		}
		_ = respPom.Body.Close()

		respJar, err := client.Get(server.URL + "/maven/org/example/demo/1.0.0/demo-1.0.0.jar")
		if err != nil || respJar.StatusCode != http.StatusOK {
			t.Errorf("Maven jar failed: %v, status: %d", err, respJar.StatusCode)
		}
		body, _ := io.ReadAll(respJar.Body)
		_ = respJar.Body.Close()
		if string(body) != MavenJarPayload {
			t.Errorf("Maven jar content mismatch: %q", string(body))
		}
	})

	// 5. PyPI 测试
	t.Run("PyPI Endpoints", func(t *testing.T) {
		respIndex, err := client.Get(server.URL + "/pypi/simple/requests/")
		if err != nil || respIndex.StatusCode != http.StatusOK {
			t.Errorf("PyPI simple failed: %v, status: %d", err, respIndex.StatusCode)
		}
		bodyIndex, _ := io.ReadAll(respIndex.Body)
		_ = respIndex.Body.Close()
		if !strings.Contains(string(bodyIndex), "https://files.pythonhosted.org/") {
			t.Errorf("PyPI simple missing upstream files domain: %s", string(bodyIndex))
		}

		respWhl, err := client.Get(server.URL + "/pythonhosted/packages/requests-2.31.0-py3-none-any.whl")
		if err != nil || respWhl.StatusCode != http.StatusOK {
			t.Errorf("PyPI whl failed: %v, status: %d", err, respWhl.StatusCode)
		}
		bodyWhl, _ := io.ReadAll(respWhl.Body)
		_ = respWhl.Body.Close()
		if string(bodyWhl) != PyPIWhlPayload {
			t.Errorf("PyPI whl content mismatch: %q", string(bodyWhl))
		}
	})

	// 6. npm 测试
	t.Run("npm Endpoints", func(t *testing.T) {
		respMeta, err := client.Get(server.URL + "/npm/express")
		if err != nil || respMeta.StatusCode != http.StatusOK {
			t.Errorf("npm metadata failed: %v, status: %d", err, respMeta.StatusCode)
		}
		bodyMeta, _ := io.ReadAll(respMeta.Body)
		_ = respMeta.Body.Close()
		if !strings.Contains(string(bodyMeta), "https://registry.npmjs.org/express/-/express-4.18.2.tgz") {
			t.Errorf("npm metadata missing tarball url: %s", string(bodyMeta))
		}

		respTgz, err := client.Get(server.URL + "/npm/express/-/express-4.18.2.tgz")
		if err != nil || respTgz.StatusCode != http.StatusOK {
			t.Errorf("npm tarball failed: %v, status: %d", err, respTgz.StatusCode)
		}
		bodyTgz, _ := io.ReadAll(respTgz.Body)
		_ = respTgz.Body.Close()
		if string(bodyTgz) != NpmTarballPayload {
			t.Errorf("npm tarball content mismatch: %q", string(bodyTgz))
		}
	})

	// 7. Docker 测试
	t.Run("Docker Endpoints", func(t *testing.T) {
		// 握手 401 质询
		respProbe, err := client.Get(server.URL + "/docker/v2/")
		if err != nil || respProbe.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Docker probe expected 401, got %v, status: %d", err, respProbe.StatusCode)
		}
		wwwAuth := respProbe.Header.Get("Www-Authenticate")
		if !strings.Contains(wwwAuth, "Bearer realm=") {
			t.Errorf("Docker Www-Authenticate missing Bearer realm: %s", wwwAuth)
		}
		_ = respProbe.Body.Close()

		// Token
		respToken, err := client.Get(server.URL + "/docker/v2/token")
		if err != nil || respToken.StatusCode != http.StatusOK {
			t.Errorf("Docker token failed: %v, status: %d", err, respToken.StatusCode)
		}
		_ = respToken.Body.Close()

		// Manifests (带 Bearer Header)
		reqManifest, _ := http.NewRequest("GET", server.URL+"/docker/v2/library/nginx/manifests/latest", nil)
		reqManifest.Header.Set("Authorization", "Bearer mock-docker-cluster-token")
		respManifest, err := client.Do(reqManifest)
		if err != nil || respManifest.StatusCode != http.StatusOK {
			t.Errorf("Docker manifest failed: %v, status: %d", err, respManifest.StatusCode)
		}
		manifestEtag := respManifest.Header.Get("ETag")
		_ = respManifest.Body.Close()

		// Manifests 304
		reqManifest304, _ := http.NewRequest("GET", server.URL+"/docker/v2/library/nginx/manifests/latest", nil)
		reqManifest304.Header.Set("Authorization", "Bearer mock-docker-cluster-token")
		reqManifest304.Header.Set("If-None-Match", manifestEtag)
		respManifest304, err := client.Do(reqManifest304)
		if err != nil || respManifest304.StatusCode != http.StatusNotModified {
			t.Errorf("Docker manifest 304 failed: %v, status: %d", err, respManifest304.StatusCode)
		}
		_ = respManifest304.Body.Close()

		// Blob HEAD & Range
		blobURL := server.URL + "/docker/v2/library/nginx/blobs/" + DockerBlobDigest
		respHead, err := client.Head(blobURL)
		if err != nil || respHead.StatusCode != http.StatusOK {
			t.Errorf("Docker blob HEAD failed: %v, status: %d", err, respHead.StatusCode)
		}
		_ = respHead.Body.Close()

		reqBlobRange, _ := http.NewRequest("GET", blobURL, nil)
		reqBlobRange.Header.Set("Range", "bytes=0-15")
		respBlobRange, err := client.Do(reqBlobRange)
		if err != nil || respBlobRange.StatusCode != http.StatusPartialContent {
			t.Errorf("Docker blob Range 206 failed: %v, status: %d", err, respBlobRange.StatusCode)
		}
		bodyBlob, _ := io.ReadAll(respBlobRange.Body)
		_ = respBlobRange.Body.Close()
		if string(bodyBlob) != DockerBlobPayload[:16] {
			t.Errorf("Docker blob Range content mismatch: %q", string(bodyBlob))
		}
	})

	// 8. Cargo 测试
	t.Run("Cargo Endpoints", func(t *testing.T) {
		// config.json
		respConfig, err := client.Get(server.URL + "/crates-index/config.json")
		if err != nil || respConfig.StatusCode != http.StatusOK {
			t.Fatalf("Cargo config.json failed: %v, status: %d", err, respConfig.StatusCode)
		}
		bodyConfig, _ := io.ReadAll(respConfig.Body)
		_ = respConfig.Body.Close()
		if !strings.Contains(string(bodyConfig), `"dl":"https://static.crates.io/crates"`) {
			t.Errorf("Cargo config dl mismatch: %s", string(bodyConfig))
		}

		// 稀疏索引与 304
		respIndex, err := client.Get(server.URL + "/crates-index/se/rd/serde")
		if err != nil || respIndex.StatusCode != http.StatusOK {
			t.Fatalf("Cargo index failed: %v, status: %d", err, respIndex.StatusCode)
		}
		indexEtag := respIndex.Header.Get("ETag")
		_ = respIndex.Body.Close()

		reqIndex304, _ := http.NewRequest("GET", server.URL+"/crates-index/se/rd/serde", nil)
		reqIndex304.Header.Set("If-None-Match", indexEtag)
		respIndex304, err := client.Do(reqIndex304)
		if err != nil || respIndex304.StatusCode != http.StatusNotModified {
			t.Errorf("Cargo index 304 failed: %v, status: %d", err, respIndex304.StatusCode)
		}
		_ = respIndex304.Body.Close()

		// .crate 下载与 Range
		crateURL := server.URL + "/crates-static/crates/serde/serde-1.0.197.crate"
		reqCrateRange, _ := http.NewRequest("GET", crateURL, nil)
		reqCrateRange.Header.Set("Range", "bytes=0-15")
		respCrateRange, err := client.Do(reqCrateRange)
		if err != nil || respCrateRange.StatusCode != http.StatusPartialContent {
			t.Errorf("Cargo crate Range 206 failed: %v, status: %d", err, respCrateRange.StatusCode)
		}
		bodyCrate, _ := io.ReadAll(respCrateRange.Body)
		_ = respCrateRange.Body.Close()
		if string(bodyCrate) != CargoCratePayload[:16] {
			t.Errorf("Cargo crate content mismatch: %q", string(bodyCrate))
		}
	})

	// 9. Stats 校验
	t.Run("Stats Endpoint", func(t *testing.T) {
		respStats, err := client.Get(server.URL + "/stats")
		if err != nil || respStats.StatusCode != http.StatusOK {
			t.Fatalf("Stats failed: %v, status: %d", err, respStats.StatusCode)
		}
		bodyStats, _ := io.ReadAll(respStats.Body)
		_ = respStats.Body.Close()
		statsStr := string(bodyStats)
		if !strings.Contains(statsStr, "apt_inrelease: 2") ||
			!strings.Contains(statsStr, "pypi_index: 1") ||
			!strings.Contains(statsStr, "npm_meta: 1") ||
			!strings.Contains(statsStr, "docker_probe: 1") ||
			!strings.Contains(statsStr, "cargo_config: 1") {
			t.Errorf("Stats output unexpected:\n%s", statsStr)
		}
	})
}
