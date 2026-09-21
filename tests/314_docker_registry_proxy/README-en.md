# Feature 314: Docker / OCI Registry v2 Relay & Layer Caching Test Suite

This directory provides a standalone local testing and verification environment for Feature 314 (Docker / OCI Registry v2 Protocol Relay & Layer Caching).

---

## Directory Structure

- `mock_docker_server.go`: Simulates the official Docker Registry v2 (`registry-1.docker.io`) and Auth service on port `9995`. Supports `/v2/` probing, 401 Bearer Token challenge, Manifest 304 conditional revalidation, Layer Blob 307 CDN redirect follow, and native HEAD acceleration.
- `proxy_docker.conf`: Dedicated test configuration for Feature 314.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## Quick Start & Verification

### Step 1: Start Mock Docker Registry Service

In Terminal 1:
```bash
go run tests/314_docker_registry_proxy/mock_docker_server.go
```
Expected output: `Mock Docker Registry v2 running on port :9995`.

### Step 2: Start url-proxy Test Instance

In Terminal 2:
```bash
go run url-proxy.go -conf tests/314_docker_registry_proxy/proxy_docker.conf -addr :8888
```

---

## Test Scenarios

### 1. v2 Probe & 401 Bearer Token Challenge Relay
```bash
curl -i http://127.0.0.1:8888/v2/
```
**Expected Outcome**:
- Returns `HTTP/1.1 401 Unauthorized`.
- Header contains `Www-Authenticate: Bearer realm="http://127.0.0.1:9995/token",service="127.0.0.1:9995"`.
- Header contains `Docker-Distribution-Api-Version: registry/2.0`.

### 2. Client Bearer Token Acquisition
```bash
curl -s http://127.0.0.1:9995/token
```
**Expected Outcome**: Returns JSON Token (`{"token":"mock-docker-token", ...}`).

### 3. Manifest Relay with Token & 304 Revalidation
```bash
# Full manifest fetch
curl -i -H "Authorization: Bearer mock-docker-token" http://127.0.0.1:8888/v2/library/ubuntu/manifests/22.04

# Conditional fetch with If-None-Match
curl -i -H "Authorization: Bearer mock-docker-token" -H 'If-None-Match: "ubuntu-2204-etag"' http://127.0.0.1:8888/v2/library/ubuntu/manifests/22.04
```
**Expected Outcome**:
- First request returns HTTP 200 with manifest JSON.
- Conditional request returns `HTTP/1.1 304 Not Modified` with zero body transfer.

### 4. Layer Blob Cold Pull (307 CDN Redirect Follow & Local Persistence)
```bash
curl -i -H "Authorization: Bearer mock-docker-token" http://127.0.0.1:8888/v2/library/ubuntu/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
```
**Expected Outcome**:
- Returns HTTP 200 with binary payload.
- Proxy logs indicate `job leader`, `job download`, and `job renamed successfully`.
- A safe cache file `sha256%3A...` is created in the cache directory.

### 5. Native HEAD Acceleration on Cached Blob (0 Upstream Requests)
```bash
curl -I http://127.0.0.1:8888/v2/library/ubuntu/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
```
**Expected Outcome**:
- Returns HTTP 200 with `Content-Length` and `Accept-Ranges: bytes`.
- Upstream server receives zero new requests (zero upstream bandwidth).

### 6. Range Partial Content Resumption (0 Upstream Requests)
```bash
curl -i -r 0-15 http://127.0.0.1:8888/v2/library/ubuntu/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
```
**Expected Outcome**:
- Returns `HTTP/1.1 206 Partial Content` served directly from local cache.

### 7. Path Prefix Mode Compatibility (`/docker/v2/`)
```bash
curl -i http://127.0.0.1:8888/docker/v2/
curl -i -H "Authorization: Bearer mock-docker-token" http://127.0.0.1:8888/docker/v2/library/ubuntu/manifests/22.04
```
**Expected Outcome**: Behaves identically to the `/v2/` daemon mirror mode.
