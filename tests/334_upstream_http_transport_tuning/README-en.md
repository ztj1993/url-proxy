# Feature 334: Upstream HTTP Transport Pool Tuning & Timeout Hardening Test Suite

This directory provides the standalone test environment and automated regression test suite for Feature 334 (Upstream HTTP Transport Pool Tuning & Timeout Hardening).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring service address and forwarding rules.
- `mock_server.go`: Mock upstream server providing slow-header and chunked streaming body endpoints.
- `transport_tuning_test.go`: Automated test suite covering transport parameter verification, response header stall timeout abort, and uninterrupted chunked body streaming.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from repository root:
```bash
go test -v -count=1 ./tests/334_upstream_http_transport_tuning/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Upstream Server
```bash
go run tests/334_upstream_http_transport_tuning/mock_server.go
```
Listens on `127.0.0.1:9998`.

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/334_upstream_http_transport_tuning/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **Response Header Timeout Abort**:
   Query `http://127.0.0.1:8888/http://127.0.0.1:9998/slow-header` and verify prompt termination on timeout.
2. **Persistent Connection Reuse**:
   Send bursts of requests and inspect socket states via netstat/ss to confirm connection reuse without TIME_WAIT accumulation.