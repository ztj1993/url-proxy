# Feature 337: Legacy Memory TTL Cleanup & Cache Convergence Test Suite

This directory provides the standalone local test environment and automated verification suite for Feature 337 (Legacy In-Memory Soft TTL Cleanup & Cache Convergence Plan).

---

## 1. Directory Structure

- `proxy_test.conf`: Test configuration file for Feature 337, specifying TTL-bearing caching and regular bypass routing rules.
- `mock_server.go`: Upstream mock server supporting 304 conditional revalidation and dynamic endpoint responses.
- `ttl_cleanup_test.go`: Core automated test suite verifying server execution with streamlined dependencies, unified disk TTL routing, zero-disk persistence for plain bypass requests, and safe passthrough for non-GET requests.
- `README-zh.md`: Chinese test documentation.
- `README-en.md`: English test documentation.

---

## 2. Automated Test Execution

Run the test suite from the repository root:
```bash
go test -v -count=1 ./tests/337_legacy_memory_ttl_cleanup/...
```

---

## 3. Manual Startup & Verification

### Step 1: Start Mock Upstream Server
```bash
go run tests/337_legacy_memory_ttl_cleanup/mock_server.go -port 19337
```

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/337_legacy_memory_ttl_cleanup/proxy_test.conf -cache ./cache -addr :8888
```

### Step 3: Verify TTL-Enabled Resource Disk Pipeline
```bash
# First request: fetches from upstream and persists to disk
curl -i http://localhost:8888/repo/InRelease
# Expected: 200 OK with X-Cache-Status: MISS-DISK-TTL

# Second request within TTL window: served from disk
curl -i http://localhost:8888/repo/InRelease
# Expected: 200 OK with X-Cache-Status: HIT-DISK-TTL
```
