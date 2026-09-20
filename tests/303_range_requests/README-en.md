# Range Requests and 206 Partial Content Resuming Test Guide

This document describes how to test HTTP Range Requests (RFC 7233 / RFC 9110) and `206 Partial Content` resuming capabilities in `url-proxy`.

## 1. Test Principle

Verify client behavior when issuing sliced range requests (`Range: bytes=start-end`) through `url-proxy`:
1. **Cache Miss**:
   - The proxy relays the `Range` request header upstream.
   - Upstream responds with `HTTP/1.1 206 Partial Content`, and the proxy streams data directly back to the client.
   - **Key Safety Check**: The partial 206 slice is strictly excluded from local disk caching to prevent partial file pollution.
2. **Cache Hit**:
   - After the full file has been cached, subsequent Range slice requests are served from local disk.
   - The proxy extracts the requested byte slice in milliseconds, returning `206 Partial Content` with `Content-Range`, **without sending any requests to the upstream origin**.
3. **Out-of-Bounds Range**:
   - Requests exceeding file boundaries (e.g. `bytes=500-600`) return `HTTP/1.1 416 Requested Range Not Satisfiable`.
4. **Forward Proxy Mode Compatibility**:
   - In combination with Feature 301, `curl -x` forward proxy mode accurately retrieves 206 slices as well.

## 2. Setting Up the Test Environment

A mock origin server `mock_server.go` (listening on port `:9993`, preconfigured with a 100-byte test payload) is provided in the current directory.

### Step 2.1: Start the mock upstream server
In the current directory, open a terminal and run:
```bash
go run mock_server.go
```
*Expected output: `测试用 Range 断点续传源站已启动，监听端口 :9993`*

### Step 2.2: Start url-proxy
Open a **second** terminal, navigate to the current test directory, and run:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./cache
```
*Expected output: `http server: :8888`*

## 3. Running the Tests

Open a **third** terminal and execute the following steps in sequence:

### Phase 1: Range Request on Cache Miss (Pure Streaming Relay)
Request the first 50 bytes (`0-49`) of the uncached resource:
```bash
curl -i -r 0-49 http://localhost:8888/http://localhost:9993/range-test
```
*Expected response*:
```text
HTTP/1.1 206 Partial Content
Accept-Ranges: bytes
Content-Length: 50
Content-Range: bytes 0-49/100
...

01234567890123456789012345678901234567890123456789
```
*Check point*: Check `./cache` directory; confirm **no** partial 50-byte file was written.

---

### Phase 2: Download Full File and Establish Cache
Execute a full GET request:
```bash
curl -i http://localhost:8888/http://localhost:9993/range-test
```
*Expected response*: Returns `200 OK`, `Accept-Ranges: bytes`, `Content-Length: 100`, creating the full 100-byte file in `./cache`.

---

### Phase 3: Local Range Slicing on Cache Hit (Instant response, 0 upstream traffic)
Request the middle 20 bytes (`10-29`) of the cached file:
```bash
curl -i -r 10-29 http://localhost:8888/http://localhost:9993/range-test
```
*Expected response*:
```text
HTTP/1.1 206 Partial Content
Accept-Ranges: bytes
Content-Length: 20
Content-Range: bytes 10-29/100
...

01234567890123456789
```
*Check point*: Check `mock_server` console logs; **no incoming request appears**, proving it was served entirely from the proxy's local cache.

---

### Phase 4: Test Out-of-Bounds Range (416 Status Code)
Request a range beyond 100 bytes (`500-600`):
```bash
curl -i -r 500-600 http://localhost:8888/http://localhost:9993/range-test
```
*Expected response*:
```text
HTTP/1.1 416 Requested Range Not Satisfiable
Content-Range: bytes */100
```

---

### Phase 5: Range Resuming in Forward Proxy Mode
Issue a range request for bytes `0-9` using `curl -x`:
```bash
curl -i -x http://localhost:8888 -r 0-9 http://localhost:9993/range-test
```
*Expected response*: Accurately returns `HTTP/1.1 206 Partial Content` with `Content-Length: 10`.

## 4. Observations and Log Verification

### url-proxy Console Logs:
```text
[1] req: http://localhost:9993/range-test
[1] range relay to upstream: http://localhost:9993/range-test (bytes=0-49)
[2] req: http://localhost:9993/range-test
[2] job leader: http://localhost:9993/range-test
[2] job finished: http://localhost:9993/range-test
[3] req: http://localhost:9993/range-test
[3] range hit local cache: http://localhost:9993/range-test (bytes=10-29)
```

## 5. Conclusion
`url-proxy` delivers comprehensive HTTP Range Requests and 206 Partial Content resuming support, providing solid low-level infrastructure for network disconnect recovery and multi-part download acceleration.
