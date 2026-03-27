# 空闲超时清理机制测试指南

本文档介绍如何测试 `url-proxy` 的空闲超时清理机制（Idle Timeout Cleanup）。

## 1. 测试原理

为了验证 `url-proxy` 能够正确清理挂起的连接，我们需要模拟一个“故障”的上游服务器：
- 该服务器在接收到请求后，会正常返回 HTTP Header 和少量初始数据。
- 随后，该服务器进入无限期休眠，**不再发送任何数据，也不主动断开 TCP 连接**。

如果 `url-proxy` 的清理机制工作正常，它应该在达到配置的 `-idle-time` 后，主动切断与该服务器的连接，清理本地临时文件，并在日志中记录该事件。

## 2. 准备测试环境

在 `tests/idle_timeout` 目录下，我们提供了一个简单的 Go 程序 `hang_server.go` 来模拟上述故障服务器。

### 步骤 2.1：启动模拟故障服务器
打开一个终端，运行以下命令启动故障服务器：
```bash
cd tests/idle_timeout
go run hang_server.go
```
*预期输出：`Test hang server listening on :9999`*

### 步骤 2.2：启动 url-proxy（设置极短的超时时间）
为了避免漫长的等待，我们在启动 `url-proxy` 时，将 `-idle-time` 设置为一个很短的时间（例如 10 秒）。
打开**第二个**终端，在项目根目录下运行：
```bash
go run url-proxy.go -addr :8888 -idle-time 10s
```
*预期输出：`watchdog started: idle-time=10s, internal-check-time=3.333333333s`*

## 3. 执行测试

在**第三个**终端中，使用 `curl` 通过 `url-proxy` 请求故障服务器：

```bash
curl http://localhost:8888/http://localhost:9999/hang
```

## 4. 观察与验证

### 4.1 `curl` 终端的表现
你会看到 `curl` 输出了初始数据：
```
Hello, this is the start of the data...
```
然后 `curl` 会卡住，等待剩余的 99MB 数据。

### 4.2 `url-proxy` 终端的表现（核心验证）
观察 `url-proxy` 的日志输出，你应该能看到类似以下的流程：

1. **请求接入并开始下载**：
   ```text
   [1] req: http://localhost:9999/hang
   [1] job leader: http://localhost:9999/hang
   [1] job download: http://localhost:9999/hang
   ```
2. **等待约 10 秒后，Watchdog 触发清理**：
   ```text
   watchdog: killing idle job: http://localhost:9999/hang, inactive for 10.002s
   ```
3. **底层连接被强制取消，触发清理逻辑**：
   ```text
   [1] job cancelled due to idle timeout: http://localhost:9999/hang
   [1] job finished: http://localhost:9999/hang
   [1] rename skipped: tmp file does not exist... (或尝试重命名失败，因为文件不完整)
   ```

### 4.3 `curl` 终端的最终表现
在 `url-proxy` 强制断开连接后，您的 `curl` 命令也会因为连接被服务器（代理）重置而报错退出：
```text
curl: (18) transfer closed with 99999961 bytes remaining to read
```

## 5. 结论
通过上述测试，您可以明确观察到 `url-proxy` 能够敏锐地捕捉到上游服务器的挂起状态，并利用 `context.Cancel` 干净利落地切断连接并回收资源，防止代理服务器因僵尸连接耗尽内存。
