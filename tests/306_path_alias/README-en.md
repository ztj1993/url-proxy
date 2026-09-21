# Path Short Alias Routing & Dynamic Rewriting Test Guide

This document describes how to test the path short alias routing and dynamic rewriting engine in `url-proxy` (Feature 306).

> **Note**: All instructions in this guide assume that your terminal's **current working directory is this test directory** (`tests/306_path_alias/`).

---

## 1. Test Objectives & Concepts

Verify that `url-proxy` correctly parses path short aliases, dynamically rewrites URLs to target upstream BaseURLs, and integrates seamlessly with all existing pipelines:
1. **Short Alias Dynamic Bypass**: Querying `http://localhost:8888/my-ubuntu/dists/jammy/InRelease` dynamically rewrites to `http://127.0.0.1:9996/upstream/ubuntu/dists/jammy/InRelease` and streams live data with **zero bytes written to disk cache**.
2. **Short Alias 304 Revalidation**: Sending `If-None-Match: "v1.0"` across an alias route relays the upstream `304 Not Modified` cleanly to the client.
3. **Short Alias Package Disk Caching**: Querying `http://localhost:8888/my-ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb` rewrites to the upstream package, downloads it, and **100% safely caches the `.deb` file on disk**.
4. **Multi-Alias Routing Isolation**: Querying `http://localhost:8888/my-debian/dists/bookworm/Release` routes independently to Debian upstream endpoints.
5. **Unknown Alias Safety**: Querying unconfigured alias routes safely yields `404 Not Found`.

---

## 2. Test Environment Setup

The following components are provided in this test directory:
- `mock_server.go`: Mock upstream repository server (listening on `127.0.0.1:9996`).
- `proxy_test.conf`: Test unified configuration file declaring `my-ubuntu` and `my-debian` aliases.

### Step 2.1: Start Mock Upstream Server
Open **Terminal 1** in this test directory and run:
```bash
go run mock_server.go
```
*Expected output*: `mock server listening on 127.0.0.1:9996`

### Step 2.2: Start url-proxy
Open **Terminal 2** in this test directory and run:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*Expected output*: Shows `proxy config loaded from explicit CLI argument: ./proxy_test.conf` and `http server: :8888`.

---

## 3. Test Scenarios

Open **Terminal 3** and execute the following steps:

### Scenario 1: Short Alias Dynamic Bypass & Real-Time Delivery
Query dynamic `InRelease` via alias twice in succession:
```bash
curl -s http://localhost:8888/my-ubuntu/dists/jammy/InRelease
curl -s http://localhost:8888/my-ubuntu/dists/jammy/InRelease
```
*Observations & Verification*:
1. The `Sequence` field in the responses increments (1 then 2), demonstrating live upstream fetching;
2. Check `./test_cache`: verify **no InRelease file exists** (zero disk cache pollution).

### Scenario 2: Short Alias 304 Conditional Revalidation
Send a conditional request with ETag:
```bash
curl -i -H 'If-None-Match: "v1.0"' http://localhost:8888/my-ubuntu/dists/jammy/InRelease
```
*Expected response*:
```text
HTTP/1.1 304 Not Modified
ETag: "v1.0"
```
*Verification Conclusion*: Upstream 304 is transparently short-circuited back to the client.

### Scenario 3: Short Alias Package Disk Caching
Query a package file via alias:
```bash
curl -i http://localhost:8888/my-ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb
```
*Observations & Verification*:
1. Response returns `HTTP/1.1 200 OK` and package payload;
2. Check `./test_cache`: verify the `.deb` file has been **successfully cached to disk**.

### Scenario 4: Multi-Alias Isolation
Query the Debian alias route:
```bash
curl -s http://localhost:8888/my-debian/dists/bookworm/Release
```
*Observations & Verification*:
1. Response returns `Origin: Debian`, proving proper routing to Debian upstream;
2. Matches `exact Release` bypass rule, so zero bytes are cached to disk.

### Scenario 5: Unknown Alias Defense
Query an unconfigured alias:
```bash
curl -i http://localhost:8888/not-exist-alias/some/file
```
*Expected response*:
```text
HTTP/1.1 404 Not Found
```

---

## 4. Conclusion
This test suite comprehensively validates path short alias routing, dynamic URL normalization, seamless composition with caching and bypass pipelines, and unknown alias rejection.
