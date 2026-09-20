# 并发下载优化测试指南

本文档介绍如何测试 `url-proxy` 的并发下载优化机制（Request Coalescing 与 Shared Streaming）。

## 1. 测试原理

为了验证 `url-proxy` 的请求合并与共享流式传输功能，我们需要模拟一个“慢速”的大文件上游服务器：
- 该服务器提供一个约 5MB 的大文件，并以较低速率分块发送（每 40ms 发送 32KB，总耗时约 6 秒）。
- 当客户端 A 发起请求时，作为 **Leader** 开始下载并写入本地临时文件。
- 在下载尚未完成期间，客户端 B 和 C 发起针对同一 URL 的请求，作为 **Follower** 加入下载。
- 如果请求合并与共享流式传输正常工作：
  1. **上游请求次数恒定为 1**：上游慢速服务器在整个过程中仅收到一次下载请求。
  2. **断点追赶与数据复用**：客户端 B 与 C 会先从磁盘快速读取已下载的前半部分，读到文件末尾后等待新数据广播同步。
  3. **数据完整与文件正确落盘**：所有客户端均完整接收到 5MB 数据，传输完成后临时文件被成功重命名为最终缓存文件。

## 2. 准备测试环境

在当前目录下，我们提供了一个简单的 Go 程序 `slow_server.go` 来模拟上述慢速服务器。

### 步骤 2.1：启动模拟慢速服务器
打开一个终端，运行以下命令启动慢速服务器：
```bash
go run slow_server.go
```
*预期输出：`测试用慢速服务器已启动，监听端口 :9998`*

### 步骤 2.2：启动 url-proxy
打开**第二个**终端，在项目根目录下运行：
```bash
go run url-proxy.go -addr :8888
```
*预期输出：`http server: :8888`*

## 3. 执行测试

### 方式一：多终端模拟并发请求
1. 打开**第三个**终端，启动客户端 A（Leader）的下载：
   ```bash
   curl -o file_a.bin http://localhost:8888/http://localhost:9998/slow-file
   ```
2. 紧接着在 1~3 秒内，打开**第四个**终端，启动客户端 B（Follower）的下载：
   ```bash
   curl -o file_b.bin http://localhost:8888/http://localhost:9998/slow-file
   ```

### 方式二：单行命令后台并发请求
在 Linux / macOS / WSL 终端中，一条命令同时触发并发下载：
```bash
curl -s -o file_a.bin http://localhost:8888/http://localhost:9998/slow-file & \
sleep 1 && \
curl -s -o file_b.bin http://localhost:8888/http://localhost:9998/slow-file & \
wait
```

## 4. 观察与验证

### 4.1 上游服务器终端的表现（核心验证 1）
观察 `slow_server` 终端的日志输出：
```text
[2026-03-27T...] [上游] 收到首个下载请求 (Request #1)，开始慢速流式传输...
[2026-03-27T...] [上游] 请求 #1 数据传输完成 (共 5242880 字节)。
```
*验证要点*：整个过程仅收到一个 Request #1，**绝对不会**出现 `[警告] 收到上游重复请求`，证明上游带宽消耗被有效合并为 1 份。

### 4.2 url-proxy 终端的表现（核心验证 2）
观察 `url-proxy` 的日志输出，能够看到清晰的角色划分与请求追踪：
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
*验证要点*：
1. 请求 1 被标识为 `job leader`，并发起底层网络请求。
2. 请求 2 被复用为 `job follower`，无需发起新的上游连接。
3. Leader 完成写入时由于 Follower 仍在读取，安全地跳过了重命名（避免 Windows 锁死）。
4. 最后一个 Follower 关闭读取句柄后，接替触发并成功完成重命名。

### 4.3 客户端文件的完整性校验
检查两份下载结果的大小及校验和：
```bash
# 检查大小（均为 5242880 字节）
ls -l file_a.bin file_b.bin

# 检查 MD5 / SHA256 校验和（应完全一致）
md5sum file_a.bin file_b.bin
```

## 5. 结论
通过上述测试，您可以明确观察到 `url-proxy` 在多个客户端并发请求同一个大文件时，能够精确地实现请求合并与数据复用，极大减轻上游服务器负载，并安全地处理并发读取与文件落盘重命名。
