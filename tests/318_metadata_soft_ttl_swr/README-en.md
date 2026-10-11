# Feature 318: Dynamic Metadata Soft TTL & SWR Caching Test Suite

This directory provides the standalone test suite and regression tests for Feature 318 (Dynamic Metadata Soft TTL & SWR Caching Engine, Bypass TTL Extension).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring `bypass` rules with `ttl:2s` as well as classic `bypass` rules for contrast.
- `mock_server.go`: Mock upstream server providing PyPI, npm, and APT endpoints with ETag & 304 support.
- `soft_ttl_test.go`: Automated test suite covering freshness window hits, 304 revalidation, and syntax parsing.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from the repository root:
```bash
go test -v -count=1 ./tests/318_metadata_soft_ttl_swr/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Server
```bash
go run tests/318_metadata_soft_ttl_swr/mock_server.go
```
Listens on `127.0.0.1:9998`.

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/318_metadata_soft_ttl_swr/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **PyPI Soft TTL Freshness Window**:
   ```bash
   curl -i http://127.0.0.1:8888/pypi/simple/pkg/
   ```
   Request multiple times within 2 seconds; returns `X-Cache-Status: HIT-SOFT-TTL` with zero upstream fetches.
2. **Silent 304 SWR Revalidation**:
   After 2 seconds, issue request again; returns `X-Cache-Status: REVALIDATED-304`.
3. **Inspect Metrics**:
   ```bash
   curl http://127.0.0.1:9998/stats
   ```
