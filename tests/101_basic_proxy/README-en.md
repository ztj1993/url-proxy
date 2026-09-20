# Basic Transparent Proxy Forwarding Test Guide

This document describes how to test the basic transparent relay and HTTP header forwarding capabilities of `url-proxy`.

## 1. Test Principle

Verify that `url-proxy` correctly acts as a transparent pipe forwarding client requests to an upstream server:
- The upstream server returns specific custom headers (such as `X-Custom-Header`, `Content-Type`) and a body.
- When the client requests through the proxy, it should receive identical status codes, headers, and body content.

## 2. Setting Up the Test Environment

We provide a simple Go program `mock_server.go` in the current directory to simulate the upstream server.

### Step 2.1: Start the simulated upstream server
Open a terminal and run the following command to start the server:
```bash
go run mock_server.go
```
*Expected output: `测试用基础上游服务器已启动，监听端口 :9991`*

### Step 2.2: Start url-proxy
Open a **second** terminal and run the following in the project root:
```bash
go run url-proxy.go -addr :8888
```
*Expected output: `http server: :8888`*

## 3. Running the Test

Open a **third** terminal and use `curl -i` to request the upstream resource via the proxy:
```bash
curl -i http://localhost:8888/http://localhost:9991/test-file
```

## 4. Observations and Verification

### 4.1 curl Response Verification
Check the HTTP response in your terminal:
```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Server: Mock-Basic-Server/1.0
X-Custom-Header: ProxyTestPass
...

url-proxy basic transparent proxy test: PASSED
```
*Key checks*:
1. HTTP status code is `200 OK`.
2. Custom response header `X-Custom-Header: ProxyTestPass` is forwarded intact.
3. Response body is completely delivered.

### 4.2 url-proxy Output Verification
Observe the log output in the proxy terminal:
```text
[1] req: http://localhost:9991/test-file
[1] job leader: http://localhost:9991/test-file
[1] job download: http://localhost:9991/test-file
[1] job finished: http://localhost:9991/test-file
```

## 5. Conclusion
Through this test, you can confirm that `url-proxy`'s basic transparent proxying and header relay mechanisms operate properly.
