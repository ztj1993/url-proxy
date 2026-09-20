# Concurrent Download Optimization Test Guide

This document describes how to test the concurrent download optimization mechanisms (Request Coalescing and Shared Streaming) in `url-proxy`.

## 1. Test Principle

To verify the request coalescing and shared streaming features of `url-proxy`, we need to simulate a "slow" large file upstream server:
- The server serves a 5MB large file in chunks at a lower transmission rate (32KB every 40ms, taking around 6 seconds in total).
- When Client A sends a request, it acts as the **Leader**, initiating the remote download and writing to a local temporary file.
- Before the download finishes, Client B and Client C request the same URL and join as **Followers**.
- If request coalescing and shared streaming work properly:
  1. **Upstream requests remain at 1**: The upstream slow server receives only one download request throughout the entire process.
  2. **Catch-up and data reuse**: Client B and C quickly catch up by reading the downloaded portion from disk, and then synchronize with the Leader via data broadcasting.
  3. **Data integrity and atomic rename**: All clients receive the complete 5MB file, and the temporary file is renamed to the final cache path only after all readers have closed their handles.

## 2. Setting Up the Test Environment

We provide a simple Go program `slow_server.go` in the current directory to simulate the slow server.

### Step 2.1: Start the simulated slow server
Open a terminal and run the following command to start the slow server:
```bash
go run slow_server.go
```
*Expected output: `测试用慢速服务器已启动，监听端口 :9998`*

### Step 2.2: Start url-proxy
Open a **second** terminal and run the following in the project root:
```bash
go run url-proxy.go -addr :8888
```
*Expected output: `http server: :8888`*

## 3. Running the Test

### Method 1: Multi-terminal concurrent requests
1. Open a **third** terminal and start downloading with Client A (Leader):
   ```bash
   curl -o file_a.bin http://localhost:8888/http://localhost:9998/slow-file
   ```
2. Within 1~3 seconds, open a **fourth** terminal and start downloading with Client B (Follower):
   ```bash
   curl -o file_b.bin http://localhost:8888/http://localhost:9998/slow-file
   ```

### Method 2: Single-line command with background processes
In Linux / macOS / WSL terminal, trigger concurrent downloads with one command:
```bash
curl -s -o file_a.bin http://localhost:8888/http://localhost:9998/slow-file & \
sleep 1 && \
curl -s -o file_b.bin http://localhost:8888/http://localhost:9998/slow-file & \
wait
```

## 4. Observations and Verification

### 4.1 Upstream server output (Core Verification 1)
Observe the log output in the `slow_server` terminal:
```text
[2026-03-27T...] [上游] 收到首个下载请求 (Request #1)，开始慢速流式传输...
[2026-03-27T...] [上游] 请求 #1 数据传输完成 (共 5242880 字节)。
```
*Key check*: Only Request #1 is received. No warnings about duplicate upstream requests appear, proving that upstream bandwidth consumption is merged into a single connection.

### 4.2 url-proxy output (Core Verification 2)
Observe the log output in the `url-proxy` terminal:
```text
[1] req: http://localhost:9998/slow-file
[1] job leader: http://localhost:9998/slow-file
[1] job download: http://localhost:9998/slow-file
[2] req: http://localhost:9998/slow-file
[2] job follower: http://localhost:9998/slow-file
[1] job finished: http://localhost:9998/slow-file
[1] rename skipped: file is still being read by 1 followers
[2] job done: http://localhost:9998/slow-file
[2] attempting rename: ... -> ...
[2] job renamed successfully: ...
```
*Key checks*:
1. Request 1 is assigned as `job leader` and issues the upstream HTTP GET.
2. Request 2 joins as `job follower` without issuing a new upstream request.
3. When the Leader finishes writing, it skips renaming because the Follower is still reading (preventing Windows file lock issues).
4. The final Follower closing the handle triggers and completes the rename successfully.

### 4.3 Data Integrity Check
Compare the downloaded files:
```bash
# Check file size (should both be 5242880 bytes)
ls -l file_a.bin file_b.bin

# Check MD5 / SHA256 hashes (must be identical)
md5sum file_a.bin file_b.bin
```

## 5. Conclusion
Through this test, you can verify that `url-proxy` correctly coalesces multiple concurrent requests into a single upstream request, streams data concurrently to followers with catch-up capability, and safely manages file locks across platforms.
