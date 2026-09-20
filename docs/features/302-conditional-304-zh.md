# 条件请求与 304 快速协商透传设计方案

## 1. 功能目标 (Goal)
支持 HTTP 条件请求（Conditional Requests）规范。当客户端（如 `apt update`、浏览器或带有 `-z` 参数的 `curl`）携带 `If-Modified-Since` 或 `If-None-Match (ETag)` 时，代理如实将条件头透传给上游服务器；在上游返回 `304 Not Modified` 时直接短路透传状态码与响应头，完全不拉取任何 Body 数据，从而实现极速的元数据查重并大幅节约网络带宽与系统 IO。

## 2. 核心机制 (Core Mechanism)
采用 **条件头透传 (Conditional Header Relay)** 与 **304 响应快速短路 (304 Fast Short-Circuit)**：

1. **客户端条件头原样透传**：
   - 客户端在发起 `apt update` 时会频繁携带最后修改时间 `If-Modified-Since` 或实体标签 `If-None-Match`。
   - 代理构造发往上游的 HTTP 请求时，精准提取并透传这些头信息，交由权威源站判断资源是否发生变动。
2. **304 快速短路与零磁盘 IO**：
   - 当上游判定资源未变动并返回 `http.StatusNotModified (304)` 时：
     - 代理立即复制上游响应头（如 `ETag`、`Date`、`Cache-Control`）并标记状态码为 304；
     - **不创建本地临时文件，不启动数据下载循环**，直接关闭通知并退出 Leader 协程；
     - 客户端处理协程接收到 304 信号后，立即写回 304 状态码并返回，零数据负载传输，现存本地缓存不受任何影响。

## 3. 技术方案 (Technical Solution)

### 3.1 关键条件头透传
在 Leader 发起上游请求时，透传标准条件协商头：
```go
// 透传客户端的条件请求头 (用于 304 缓存协商)
conditionalHeaders := []string{"If-Modified-Since", "If-None-Match"}
for _, h := range conditionalHeaders {
    if val := r.Header.Get(h); val != "" {
        req.Header.Set(h, val)
    }
}
```

### 3.2 Leader 304 短路退出
```go
// 设置 Job 信息
job.StatusCode = resp.StatusCode
copyHeader(job.Header, resp.Header)

// 如果上游返回 304 Not Modified，无需拉取 Body，直接短路通知并返回
if resp.StatusCode == http.StatusNotModified {
    log.Printf("[%d] job not modified (304): %s", reqID, uri)
    close(job.HeaderDone)
    return
}
```

### 3.3 客户端流式处理的 304 拦截
```go
// 发送响应头
copyHeader(w.Header(), job.Header)
w.WriteHeader(job.StatusCode)

// 如果是 304 Not Modified，无需读取和传输 Body，直接返回
if job.StatusCode == http.StatusNotModified {
    log.Printf("[%d] 304 response sent: %s", reqID, uri)
    return
}
```

## 4. 预期收益 (Benefits)
1. **APT 更新大幅加速**：日常 `apt update` 查重操作能在数十毫秒内完成，上游判定无更新即可立即响应，消除多余的网络与磁盘负担。
2. **零带宽浪费**：在资源未变更时，传输体大小恒为 0 字节，极大保护公网出口带宽。
3. **缓存资产安全保护**：避免了上游返回 304 时因误创建空临时文件而覆盖本地已有完整缓存的严重风险。
