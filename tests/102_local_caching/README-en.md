# Local File Caching Test Guide

This document describes how to test the Local File Caching functionality of `url-proxy`.

## 1. Test Principle

Verify the cache-miss backfill and cache-hit mechanisms of `url-proxy`:
1. **First Request (Cache Miss)**: The proxy fetches the resource from the upstream server, streams data back to the client, and persists it to the `-cache` directory.
2. **Second Request (Cache Hit)**: When the client requests the same URL again, the proxy detects the complete local file and serves it directly from disk, **making zero upstream network requests**.

## 2. Setting Up the Test Environment

We provide a simple Go program `caching_server.go` in the current directory to simulate the upstream server and count incoming requests.

### Step 2.1: Start the simulated upstream server
Open a terminal and run the following command to start the server:
```bash
go run caching_server.go
```
*Expected output: `测试用缓存验证上游服务器已启动，监听端口 :9992`*

### Step 2.2: Start url-proxy (with a custom test cache directory)
Open a **second** terminal and run the following in the project root:
```bash
mkdir -p .test_cache
go run url-proxy.go -addr :8888 -cache ./.test_cache
```
*Expected output: `cache dir: ./.test_cache`*

## 3. Running the Test

Open a **third** terminal and issue two identical download requests sequentially:

1. **First request (triggers upstream download and cache file creation)**:
   ```bash
   curl -i http://localhost:8888/http://localhost:9992/cache-file
   ```

2. **Second request (triggers local cache hit)**:
   ```bash
   curl -i http://localhost:8888/http://localhost:9992/cache-file
   ```

## 4. Observations and Verification

### 4.1 Upstream Server Output (Core Verification)
Observe the log in the `caching_server` terminal:
- On the first request:
  ```text
  [...] [上游服务器] 收到下载请求 #1
  ```
- On the second request: **No new log output appears**.
- Check total upstream requests via curl:
  ```bash
  curl http://localhost:9992/stats
  # Output: upstream_requests: 1
  ```
  This proves the second request was entirely fulfilled by the local disk cache.

### 4.2 url-proxy Output Verification
Observe proxy terminal logs:
```text
# First request (Download and save)
[1] req: http://localhost:9992/cache-file
[1] job leader: http://localhost:9992/cache-file
[1] job download: http://localhost:9992/cache-file
[1] job finished: http://localhost:9992/cache-file
[1] job renamed successfully: ...

# Second request (Direct cache hit)
[2] req: http://localhost:9992/cache-file
[2] job leader: http://localhost:9992/cache-file
[2] job cached: http://localhost:9992/cache-file
```

### 4.3 Cache Directory Verification
Inspect the `.test_cache` directory to verify the cached file structure:
```bash
ls -la .test_cache/http-localhost-9992/
```

## 5. Conclusion
Through this test, you can verify that `url-proxy` accurately caches files and serves repeated requests with zero upstream overhead.
