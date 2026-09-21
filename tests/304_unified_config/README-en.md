# Unified Proxy Configuration System Test Guide

This document describes how to test the unified proxy configuration system in `url-proxy`.

> **Note**: All instructions in this guide assume that your terminal's **current working directory is this test directory** (`tests/304_unified_config/`).

---

## 1. Test Objectives & Concepts

Verify the unified configuration loading and multi-tier discovery mechanism:
1. **Multi-Directive Parsing**: Accurately parse `alias`, `forward`, `bypass`, and `cache` directives.
2. **Explicit CLI Specification (`-conf`)**: Load a custom configuration file via `-conf proxy_test.conf` and drive request redirection.
3. **Working Directory Auto-Discovery**: Automatically discover and load `proxy.conf` when present in the current working directory without `-conf`.

---

## 2. Test Environment Setup

The following components are provided in this test directory:
- `mock_server.go`: Mock mirror gateway server (listening on `127.0.0.1:9994`).
- `proxy_test.conf`: Test unified configuration file.

### Step 2.1: Start Mock Mirror Gateway
Open **Terminal 1** in this test directory and run:
```bash
go run mock_server.go
```
*Expected output*: `mock server listening on :9994`

---

## 3. Test Scenarios

### Scenario A: Load Configuration via `-conf` CLI Argument
Open **Terminal 2** in this test directory and start `url-proxy` pointing to `proxy_test.conf`:
```bash
go run ../../url-proxy.go -addr :8888 -conf ./proxy_test.conf
```
*Observe Terminal 2 log*:
```text
proxy config loaded from explicit CLI argument: ./proxy_test.conf
proxy configuration summary [./proxy_test.conf]: 2 aliases, 2 forwards, 4 cache/bypass rules
http server: :8888
```

Open **Terminal 3** and send a request to trigger the `forward` directive:
```bash
curl -i http://localhost:8888/http://example.internal/test/package.deb
```
*Expected response*:
```text
HTTP/1.1 200 OK
Content-Type: text/plain
X-Forwarded-Mock: true

unified proxy forwarding test passed: URI=/mirror/http://example.internal/test/package.deb
```
*Verification Conclusion*: Configuration loaded via `-conf` successfully redirected traffic to the mock server. Press `Ctrl+C` to stop the proxy in Terminal 2.

---

### Scenario B: Working Directory Auto-Discovery of `proxy.conf`
Copy the test configuration to `proxy.conf` in this test directory:
```bash
cp proxy_test.conf proxy.conf
```
Start `url-proxy` in Terminal 2 without the `-conf` flag:
```bash
go run ../../url-proxy.go -addr :8888
```
*Observe Terminal 2 log*:
```text
proxy config file loaded: proxy.conf
proxy configuration summary [proxy.conf]: 2 aliases, 2 forwards, 4 cache/bypass rules
http server: :8888
```
Clean up and stop the proxy (`Ctrl+C`):
```bash
rm proxy.conf
```
*Verification Conclusion*: The proxy automatically detected and loaded `proxy.conf` from the current working directory.

---

## 4. Conclusion
This test suite comprehensively validates unified configuration parsing, CLI override, and local auto-discovery capabilities.
