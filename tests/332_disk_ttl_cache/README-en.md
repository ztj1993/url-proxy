# Feature 332: Disk TTL Cache with mtime Freshness & Conditional Revalidation Test Suite

This directory provides the standalone test environment and automated regression test suite for Feature 332 (Disk TTL Cache with mtime Freshness & Conditional Revalidation).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring `bypass` rules with `ttl:10s` for dynamic metadata.
- `mock_server.go`: Mock upstream server supporting 200 OK content updates, 304 conditional revalidation, and 502 simulated outages.
- `disk_ttl_test.go`: Automated test suite covering freshness window hits, 304 timestamp extension, content updates with atomic replacement, stale-fallback resilience, and persistence across restarts.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from repository root:
```bash
go test -v -count=1 ./tests/332_disk_ttl_cache/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Upstream Server
```bash
go run tests/332_disk_ttl_cache/mock_server.go
```
Listens on `127.0.0.1:9996`.

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/332_disk_ttl_cache/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **Initial Fetch & Disk Persistence**:
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9996/repo/InRelease
   ```
   Inspect response header `X-Cache-Status: MISS-DISK-TTL`, and verify disk file is created.
2. **Freshness Hit Window**:
   Request again within 10 seconds; returns `X-Cache-Status: HIT-DISK-TTL` with zero upstream fetches.
3. **Conditional Lease Renewal & Stale-Fallback**:
   Request after 10 seconds; origin responds with 304, proxy sets `X-Cache-Status: REVALIDATED-304` and touches the file's modification timestamp.