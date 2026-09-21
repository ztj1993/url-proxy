# Configurable Cache Bypass Rules & Anti-False-Positive Whitelisting Test Guide

This document describes how to test the configurable cache bypass rules and anti-false-positive engine in `url-proxy` (Feature 305).

> **Note**: All instructions in this guide assume that your terminal's **current working directory is this test directory** (`tests/305_selective_cache/`).

---

## 1. Test Objectives & Concepts

Verify dynamic/static content separation and anti-false-positive protection driven by `proxy.conf`:
1. **Dynamic Metadata Bypass**: Requests matching `bypass /dists/ exact InRelease` are streamed transparently with **zero bytes written to disk cache**.
2. **Whitelist Anti-False-Positive Enforcement**: Querying `/pool/main/p/packages-base/packages-base_1.0_amd64.deb`—despite containing `packages` and having a `bypass /dists/ prefix Packages` rule—matches `cache * suffix .deb`, giving it absolute priority and ensuring it is **100% safely cached to disk**.
3. **Context Scoping**: Querying `/pool/main/r/release-info/Release`—despite being named `Release`—is outside `/dists/`, so bypass is not triggered and it is **safely cached to disk**.
4. **Conditional Revalidation Relay**: Querying a bypass resource with `If-None-Match` accurately relays the upstream `304 Not Modified` back to the client.

---

## 2. Test Environment Setup

The following components are provided in this test directory:
- `mock_server.go`: Mock APT upstream server (listening on `127.0.0.1:9995`).
- `proxy_test.conf`: Test unified configuration file.

### Step 2.1: Start Mock Upstream Server
Open **Terminal 1** in this test directory and run:
```bash
go run mock_server.go
```
*Expected output*: `mock server listening on 127.0.0.1:9995`

### Step 2.2: Start url-proxy
Open **Terminal 2** in this test directory and run:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*Expected output*: Shows `proxy config loaded from explicit CLI argument: ./proxy_test.conf` and `http server: :8888`.

---

## 3. Test Scenarios

Open **Terminal 3** and execute the following steps:

### Scenario 1: Dynamic Metadata Bypass & Real-Time Delivery
Query dynamic `InRelease` twice in succession:
```bash
curl -s http://localhost:8888/http://127.0.0.1:9995/dists/jammy/InRelease
curl -s http://localhost:8888/http://127.0.0.1:9995/dists/jammy/InRelease
```
*Observations & Verification*:
1. The `Sequence` field in the responses increments (1 then 2), demonstrating live upstream fetching;
2. Check `./test_cache`: verify **no InRelease file exists** (zero disk cache pollution).

### Scenario 2: Whitelist Anti-False-Positive Disk Caching
Query a package whose filename contains the keyword `packages`:
```bash
curl -i http://localhost:8888/http://127.0.0.1:9995/pool/main/p/packages-base/packages-base_1.0_amd64.deb
```
*Observations & Verification*:
1. Response returns `HTTP/1.1 200 OK` and `Accept-Ranges: bytes`;
2. Check `./test_cache`: verify the `.deb` file has been **successfully cached to disk**.

### Scenario 3: Context Scoping Protection
Query a release note named `Release` under `/pool/`:
```bash
curl -i http://localhost:8888/http://127.0.0.1:9995/pool/main/r/release-info/Release
```
*Observations & Verification*:
1. Even though the filename is `Release`, it is outside the `/dists/` scope;
2. Check `./test_cache`: verify the file has been **successfully cached to disk**.

### Scenario 4: 304 Revalidation on Bypass Resource
Send a conditional request with ETag:
```bash
curl -i -H 'If-None-Match: "v1.0"' http://localhost:8888/http://127.0.0.1:9995/dists/jammy/InRelease
```
*Expected response*:
```text
HTTP/1.1 304 Not Modified
ETag: "v1.0"
```
*Verification Conclusion*: Upstream 304 is transparently short-circuited back to the client.

---

## 4. Conclusion
This test suite comprehensively validates configurable cache bypass, whitelist anti-false-positive protection, scope isolation, and transparent 304 revalidation.
