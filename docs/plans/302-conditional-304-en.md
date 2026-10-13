# Conditional Requests and 304 Not Modified Passthrough Design Plan

## 1. Goal
Support the HTTP Conditional Requests specification. When a client (such as `apt update`, a web browser, or `curl` with `-z` / `--etag-compare`) sends `If-Modified-Since` or `If-None-Match (ETag)` headers, the proxy faithfully relays these conditional headers to the upstream server. When the upstream responds with `304 Not Modified`, the proxy immediately short-circuits the response status code and headers without downloading any response body, achieving instant metadata revalidation while saving network bandwidth and system I/O.

## 2. Core Mechanism
The design utilizes **Conditional Header Relay** and **304 Fast Short-Circuit**:

1. **Client Conditional Header Relay**:
   - Clients running `apt update` frequently send the last modification time (`If-Modified-Since`) or entity tag (`If-None-Match`).
   - When constructing the upstream HTTP request, the proxy extracts and relays these headers, allowing the authoritative origin server to determine whether the requested resource has changed.
2. **304 Fast Short-Circuit & Zero Disk I/O**:
   - When upstream confirms the resource is unchanged and responds with `http.StatusNotModified (304)`:
     - The proxy copies upstream response headers (such as `ETag`, `Date`, `Cache-Control`) and records the 304 status code;
     - **No local temporary file is created, and no download loop is started**; the Leader closes notifications and returns immediately;
     - The client handling goroutine detects status code 304, writes the 304 status code and headers back to the client, and returns with zero body payload transferred. Existing local cache files remain completely untouched and intact.

## 3. Technical Solution

### 3.1 Critical Conditional Header Relay
When the Leader constructs the upstream request, it transparently copies standard conditional headers:
```go
// 透传客户端的条件请求头 (用于 304 缓存协商)
conditionalHeaders := []string{"If-Modified-Since", "If-None-Match"}
for _, h := range conditionalHeaders {
    if val := r.Header.Get(h); val != "" {
        req.Header.Set(h, val)
    }
}
```

### 3.2 Leader 304 Short-Circuit Exit
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

### 3.3 Client Stream Handling 304 Interception
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

## 4. Expected Benefits
1. **Dramatic APT Update Speedup**: Routine `apt update` metadata checks finish in tens of milliseconds when upstream reports no changes, removing redundant network transfers and disk writes.
2. **Zero Bandwidth Waste**: When resources are unchanged, payload transfer is strictly 0 bytes, minimizing public network bandwidth usage.
3. **Cache Asset Integrity Protection**: Eliminates the risk of overwriting existing complete cache files with empty temporary files when upstream returns 304.
