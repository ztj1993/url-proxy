# Request Coalescing Optimization Plan

## 1. 优化目标 (Optimization Goal)
针对多个用户同时请求同一个大文件（Large File）的场景，减少对上游服务器的重复请求，节省带宽，并提升并发下载体验。

## 2. 核心机制 (Core Mechanism)
采用 **Request Coalescing (请求合并)** 与 **Shared Streaming (共享流式传输)** 相结合的策略。

- **请求合并**：当多个用户请求同一资源时，仅由第一个用户（Leader）发起一次远程下载请求。
- **共享流式传输**：后续用户（Followers）直接从 Leader 正在下载的临时文件中读取数据，实现数据复用。

## 3. 技术方案 (Technical Solution)

### 3.1 状态管理 (`DownloadJob`)
引入 `DownloadJob` 结构体来管理并发下载任务的状态：

```go
type DownloadJob struct {
    Uri        string
    FilePath   string       // 本地临时文件路径
    Header     http.Header  // 响应头
    HeaderDone chan struct{} // 通知 Header 已就绪
    Done       chan struct{} // 通知下载已完成
    Cond       *sync.Cond    // 用于广播“有新数据写入”
    Mu         sync.Mutex    // 配合 Cond 使用
}
```

### 3.2 实时广播 (`sync.Cond`)
利用 Go 语言的 `sync.Cond` 实现高效的“发布/订阅”模式：
- **发布者 (Leader)**：每写入一块数据到临时文件，调用 `Cond.Broadcast()` 唤醒所有等待者。
- **订阅者 (Follower)**：读取完当前所有可用数据后，调用 `Cond.Wait()` 进入休眠，等待新数据到达。

### 3.3 文件系统缓存 (File System Buffer)
利用本地文件系统作为缓冲区，实现**断点追赶 (Catch-up)**：
- **Leader**：将数据追加写入临时文件。
- **Follower**：打开同一个临时文件，从头（Offset 0）开始读取。
    - **追赶阶段**：以磁盘 IO 速度快速读取已下载的部分（如前 50%）。
    - **同步阶段**：读到文件末尾（EOF）后，自动切换为 `Wait` 模式，实时跟随 Leader 的写入进度。

## 4. 工作流程 (Workflow)

### 场景演示：下载 1GB 文件
1.  **T=0 (Leader)**：用户 A 发起请求。
    -   系统创建 `DownloadJob`。
    -   开始下载并写入 `temp.dat`。
2.  **T=5 (Follower)**：用户 B 发起相同请求。
    -   此时 `temp.dat` 已有 500MB 数据。
    -   系统发现 `DownloadJob` 存在，B 加入为 Follower。
    -   B 打开 `temp.dat`，**瞬间读取**前 500MB 数据。
3.  **T=5.1 (Sync)**：B 读完 500MB，追上 A 的进度。
    -   B 进入 `Cond.Wait()` 状态。
    -   A 每写入 32KB，B 也会收到通知并发送给客户端。
4.  **T=10 (Finish)**：下载完成。
    -   A 关闭 `Done` 通道。
    -   `temp.dat` 重命名为最终缓存文件。
    -   所有用户完成下载。

## 5. 预期收益 (Benefits)
1.  **带宽节省**：无论多少用户并发下载，上游带宽消耗恒定为 1 份。
2.  **即时响应**：后加入的用户能立即获得已下载的数据，无须等待。
3.  **资源高效**：利用文件系统缓存，内存占用极低（仅需少量 Buffer）。
4.  **并发安全**：通过 `sync.Mutex` 和 `sync.Cond` 严格控制并发，避免竞态条件。
