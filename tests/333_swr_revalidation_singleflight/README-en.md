# Feature 333: Disk TTL Concurrent Revalidation & Request Coalescing Test Suite

This directory provides the standalone test environment and automated regression test suite for Feature 333 (Disk TTL Concurrent Revalidation & Request Coalescing to prevent revalidation stampedes).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring `bypass` rules with `ttl:10s` for dynamic metadata.
- `mock_server.go`: Mock upstream server with configurable artificial latency to rigorously test concurrency coalescing and fetch counters.
- `revalidation_test.go`: Automated test suite covering 30-concurrent-probe 304 revalidation coalescing, 30-concurrent-client 200 update single-stream downloads, Leader mid-flight abort isolation, and concurrent stale-fallback under upstream outages.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from repository root:
```bash
go test -v -count=1 ./tests/333_swr_revalidation_singleflight/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Upstream Server
```bash
go run tests/333_swr_revalidation_singleflight/mock_server.go
```
Listens on `127.0.0.1:9997`.

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/333_swr_revalidation_singleflight/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **304 Revalidation Coalescing**:
   Send 30 concurrent requests using load tools (e.g. `hey` or `ab`); confirm upstream counter increases by strictly 1.
2. **200 Content Update Single Stream**:
   Upon content updates, verify origin serves only 1 stream while all 30 clients receive complete updated payloads.