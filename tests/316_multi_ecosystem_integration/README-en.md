# Feature 316: Multi-Ecosystem End-to-End Integration Testing & Preset Templates

This directory provides the high-fidelity mock cluster, integration testing configuration, and automated test suite for Feature 316.

---

## 1. Directory Structure

- `mock_multi_cluster.go`: High-fidelity multi-source mock cluster listening on port `9990` by default. Simulates 8 upstream ecosystems (APT, Alpine, Go, Maven, PyPI, npm, Docker/OCI, Rust Cargo) in a single process, equipped with atomic request counters and a `/stats` monitoring endpoint.
- `proxy_integration.conf`: All-in-one configuration file specifically tuned for integration testing against the local mock cluster.
- `multi_cluster_test.go`: Automated test verifying mock cluster protocol contracts and endpoint responses.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation (this file).

---

## 2. Simulated Upstream Protocols & Endpoints

| # | Ecosystem | Upstream Endpoints | Protocol Behaviors & Verification Points |
| :---: | :--- | :--- | :--- |
| 1 | **APT** (Debian/Ubuntu) | `/ubuntu/dists/jammy/InRelease`<br>`/ubuntu/pool/main/h/hello/hello_2.10-2_amd64.deb` | `InRelease` bypass + 304 conditional revalidation; `.deb` permanent cache and Range 206 chunking |
| 2 | **Alpine** | `/alpine/v3.19/main/x86_64/APKINDEX.tar.gz`<br>`/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk` | `APKINDEX.tar.gz` streaming passthrough; `.apk` cache and atomic disk persistence |
| 3 | **Go Modules** | `/goproxy/github.com/gin-gonic/gin/@v/list`<br>`.../@v/v1.9.1.mod`, `.info`, `.zip` | `/@v/list` bypass; `.mod` / `.info` / `.zip` cache |
| 4 | **Maven** | `/maven/org/example/demo/1.0.0/maven-metadata.xml`<br>`.../demo-1.0.0.pom`, `.../demo-1.0.0.jar` | `maven-metadata.xml` passthrough; `.jar` / `.pom` cache |
| 5 | **PyPI** | `/pypi/simple/requests/`<br>`/pythonhosted/packages/requests-2.31.0-py3-none-any.whl` | Simple index response body rewrite (cross-domain CDN URLs bound to local proxy); `.whl` cache |
| 6 | **npm** | `/npm/express`<br>`/npm/express/-/express-4.18.2.tgz` | package JSON `dist.tarball` rewrite; `.tgz` cache |
| 7 | **Docker / OCI** | `/docker/v2/`<br>`/docker/v2/library/nginx/manifests/latest`<br>`/docker/v2/library/nginx/blobs/sha256:...` | `/v2/` 401 Www-Authenticate challenge relay; manifest 304; Layer Blob cache and Range 206 |
| 8 | **Rust Cargo** | `/crates-index/config.json`<br>`/crates-index/se/rd/serde`<br>`/crates-static/crates/serde/serde-1.0.197.crate` | `config.json` dynamic `dl` endpoint rewrite; sparse index 304 revalidation; `.crate` cache |

---

## 3. Manual Testing & Verification

### Step 1: Launch Mock Cluster
In terminal 1:
```bash
go run tests/316_multi_ecosystem_integration/mock_multi_cluster.go -port 9990
```

### Step 2: Launch url-proxy Instance
In terminal 2:
```bash
go run url-proxy.go -conf tests/316_multi_ecosystem_integration/proxy_integration.conf -port 8888
```

### Step 3: Run Verification Queries

```bash
# 1. APT
curl -i http://127.0.0.1:8888/ubuntu/dists/jammy/InRelease
curl -i http://127.0.0.1:8888/ubuntu/pool/main/h/hello/hello_2.10-2_amd64.deb

# 2. Alpine
curl -i http://127.0.0.1:8888/alpine/v3.19/main/x86_64/APKINDEX.tar.gz
curl -i http://127.0.0.1:8888/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk

# 3. Go Modules
curl -i http://127.0.0.1:8888/goproxy/github.com/gin-gonic/gin/@v/list
curl -i http://127.0.0.1:8888/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.zip

# 4. Maven
curl -i http://127.0.0.1:8888/maven/org/example/demo/1.0.0/maven-metadata.xml
curl -i http://127.0.0.1:8888/maven/org/example/demo/1.0.0/demo-1.0.0.jar

# 5. PyPI Rewriting
curl -i http://127.0.0.1:8888/pypi/simple/requests/

# 6. npm Rewriting
curl -i http://127.0.0.1:8888/npm/express

# 7. Docker Registry 401 Probe & Blobs
curl -i http://127.0.0.1:8888/docker/v2/
curl -i http://127.0.0.1:8888/docker/v2/library/nginx/blobs/sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

# 8. Cargo Rewriting
curl -i http://127.0.0.1:8888/crates-index/config.json
```

---

## 4. Automated Regression Tests

Run mock cluster tests:
```bash
go test -v ./tests/316_multi_ecosystem_integration/...
```

Run comprehensive end-to-end integration and concurrency pipeline:
```bash
go test -v -run TestMultiEcosystemIntegrationPipeline .
```
