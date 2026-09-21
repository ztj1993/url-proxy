# APT Proxy End-to-End Integration Verification Test Guide

This document describes how to execute the full end-to-end integration test suite for `url-proxy` APT caching capabilities (Feature 307).

> **Note**: All instructions in this guide assume that your terminal's **current working directory is this test directory** (`tests/307_apt_integration/`).

---

## 1. Test Objectives & End-to-End Lifecycle

This test suite aggregates and validates all 6 previously implemented capabilities:
1. **Cold `apt update` (Dynamic Bypass)**: Fetching `InRelease` and `Packages.xz` streams live data with **zero bytes written to disk**.
2. **Warm `apt update` (304 Passthrough)**: Sending conditional requests with ETags returns `304 Not Modified` with zero upstream payload transfer.
3. **Cold `apt install` (Package Caching)**: The first client to request `hello.deb` pulls it from upstream and safely writes it to local disk cache.
4. **Warm `apt install` (Local Cache Hit)**: Subsequent clients requesting the same package are served instantly from local disk, consuming **zero upstream bandwidth**.
5. **Cross-Mode Cache Sharing**: Accessing the package via short alias (`/ubuntu/...`) vs standard forward proxy (`archive.ubuntu.com/...`) hits the exact same local cache.
6. **Range Slicing & Resuming**: Partial download requests return `206 Partial Content` with correct slice offsets.
7. **Whitelist Anti-False-Positive Protection**: Packages with names containing keywords like `packages` are 100% guaranteed to be cached on disk.

---

## 2. Test Environment Setup

The following components are provided in this test directory:
- `mock_apt_repo.go`: High-fidelity APT repository server (listening on `127.0.0.1:9997`) with live `/stats` auditing.
- `proxy_test.conf`: Full-featured production-like proxy configuration.

### Step 2.1: Start Mock APT Repository
Open **Terminal 1** in this test directory and run:
```bash
go run mock_apt_repo.go
```
*Expected output*: `mock APT repository server listening on 127.0.0.1:9997`

### Step 2.2: Start url-proxy
Open **Terminal 2** in this test directory and run:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*Expected output*: Shows `proxy configuration summary` and `http server: :8888`.

---

## 3. Test Execution Scenarios

Open **Terminal 3** and execute the following steps:

### Phase 1: Simulated `apt update` (Cold & Warm)
1. **Cold Update**: Fetch metadata
   ```bash
   curl -s http://localhost:8888/ubuntu/dists/jammy/InRelease
   curl -s http://localhost:8888/ubuntu/dists/jammy/main/binary-amd64/Packages.xz >/dev/null
   ```
   *Verification*: Check `./test_cache` to confirm **zero metadata files exist on disk**.
2. **Warm Update (304 Revalidation)**: Re-check with ETag
   ```bash
   curl -i -H 'If-None-Match: "inrelease-v1.0"' http://localhost:8888/ubuntu/dists/jammy/InRelease
   ```
   *Expected response*: `HTTP/1.1 304 Not Modified`.

### Phase 2: Simulated `apt install` (Cold & Warm)
1. **Client 1 Install (Cold Pull)**:
   ```bash
   curl -i http://localhost:8888/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb -o hello_client1.deb
   ```
   *Verification*: Check `./test_cache` to confirm `hello_2.10-2ubuntu2_amd64.deb` is **fully cached on disk**.
2. **Client 2 Install (Warm Cache Hit)**:
   ```bash
   curl -i http://localhost:8888/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb -o hello_client2.deb
   ```
   *Verification*: Query `/stats` to confirm upstream download count remains 1:
   ```bash
   curl -s http://127.0.0.1:9997/stats
   ```

### Phase 3: Cross-Mode Cache Sharing
Fetch via standard forward proxy:
```bash
curl -x http://localhost:8888 -s http://archive.ubuntu.com/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb -o hello_client3.deb
```
*Verification*: Served directly from the cache generated in Phase 2; upstream download count does not increment.

### Phase 4: Range Resuming
Fetch the first 500 bytes:
```bash
curl -i -r 0-499 http://localhost:8888/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb
```
*Expected response*: `HTTP/1.1 206 Partial Content` with `Content-Range: bytes 0-499/10240`.

### Phase 5: Anti-False-Positive Validation
Fetch a package containing `packages` in its name:
```bash
curl -i http://localhost:8888/ubuntu/pool/main/p/packages-tool/packages-tool_1.0_amd64.deb
```
*Verification*: Check `./test_cache` to confirm the package was **successfully cached on disk**.

---

## 4. Summary & Network Audit
Query `http://127.0.0.1:9997/stats` to review upstream resource consumption. Validates uncacheable metadata delivery, 304 fast paths, 100% package cache hits, and cross-mode cache co-location.
