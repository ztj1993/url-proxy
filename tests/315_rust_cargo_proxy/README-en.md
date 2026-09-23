# Feature 315: Rust Cargo Sparse Index & Crates Official Proxy Test Suite

This directory provides a standalone local testing and verification environment for Feature 315 (Rust Cargo Sparse Index Protocol & Crates Official Proxy).

---

## Directory Structure

- `mock_cargo_server.go`: Simulates `index.crates.io` (RFC 2785 sparse index) and `static.crates.io` (Crate source package distribution) on port `9994`. Supports `/config.json`, multi-level sparse index queries, 304 conditional revalidation, and `.crate` source package downloads.
- `proxy_cargo.conf`: Dedicated test configuration for Feature 315.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## Quick Start & Verification

### Step 1: Start Mock Cargo Upstream Service

In Terminal 1:
```bash
go run tests/315_rust_cargo_proxy/mock_cargo_server.go
```
Expected output: `Mock Cargo & Crates Server running on port :9994`.

### Step 2: Start url-proxy Test Instance

In Terminal 2:
```bash
go run url-proxy.go -conf tests/315_rust_cargo_proxy/proxy_cargo.conf -addr :8888
```

---

## Test Scenarios & Verification Steps

### 1. `config.json` Dynamic Response Body Rewriting
```bash
curl -i http://127.0.0.1:8888/crates-index/config.json
```
**Expected Outcome**:
- Returns `HTTP/1.1 200 OK`.
- The `"dl"` field in the JSON body is automatically rewritten to the proxy address: `"dl": "http://127.0.0.1:8888/crates-static/crates"`.
- The original external URL `https://static.crates.io/crates` is replaced.
- Upstream `ETag` is stripped and `Content-Length` is updated.

### 2. Sparse Index Query & 304 Conditional Revalidation
```bash
# Initial query for rand crate (200 OK)
curl -i http://127.0.0.1:8888/crates-index/ra/nd/rand

# Conditional query (304 Not Modified)
curl -i -H 'If-None-Match: "mock-index-etag-123"' http://127.0.0.1:8888/crates-index/ra/nd/rand
```
**Expected Outcome**:
- First request returns HTTP 200 with NDJSON index content.
- Conditional request returns `HTTP/1.1 304 Not Modified` without redundant body transfer.

### 3. Short Crate Names (1, 2, 3 letters)
```bash
curl -i http://127.0.0.1:8888/crates-index/1/a
curl -i http://127.0.0.1:8888/crates-index/2/ab
curl -i http://127.0.0.1:8888/crates-index/3/a/abc
```
**Expected Outcome**: All short paths are properly relayed and streamed.

### 4. Cold Download of `.crate` Package & Local Disk Persistence
```bash
curl -i http://127.0.0.1:8888/crates-static/crates/rand/rand-0.8.5.crate
```
**Expected Outcome**:
- Returns HTTP 200 with crate binary payload.
- Proxy logs show `job leader`, `job download`, and atomic rename to the final cache file.

### 5. Local Range 206 Cache Hit (0 Upstream Requests)
```bash
# Range partial download
curl -i -r 0-15 http://127.0.0.1:8888/crates-static/crates/rand/rand-0.8.5.crate

# Check upstream stats
curl -s http://127.0.0.1:9994/stats
```
**Expected Outcome**:
- Returns `HTTP/1.1 206 Partial Content` directly from local disk cache.
- `crate_requests` counter remains 1 (zero additional upstream requests).
