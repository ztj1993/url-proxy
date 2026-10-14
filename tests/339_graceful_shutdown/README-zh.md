# Feature 339: 优雅停机与进程生命周期治理测试套件

本目录提供 Feature 339（优雅停机与进程生命周期治理方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 339 专用测试配置文件，声明基础监听与时效规则。
- `mock_server.go`: 模拟上游源站（提供慢速分块流式传输、快速文件传输与请求计数）。
- `graceful_shutdown_test.go`: 核心自动化测试套件，全面覆盖在途 HTTP 请求平稳排空、活跃下载任务取消与临时文件清理、CONNECT 劫持隧道切断与连接池回收、生命周期调用幂等性等场景。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/339_graceful_shutdown/...
```

---

## 3. 测试覆盖场景

1. **在途 HTTP 请求平稳排空 (`TestGracefulShutdownHTTPDrain`)**：
   - 在慢速文件传输中途触发 `server.Shutdown`；
   - 验证后续新连接被立即拒绝，而已经在途的 HTTP 请求能在宽限超时内平稳完整接收数据。
2. **活跃下载任务取消与临时文件清理 (`TestGracefulShutdownJobCancellationAndTempFileCleanup`)**：
   - 在并发下载进行中调用 `downMgr.Shutdown()`；
   - 验证任务 context 接收到取消信号，`job.Err` 准确记录为 `context.Canceled`；
   - 验证未完成的临时文件（`.tmp`）被安全清除，绝不重命名为最终缓存文件。
3. **CONNECT 劫持隧道主动回收与空闲连接池释放 (`TestGracefulShutdownConnectTunnelCleanup`)**：
   - 建立双向长连接穿透代理，调用 `proxyServer.Close()`；
   - 验证被劫持的 TCP Socket 在毫秒级内被主动切断，数据中继协程优雅退出；
   - 验证出向 HTTP 客户端的 Transport 空闲连接池被完全释放。
4. **生命周期接口调用幂等性 (`TestGracefulShutdownManagerIdempotence`)**：
   - 多次重复调用 `downMgr.StopWatchdog()` 与 `downMgr.Shutdown()`；
   - 验证内部同步屏障有效拦截重复操作，零 panic 与零死锁。
