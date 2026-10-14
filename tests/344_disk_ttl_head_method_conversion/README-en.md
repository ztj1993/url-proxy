# Feature 344: Disk TTL HEAD Method Conversion Test Suite

## 1. Test Objectives
Verify the hardened implementation of the disk TTL policy in `internal/proxy/disk_ttl.go`:
1. **Uncached Initial HEAD Converted to GET Full Persistence**: When a client issues an initial `HEAD` request for an uncached resource, the upstream server receives a `GET` request, the downstream client receives 200 OK with strictly 0 body bytes; the local disk successfully persists the complete payload, avoiding 0-byte file corruption; subsequent `GET` requests hit the fresh cache (`HIT-DISK-TTL`) with complete content;
2. **Expired Content Update with Atomic Replacement**: When a stale file expires and the upstream content is updated, a client `HEAD` request triggers an atomic replacement of the old file with the complete new payload rather than overwriting it with an empty file; subsequent `GET` receives the new payload;
3. **Expired Content Unchanged with 304 Lease Renewal**: When expired and upstream content is unchanged, a client `HEAD` request triggers a 304 response, local `mtime` is renewed, and the response is marked `REVALIDATED-304`;
4. **Concurrent HEAD and GET Coalescing**: When HEAD arrives first as Leader and GET arrives as Follower, only 1 upstream request is made, and both requests successfully receive 0 body bytes and full payload respectively.

---

## 2. Test Suite Structure
- `proxy_test.conf`: Test proxy configuration defining 100ms TTL rules;
- `mock_server.go`: Mock upstream server supporting HEAD headers-only, GET full bodies, and If-Modified-Since 304 revalidation;
- `head_conversion_test.go`: Automated test cases covering the 4 core verification scenarios;
- `README-zh.md` / `README-en.md`: Bilingual test documentation.

---

## 3. Running Tests

```bash
# Run this standalone test suite
go test -v -count=1 ./tests/344_disk_ttl_head_method_conversion/...

# Full regression test
go test -v -count=1 ./tests/...
```
