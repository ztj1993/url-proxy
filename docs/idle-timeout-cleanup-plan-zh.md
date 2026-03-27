# 空闲超时与清理机制优化方案

## 1. 优化目标 (Optimization Goal)
解决因网络异常（如上游服务器停止响应但连接未断开）导致并发下载任务永久挂起（Hang）的问题。实现一套健壮的机制来检测停滞的下载，优雅地中止它们，并清理相关资源，以防止内存和文件句柄泄漏。

## 2. 核心机制 (Core Mechanism)
采用 **“空闲超时巡检 + Context 取消”** 的策略。

- **空闲超时 (Idle Timeout)**：不使用全局的绝对超时（这不适合大文件），而是监控自上次收到数据字节以来的流逝时间。
- **后台巡检 (Watchdog Routine)**：启动一个后台 Goroutine，定期扫描所有活跃的 `DownloadJob` 实例，检查其是否已停滞。
- **Context 取消 (Context Cancellation)**：利用 Go 的 `context.Context` 强行终止挂起的 HTTP 请求，从而立即触发 Leader 下载循环中的清理逻辑。

## 3. 技术方案 (Technical Solution)

### 3.1 状态管理 (`DownloadJob` 更新)
扩展 `DownloadJob` 结构体，增加活跃度追踪和取消支持：

```go
type DownloadJob struct {
    // ... 现有字段 ...
    LastActive time.Time          // 最后一次成功读取数据的时间戳
    Cancel     context.CancelFunc // 用于中止上游请求的函数
}
```

### 3.2 携带 Context 的 Leader 请求
当 Leader 发起下载时，将请求包装在一个可取消的 Context 中：

1.  创建 Context：`ctx, cancel := context.WithCancel(context.Background())`。
2.  将 `cancel` 存入 `DownloadJob`。
3.  执行请求：`req, _ := http.NewRequestWithContext(ctx, "GET", targetUri, nil)`。

### 3.3 活跃度追踪 (Heartbeat)
在 Leader 的 `resp.Body.Read` 循环中，每次成功读取数据时更新 `LastActive`：

```go
n, err := resp.Body.Read(buf)
if n > 0 {
    job.Mu.Lock()
    job.LastActive = time.Now()
    job.Mu.Unlock()
    // ... 处理数据 ...
}
```

### 3.4 后台巡检 (Background Watchdog)
引入一个全局的后台 goroutine 来监控所有任务：

1.  使用 `time.NewTicker` 定时执行（例如每 1 分钟）。
2.  遍历全局的 `jobs` 字典。
3.  如果 `time.Since(job.LastActive) > 空闲超时阈值`（例如 5 分钟）：
    - 调用 `job.Cancel()`。
    - 记录强制终止的日志。

### 3.5 清理流程与错误处理 (Cleanup & Error Handling Flow)
当调用 `job.Cancel()` 或发生其他网络错误时：
1.  Leader 的 `resp.Body.Read` 会立即返回 `context.Canceled` 或相应的网络错误。
2.  Leader 记录错误状态 (`job.Err = err`)，退出读取循环，并执行其 `defer` 块。
3.  `defer` 块安全地执行以下操作：
    - 从全局字典中移除该任务 (`removeJob`)。
    - 唤醒所有正在等待的 Followers (`job.Cond.Broadcast()`)，并关闭 `Done` 通道。
    - **清理损坏数据**：在最终的 `tryRename` 阶段，如果检测到任务存在错误 (`job.Err != nil`)，**绝对不进行重命名**，而是直接使用 `os.Remove(job.TmpName)` 删除不完整的临时文件。
    - **Follower 通知**：被唤醒的 Follower 读到 EOF 后，如果检查到 `job.Err != nil`，也会立刻中断与客户端的连接并返回错误，防止客户端收到损坏的缓存。

## 4. 预期收益 (Expected Benefits)
1.  **高可靠性**：有效防止“僵尸”任务永久占用内存和文件描述符。
2.  **数据一致性**：下载中途失败时（如 10GB 下载到 9GB 报错），系统会自动清理残缺的临时文件，同时切断当前所有 Follower 的连接。后续的新请求将作为新 Leader 从头开始下载，彻底杜绝了将损坏文件当作合法缓存提供给用户的风险。
3.  **大文件友好**：通过关注“空闲时间”而非“总耗时”，即使是极慢速下载的超大文件也不会被错误地终止。
4.  **干净的退出**：利用 Go 原生的 `context` 确保 TCP 连接被正确切断，且所有相关的 goroutine 锁和通道都能被安全释放。
