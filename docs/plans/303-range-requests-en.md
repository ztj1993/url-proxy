# Range Requests and 206 Partial Content Resuming Support Design Plan

## 1. Goal
Support the HTTP Range Requests specification (RFC 7233 / RFC 9110) and `206 Partial Content` resuming mechanism. When clients (such as `apt` resuming interrupted package downloads, `curl -r`, multi-threaded download accelerators, or media streamers) issue requests with `Range: bytes=start-end`:
1. **Millisecond Local Slicing on Cache Hit**: Directly extract the requested byte range from the locally cached complete file, returning status 206 with `Content-Range` without contacting upstream origin servers.
2. **Transparent Streaming Relay on Cache Miss**: Forward the `Range` request headers upstream, stream the upstream 206 response body directly to the client, and strictly prevent partial slices from polluting or corrupting full cache files on disk.
3. **Slice Isolation Protection**: Range requests bypass full-file request coalescing, preventing differing client slice offsets from conflicting with one another.

## 2. Core Mechanism

### 2.1 Local Cache Hit & High-Performance Slicing (`http.ServeFile`)
When a client sends a `Range` request and a complete cache file already exists locally:
- The proxy delegates servicing directly to Go's standard library `http.ServeFile`;
- Automatically parses range expressions (supporting closed intervals `bytes=0-49`, suffix ranges `bytes=-100`, open-ended intervals `bytes=500-`, etc.);
- Automatically computes and returns `HTTP/1.1 206 Partial Content`, `Content-Range: bytes <start>-<end>/<total>`, and `Content-Length`;
- Automatically returns RFC-compliant `HTTP/1.1 416 Requested Range Not Satisfiable` if the range is out of bounds;
- Zero public bandwidth consumption and zero disk write overhead, delivering near raw-disk seek performance.

### 2.2 Cache Miss & Upstream 206 Pure Streaming Relay
When no complete local cache file exists (e.g. initial download interrupted by network drop and subsequently resumed by the client):
- The proxy transparently forwards client control headers (`Range`, `If-Range`, `If-Modified-Since`, `If-None-Match`, `User-Agent`) upstream;
- Upstream processes the slice request and responds with `206 Partial Content`;
- The proxy forwards all headers and status codes, streaming payload bytes back to the client in real-time via `io.Copy`;
- **Core Security Mechanism**: The 206 partial slice is never written to temporary `.tmp` or final cache files, fundamentally preventing corrupt or truncated files from poisoning the cache.

### 2.3 Advertising Resuming Capability (`Accept-Ranges: bytes`)
On all standard full-file download responses, the proxy attaches the `Accept-Ranges: bytes` response header, explicitly announcing to clients that byte-level resuming is natively supported.

## 3. Technical Solution

### 3.1 Core Dispatch and Relay Code (`url-proxy.go`)
```go
// 处理 HTTP Range 分片请求与 206 断点续传
if r.Header.Get("Range") != "" {
    cachePath := filepath.Join(*cache, getSafeUrlPath(uri))
    if stat, err := os.Stat(cachePath); err == nil && !stat.IsDir() {
        // 本地存在已下载完成的完整缓存，直接通过 http.ServeFile 高效处理 Range 分片
        log.Printf("[%d] range hit local cache: %s (%s)", reqID, uri, r.Header.Get("Range"))
        http.ServeFile(w, r, cachePath)
        return
    }

    // 本地无完整缓存，直接向上游发起透传 Range 请求，纯流式返回，不落盘（避免局部脏切片污染完整文件缓存）
    log.Printf("[%d] range relay to upstream: %s (%s)", reqID, uri, r.Header.Get("Range"))
    targetUri := uri
    if _, ok := forwards[uris.Host]; ok {
        targetUri = forwards[uris.Host] + "/" + uri
    }

    req, err := http.NewRequestWithContext(r.Context(), "GET", targetUri, nil)
    if err != nil {
        log.Printf("[%d] req create err: %s", reqID, err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }

    // 透传 Range、If-Range、条件协商与客户端特征头
    rangeRelayHeaders := []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match", "User-Agent"}
    for _, h := range rangeRelayHeaders {
        if val := r.Header.Get(h); val != "" {
            req.Header.Set(h, val)
        }
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        log.Printf("[%d] upstream range get err: %s", reqID, err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    defer func() {
        _ = resp.Body.Close()
    }()

    copyHeader(w.Header(), resp.Header)
    w.WriteHeader(resp.StatusCode)
    _, _ = io.Copy(w, resp.Body)
    return
}
```

### 3.2 Response Header Capability Advertising
```go
// 发送响应头
copyHeader(w.Header(), job.Header)
w.Header().Set("Accept-Ranges", "bytes")
w.WriteHeader(job.StatusCode)
```

## 4. Expected Benefits
1. **Network Fault Tolerance & Seamless Resuming**: Large software packages (such as multi-hundred-megabyte Linux kernels or container layers) can resume from where they left off upon transient disconnection, eliminating costly restart from byte zero.
2. **Multi-Threaded Slicing Acceleration**: Supports modern download managers that divide files into byte segments for parallel downloading, drastically improving throughput.
3. **Cache Integrity Protection**: Strict segregation between 206 partial slices and cached files ensures cache validity while offering full flexibility for segmented requests.
