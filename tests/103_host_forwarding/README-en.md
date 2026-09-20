# Host Forwarding Rules Test Guide

This document describes how to test the Host Forwarding Rules configuration in `url-proxy`.

## 1. Test Principle

Verify that `url-proxy` rewrites requests for specific hosts based on `forwarding.conf`:
- Rule definition: `example.internal http://localhost:9993/mirror`
- When a client requests `http://localhost:8888/http://example.internal/package.tar.gz`, `url-proxy` dynamically rewrites the upstream request to `http://localhost:9993/mirror/http://example.internal/package.tar.gz`, redirecting traffic to an internal mirror gateway.

## 2. Setting Up the Test Environment

We provide a simple Go program `forward_server.go` to simulate the mirror gateway and a sample `forwarding.conf` in the current directory.

### Step 2.1: Start the simulated mirror gateway server
Open a terminal and run the following command to start the server:
```bash
go run forward_server.go
```
*Expected output: `测试用镜像转发服务器已启动，监听端口 :9993`*

### Step 2.2: Start url-proxy (loading the forwarding configuration)
`url-proxy` automatically detects `forwarding.conf` in its working directory.
Open a **second** terminal, copy the configuration to the project root, and launch the proxy:
```bash
# Copy configuration to root directory
cp tests/103_host_forwarding/forwarding.conf ./forwarding.conf

# Start url-proxy
go run url-proxy.go -addr :8888
```
*Expected output: Contains `forward config file loaded: forwarding.conf`*

## 3. Running the Test

Open a **third** terminal and use `curl` to request the configured private domain `example.internal`:
```bash
curl -i http://localhost:8888/http://example.internal/package.tar.gz
```

## 4. Observations and Verification

### 4.1 curl Response Verification
Check the terminal output:
```text
HTTP/1.1 200 OK
Content-Type: text/plain
X-Forwarded-Mock: true
...

url-proxy host forwarding test: PASSED (target: /mirror/http://example.internal/package.tar.gz)
```
*Key checks*:
1. The client receives a successful response for the internal non-routable domain `example.internal`.
2. The response includes the `X-Forwarded-Mock: true` header from the mirror gateway.

### 4.2 forward_server Output Verification
Observe the `forward_server` log:
```text
[...] [镜像网关] 成功接收到被转发重写的请求: /mirror/http://example.internal/package.tar.gz
```

### 4.3 url-proxy Output Verification
Observe proxy terminal logs:
```text
[1] req: http://example.internal/package.tar.gz
[1] job leader: http://example.internal/package.tar.gz
[1] job download: http://localhost:9993/mirror/http://example.internal/package.tar.gz
```
The log explicitly confirms `job download` is redirected with prefix `http://localhost:9993/mirror/`.

## 5. Conclusion
Through this test, you can verify that `url-proxy` matches target hosts and applies prefix rewrites according to `forwarding.conf`.
