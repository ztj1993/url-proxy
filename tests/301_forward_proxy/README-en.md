# Standard Forward HTTP Proxy Protocol Test Guide

This document describes how to test the Standard Forward HTTP Proxy Protocol support in `url-proxy`.

## 1. Test Principle

Verify that clients can direct traffic to `url-proxy` without prefixing the proxy address in the URL path, using standard proxy configurations (e.g. `curl -x`, environment variable `http_proxy`, or APT directive `Acquire::http::Proxy`):
- The client sends a standard absolute URI request: `GET http://localhost:9996/proxy-test HTTP/1.1`
- `url-proxy` automatically detects and extracts the target URI, performing standard upstream download, stream relaying, and local caching.
- Path-prefix mode (`GET /http://...`) remains completely supported.

## 2. Setting Up the Test Environment

We provide a simple Go program `mock_server.go` in the current directory to simulate the upstream target server.

### Step 2.1: Start the simulated target server
Open a terminal and run the following command to start the server:
```bash
go run mock_server.go
```
*Expected output: `测试用标准代理目标服务器已启动，监听端口 :9996`*

### Step 2.2: Start url-proxy
Open a **second** terminal and run the following in the project root:
```bash
go run url-proxy.go -addr :8888
```
*Expected output: `http server: :8888`*

## 3. Running the Test

Open a **third** terminal and test using both standard forward proxy and path compatibility modes:

### Method 1: Using curl -x
```bash
curl -i -x http://localhost:8888 http://localhost:9996/proxy-test
```

### Method 2: Using http_proxy environment variable
```bash
http_proxy=http://localhost:8888 curl -i http://localhost:9996/proxy-test
```

### Method 3: Verifying path-prefix compatibility
```bash
curl -i http://localhost:8888/http://localhost:9996/proxy-test
```

## 4. Observations and Verification

### 4.1 curl Response Verification
Check the terminal output:
```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
X-Forward-Proxy-Mock: true
...

url-proxy standard forward proxy test: PASSED
```
*Key checks*:
1. The client successfully requests `http://localhost:9996/proxy-test` via proxy flag `-x`.
2. The response header `X-Forward-Proxy-Mock: true` is forwarded intact.

### 4.2 url-proxy Output Verification
Observe proxy terminal logs:
```text
[1] req: http://localhost:9996/proxy-test
[1] job leader: http://localhost:9996/proxy-test
[1] job download: http://localhost:9996/proxy-test
[1] job finished: http://localhost:9996/proxy-test
```
The proxy extracts the full `http://localhost:9996/proxy-test` target and dispatches it seamlessly.

## 5. Conclusion
Through this test, you can verify that `url-proxy` natively supports standard forward HTTP proxy semantics, allowing APT and other standard tools to integrate with zero URL modification.
