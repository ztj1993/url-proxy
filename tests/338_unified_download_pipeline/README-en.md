# Feature 338: Unified Concurrent Download & Broadcast Pipeline Test Suite

This directory provides the local standalone test environment and automated verification suite for Feature 338 (Unified Concurrent Download & Broadcast Pipeline Plan).

---

## 1. Directory Structure

- `proxy_test.conf`: Dedicated test configuration for Feature 338, declaring static caching rules alongside dynamic metadata rules with `ttl:5s`.
- `mock_server.go`: Mock upstream server supporting static binary artifacts, dynamic TTL metadata (304, 500, 200), and 404 error responses.
- `unified_pipeline_test.go`: Core automated test suite covering static artifact single-flight coalescing, TTL concurrent 304 lease renewal sharing, 5xx upstream failure STALE-FALLBACK sharing, and non-200 anti-pollution zero-disk persistence.
- `README-zh.md`: Chinese test documentation.
- `README-en.md`: English test documentation.

---

## 2. Automated Test Execution

Run from the repository root:
```bash
go test -v -count=1 ./tests/338_unified_download_pipeline/...
```

---

## 3. Verified Scenarios

1. **Static Binary Artifact Single-Flight Coalescing (`TestStaticDownloadCoalescing`)**:
   - 20 concurrent clients fetch the same static binary artifact; upstream strictly receives exactly 1 request;
   - All 20 clients receive complete and intact content, with safe atomic file renaming on disk.
2. **Dynamic Metadata Concurrent 304 Lease Renewal (`TestDiskTTLRevalidationCoalescing`)**:
   - 25 concurrent clients request expired TTL metadata; upstream receives only 1 conditional revalidation probe;
   - 304 result is shared and local file `mtime` is touched, with all clients receiving 200 OK annotated with `REVALIDATED-304`.
3. **Upstream 500 Failure STALE-FALLBACK Sharing (`TestDiskTTLStaleFallback`)**:
   - 15 concurrent clients hit an upstream 500 internal server error, smoothly triggering shared `STALE-FALLBACK` to return cached content.
4. **Non-200 Pass-Through & Zero-Disk Anti-Pollution (`TestNon200PassthroughAntiPollution`)**:
   - Streams 404 responses directly to clients with zero disk caching and zero temporary file pollution.
