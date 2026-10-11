# Feature 317: Route-Scoped Plugins & Docker Layer Caching Decoupling Test Suite

This directory provides the standalone test suite and automated regression tests for Feature 317 (Route-Scoped Plugins & Docker Layer Caching Decoupling).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration file binding `plugin:docker` to `docker` and `v2` aliases while leaving `plain` and `generic` unaugmented.
- `mock_server.go`: Mock upstream server handling Docker Registry v2 handshakes, layer blobs, and plain responses while recording upstream hit metrics.
- `scoped_plugins_test.go`: Automated test suite covering positive Docker plugin serving, negative scope isolation on non-docker routes, and configuration syntax parsing.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run the following command from the project root:

```bash
go test -v -count=1 ./tests/317_scoped_route_plugins/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Server
```bash
go run tests/317_scoped_route_plugins/mock_server.go
```
Listens on `127.0.0.1:9997`.

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/317_scoped_route_plugins/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **Positive Docker Plugin Route**:
   ```bash
   curl -i http://127.0.0.1:8888/docker/v2/
   curl -i http://127.0.0.1:8888/docker/v2/library/test/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
   ```
2. **Negative Non-Docker Scope Isolation**:
   ```bash
   curl -i http://127.0.0.1:8888/plain/repo/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
   ```
   Confirms that generic upstream payload is returned and Docker fast-path is not intercepted.
3. **Inspect Metrics**:
   ```bash
   curl http://127.0.0.1:9997/stats
   ```
