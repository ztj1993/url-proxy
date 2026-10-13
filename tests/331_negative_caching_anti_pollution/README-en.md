# Feature 331: Non-200 Cache Anti-Pollution & Direct Streaming Passthrough Test Suite

This directory provides the standalone test suite and regression tests for Feature 331 (Non-200 Cache Anti-Pollution & Direct Streaming Passthrough).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring service address, cache path, and routing rules.
- `mock_server.go`: Mock upstream server providing 404 Not Found, 500 Internal Server Error, 502 Bad Gateway, and 200 OK valid artifact endpoints.
- `anti_pollution_test.go`: Automated test suite covering 404 zero-disk persistence, 500/502 fault isolation and recovery, 200 OK artifact caching, and concurrent request safety.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from the repository root:
```bash
go test -v -count=1 ./tests/331_negative_caching_anti_pollution/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Server
```bash
go run tests/331_negative_caching_anti_pollution/mock_server.go
```
Listens on `127.0.0.1:9995`.

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/331_negative_caching_anti_pollution/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **404 Zero-Disk-Persistence**:
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9995/repo/not-found.deb
   ```
   Verify HTTP 404 is received with upstream error body, and no cache file or temp file is created in the cache directory.
2. **500 Outage & Recovery**:
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9995/repo/server-error.deb
   ```
   Verify HTTP 500 is passed through cleanly without lingering cache files.
3. **200 OK Valid Artifact Caching**:
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9995/repo/valid-pkg.deb
   ```
   Verify valid package is received and successfully persisted in cache with matching checksum.