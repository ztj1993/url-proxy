# Conditional Requests and 304 Not Modified Passthrough Test Guide

This document describes how to test HTTP Conditional Requests and `304 Not Modified` fast negotiation passthrough in `url-proxy`.

## 1. Test Principle

Verify that when a client issues requests containing conditional headers (`If-Modified-Since` or `If-None-Match`) through the proxy:
- The proxy relays the conditional headers intact to the upstream origin server.
- When the origin confirms the resource is unchanged and returns `HTTP/1.1 304 Not Modified`:
  - The proxy immediately forwards the 304 status code and response headers (such as `ETag`, `Last-Modified`).
  - No response body is downloaded or transmitted (0-byte payload).
  - Existing local cache files remain intact and will not be overwritten or truncated.
- Fresh unconditional requests continue to return `200 OK` and populate the local cache normally.

## 2. Setting Up the Test Environment

A mock server `mock_server.go` (listening on port `:9994`) is provided in the current directory.

### Step 2.1: Start the mock upstream server
In the current directory, open a terminal and run:
```bash
go run mock_server.go
```
*Expected output: `测试用条件协商目标服务器已启动，监听端口 :9994`*

### Step 2.2: Start url-proxy
Open a **second** terminal, navigate to the current test directory, and run:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./cache
```
*Expected output: `http server: :8888`*

## 3. Running the Tests

Open a **third** terminal and execute the following verification steps in sequence:

### Phase 1: Initial Request (No conditional headers, expects 200 OK and cache written)
Issue the first request using `curl -i`:
```bash
curl -i http://localhost:8888/http://localhost:9994/conditional-test
```
*Expected response*:
```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Etag: "packages-v1-hash"
Last-Modified: Sat, 20 Sep 2026 08:00:00 GMT
...

Package: test-pkg
Version: 1.0.0
Architecture: amd64
```
At this point, check `./cache`; the cached file should be created completely.

---

### Phase 2: Conditional Request with ETag (304 Fast Negotiation Passthrough)
Issue a request with the previously obtained ETag (`If-None-Match: "packages-v1-hash"`):
```bash
curl -i -H 'If-None-Match: "packages-v1-hash"' http://localhost:8888/http://localhost:9994/conditional-test
```
*Expected response*:
```text
HTTP/1.1 304 Not Modified
Etag: "packages-v1-hash"
Last-Modified: Sat, 20 Sep 2026 08:00:00 GMT
```
*Key checks*:
1. HTTP status code is `304 Not Modified`.
2. Response body is empty (0 bytes).
3. The cache file in `./cache` remains intact and has not been truncated or damaged.

---

### Phase 3: Conditional Request with Last-Modified (304 Fast Negotiation Passthrough)
Issue a request with the timestamp (`If-Modified-Since: Sat, 20 Sep 2026 08:00:00 GMT`):
```bash
curl -i -H 'If-Modified-Since: Sat, 20 Sep 2026 08:00:00 GMT' http://localhost:8888/http://localhost:9994/conditional-test
```
*Expected response*: Returns `HTTP/1.1 304 Not Modified` with 0-byte body.

---

### Phase 4: Forward Proxy Mode with Conditional Negotiation
Combining Feature 301 (forward proxy mode), send a conditional request using `curl -x`:
```bash
curl -i -x http://localhost:8888 -H 'If-None-Match: "packages-v1-hash"' http://localhost:9994/conditional-test
```
*Expected response*: Returns `HTTP/1.1 304 Not Modified` accurately in forward proxy mode as well.

## 4. Observations and Log Verification

### 4.1 mock_server Logs
```text
[mock_server] 收到请求: GET /conditional-test, If-None-Match: , If-Modified-Since: 
[mock_server] 未命中条件，返回 200 OK 完整数据
[mock_server] 收到请求: GET /conditional-test, If-None-Match: "packages-v1-hash", If-Modified-Since: 
[mock_server] 命中缓存条件，返回 304 Not Modified (0 字节负载)
```

### 4.2 url-proxy Logs
```text
[1] req: http://localhost:9994/conditional-test
[1] job leader: http://localhost:9994/conditional-test
[1] job finished: http://localhost:9994/conditional-test
[2] req: http://localhost:9994/conditional-test
[2] job leader: http://localhost:9994/conditional-test
[2] job not modified (304): http://localhost:9994/conditional-test
[2] 304 response sent: http://localhost:9994/conditional-test
```
The logs clearly indicate that the proxy captures upstream 304 responses, notifies callers directly, and writes back 304 without downloading.

## 5. Conclusion

This test validates that `url-proxy` supports full HTTP conditional request negotiation, integrating seamlessly with APT index verification to accelerate repeated `apt update` runs while conserving network resources.
