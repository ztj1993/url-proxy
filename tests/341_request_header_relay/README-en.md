# Feature 341: Request Header Relay & Hop-by-Hop Filtering Test Suite

This directory provides the standalone test environment and automated test suite for Feature 341 (Outbound Request Header Relay and RFC 9110 Hop-by-Hop Header Stripping Governance).

---

## 1. Directory Structure

- `proxy_test.conf`: Feature 341 dedicated test configuration, declaring static rules, dynamic bypass rules, and disk TTL metadata rules.
- `mock_server.go`: Mock upstream server recording inbound request headers, host, method, and path for verification.
- `header_relay_test.go`: Core automated test suite covering multi-value header fidelity, credential preservation, dynamic hop-by-hop stripping, host isolation, anti-compression guards, and conditional request contamination guards.
- `README-zh.md`: Chinese test documentation.
- `README-en.md`: English test documentation.

---

## 2. Running Automated Tests

Run the following command in the project root:
```bash
go test -v -count=1 ./tests/341_request_header_relay/...
```

---

## 3. Test Scenarios Covered

1. **Bypass Branch Multi-Value Headers & Hop-by-Hop Stripping (`TestBypassHeaderRelay_MultiValueAndDynamicHops`)**:
   - Verifies multi-value headers (`Accept`, `X-Custom-List`) maintain multi-value fidelity without flattening.
   - Verifies credential headers (`Authorization`, `Cookie`) are preserved intact.
   - Verifies static hop-by-hop headers (`Connection`, `Proxy-Connection`, `Keep-Alive`, `Upgrade`) are stripped.
   - Verifies dynamic hop-by-hop headers declared via `Connection` values (e.g., `X-Ephemeral-Hop`) are stripped alongside `Connection`.
   - Verifies outbound `Host` is rewritten to upstream target host rather than inheriting proxy client host.

2. **Static Download Compression Guard & Conditional Anti-Contamination (`TestStaticDownloadHeaderRelay_CompressionAndConditionalGuard`)**:
   - Verifies client `Accept-Encoding: gzip, deflate` is stripped on outbound static requests to prevent compressed payloads from persisting to disk.
   - Verifies `If-None-Match` and `If-Modified-Since` are stripped when no local cache exists, preventing upstream 304s from breaking coalesced download singleflights.
   - Verifies business application headers (such as `User-Agent: apt/...`) pass through transparently.

3. **HEAD and Range Branch Relay (`TestHeadAndRangeHeaderRelay`)**:
   - Verifies un-cached HEAD and Range requests correctly relay `Range` and custom headers while filtering hop-by-hop headers.
