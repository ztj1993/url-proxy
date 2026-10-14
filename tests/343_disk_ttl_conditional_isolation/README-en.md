# Feature 343: Disk TTL Conditional Isolation & Compression Guard Test Suite

This directory provides the standalone test environment and automated test suite for Feature 343 (Disk TTL Conditional Header Isolation, Empty 304 Black Hole Prevention, ETag Hijacking Prevention, and Dirty Lease Renewal Deadlock Prevention).

---

## 1. Directory Structure

- `proxy_test.conf`: Feature 343 dedicated test configuration declaring dynamic metadata rules with `ttl:100ms`.
- `mock_server.go`: Mock origin server strictly evaluating `If-None-Match` and `If-Modified-Since` according to RFC 9110 Section 13.2.2 precedence.
- `conditional_isolation_test.go`: Core automated test suite covering uncached conditional stripping, expired revalidation ETag stripping, stale cache self-healing, and outbound compression guards.
- `README-zh.md`: Chinese test documentation.
- `README-en.md`: English test documentation.

---

## 2. Running Automated Tests

Run the following command in the project root:
```bash
go test -v -count=1 ./tests/343_disk_ttl_conditional_isolation/...
```

---

## 3. Test Scenarios Covered

1. **Uncached First Fetch Conditional Stripping (`TestUncachedFirstFetch_ConditionalStripped`)**:
   - Client sends `If-None-Match`, `If-Modified-Since`, and `Accept-Encoding` on uncached metadata.
   - Verifies outbound request strips all conditional headers and compression headers; origin responds with 200 OK, and local cache is created, eliminating the 304 black hole.

2. **Expired Revalidation ETag Stripping (`TestExpiredRevalidation_ETagStripped_Renew304`)**:
   - Local cache is expired; client sends mismatched `If-None-Match`.
   - Verifies outbound request strips client ETag and uses local `mtime`, origin responds with 304, and proxy successfully renews the lease with `REVALIDATED-304`.

3. **Stale Cache Self-Healing (`TestSelfHealing_ClientNewer_CacheOlder_OriginNewer`)**:
   - Proxy has old version $T_1$, origin has new version $T_2$, client has $T_2$ and sends condition $T_2$.
   - Verifies proxy uses its own local timestamp $T_1$ instead of relaying the client's $T_2$, preventing the "dirty lease renewal deadlock".
   - Verifies origin returns 200 OK, and the proxy updates its local disk cache atomically to the latest version.
