# Feature 342: Response Header Safety & Rewrite Sanitization Test Suite

This directory provides the standalone test environment and automated test suite for Feature 342 (Inbound Response Header RFC 9110 Hop-by-Hop Filtering, Rewrite Deep Sanitization, Non-Standard Gzip Decompression on Disk, and Ecosystem Strong Verification Relay).

---

## 1. Directory Structure

- `proxy_test.conf`: Feature 342 dedicated test configuration, declaring rewrite rules, dynamic bypass rules, and disk TTL metadata rules.
- `mock_server.go`: Mock upstream server returning static and dynamic hop-by-hop response headers, rewritable HTML payloads, forced gzip streams, Docker registry manifests, and static binary files.
- `response_safety_test.go`: Core automated test suite covering response hop-by-hop stripping, rewrite header sanitization, forced gzip transparent disk decompression, and ecosystem digest/X-Cache-Status observability.
- `README-zh.md`: Chinese test documentation.
- `README-en.md`: English test documentation.

---

## 2. Running Automated Tests

Run the following command in the project root:
```bash
go test -v -count=1 ./tests/342_response_header_safety/...
```

---

## 3. Test Scenarios Covered

1. **RFC 9110 Hop-by-Hop Response Header Stripping (`TestResponseHopByHopStripping`)**:
   - Verifies upstream static hop-by-hop headers (`Transfer-Encoding: chunked`, `Keep-Alive`, `Upgrade`, `Proxy-Connection`, `Connection: close`) are stripped, eliminating HTTP/2 `PROTOCOL_ERROR` and abrupt TCP connection resets;
   - Verifies dynamically declared hop-by-hop headers via `Connection` (such as `X-Dynamic-Upstream-Hop`) are parsed and filtered;
   - Verifies end-to-end business headers (`Server`, `X-Custom-Echo`) are relayed safely.

2. **Rewrite Response Header Sanitization (`TestRewriterResponseHeaderSanitization`)**:
   - Verifies invalid checksum headers (`ETag`, `Content-MD5`, `Digest`) tied to stale bodies are deleted after dynamic body modification;
   - Verifies `Accept-Ranges` and `Content-Encoding` are cleared to prevent client range errors or decompression failures;
   - Verifies `Content-Length` is recalculated to match the exact rewritten byte length.

3. **Forced Gzip Static Download Decompression Guard (`TestForcedGzipStaticDownloadDecompression`)**:
   - Simulates non-standard upstreams forcing `Content-Encoding: gzip` on static binary downloads;
   - Verifies the proxy transparently decompresses the stream so that the disk cache stores 100% raw uncompressed data (checking absence of `0x1f 0x8b` magic bytes);
   - Verifies `Content-Encoding` is stripped from client responses, allowing seamless raw reading.

4. **Ecosystem Strong Verification Relay & X-Cache-Status Observability (`TestEcosystemDigestAndCacheStatusObservability`)**:
   - Verifies `Docker-Content-Digest` and other strong verification headers pass through with absolute fidelity;
   - Verifies static download injects `X-Cache-Status: MISS` on initial fetch and `X-Cache-Status: HIT` on subsequent cached requests.
