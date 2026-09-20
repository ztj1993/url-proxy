# Range 请求与 206 断点续传设计方案

## 1. 功能目标 (Goal)
支持 HTTP 分片范围请求（Range Requests，RFC 7233 / RFC 9110）与 `206 Partial Content` 断点续传规范。当客户端（如 `apt` 恢复中断的包下载、`curl -r`、多线程下载工具或流媒体播放器）发起带有 `Range: bytes=start-end` 的请求时，代理提供：
1. **缓存命中时毫秒级本地切片响应**：从本地已缓存的完整文件中按需截取指定字节范围，返回 206 状态码与 `Content-Range`，无需向上游发起任何网络请求。
2. **缓存未命中时透明流式透传**：向上游透传 Range 请求头，将上游返回的 206 响应体纯流式传输给客户端，严禁将局部切片作为完整文件落盘，杜绝脏切片污染。
3. **分片隔离保护**：Range 请求绕过全量下载合并队列（Request Coalescing），避免不同客户端的不同切片范围相互冲突。

## 2. 核心机制 (Core Mechanism)

### 2.1 本地缓存命中与高效切片 (`http.ServeFile`)
当客户端发起 `Range` 请求且本地存在已下载完成的完整缓存时：
- 代理直接利用 Go 标准库 `http.ServeFile` 处理该文件；
- 自动解析 `Range` 范围（支持单区间 `bytes=0-49`、后缀区间 `bytes=-100`、开区间 `bytes=500-` 等）；
- 自动计算并返回 `HTTP/1.1 206 Partial Content`、`Content-Range: bytes <start>-<end>/<total>` 以及 `Content-Length`；
- 如果请求超出文件大小范围，自动返回符合 RFC 规范的 `HTTP/1.1 416 Requested Range Not Satisfiable`；
- 零公网流量消耗，零磁盘写开销，达到近乎本地硬盘寻道的高性能。

### 2.2 缓存未命中与上游 206 纯流式透传
当本地尚无完整缓存文件时（如首次下载中途断网后由客户端发起的断点续传）：
- 代理向上游源站透明转发客户端控制头（`Range`、`If-Range`、`If-Modified-Since`、`If-None-Match`、`User-Agent`）；
- 上游源站处理分片请求并返回 `206 Partial Content`；
- 代理透传所有响应头与状态码，并通过 `io.Copy` 将数据流实时泵送回客户端；
- **核心安全机制**：该 206 局部切片绝不写入 `.tmp` 临时文件或最终缓存路径，从根本上防止后续请求获取到破损或残缺的文件。

### 2.3 广播 Range 支持能力 (`Accept-Ranges: bytes`)
在所有常规的全量文件下载响应中，代理均向客户端下发 `Accept-Ranges: bytes` 响应头，明确告知客户端当前代理服务器原生支持断点续传。

## 3. 技术方案 (Technical Solution)

### 3.1 核心分流与透传代码 (`url-proxy.go`)
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

### 3.2 响应头能力通告
```go
// 发送响应头
copyHeader(w.Header(), job.Header)
w.Header().Set("Accept-Ranges", "bytes")
w.WriteHeader(job.StatusCode)
```

## 4. 预期收益 (Benefits)
1. **网络容错与无缝续传**：大体积软件包（如几百 MB 的 Linux 内核包或 Docker 镜像层）在网络抖动断开后，客户端可无缝续传剩余部分，避免从 0 开始重新下载。
2. **多线程并发切片加速**：支持各类现代多线程下载器按字节区间并发分段拉取，显著提升下载吞吐。
3. **缓存资产安全无污染**：严格隔离 206 局部切片与完整缓存文件，既保障了缓存内容的权威完整性，又兼顾了分段请求的灵活性。
