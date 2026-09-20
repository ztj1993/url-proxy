# 基础透明代理转发设计方案

## 1. 功能目标 (Goal)
提供一个轻量、高效的透明 HTTP/HTTPS/FTP 下载代理中继服务。客户端只需在代理服务器 URL 后追加目标资源完整 URL，即可完成资源的透明下载与协议透传。

## 2. 核心机制 (Core Mechanism)
采用 **透明 URL 路由解析 (Transparent URL Routing)** 与 **流式响应透传 (Streaming Relay)**：

- **URL 协议与合法性校验**：自动解析请求路径中的目标 URI，校验前缀协议（支持 `http`、`https`、`ftp`）。
- **流式中继传输**：通过 Go 标准库 `net/http` 发起底层远端请求，通过 `io.Copy` 流式将远端数据管道传输给客户端，无需在内存中预载完整文件。
- **元数据原样透传**：完整复制远端服务器返回的 HTTP Headers（如 `Content-Type`、`Content-Length`、`Content-Disposition` 等）及 HTTP 状态码。

## 3. 技术方案 (Technical Solution)

### 3.1 请求路由解析与校验
```go
uri := r.URL.Path[1:]
uris, err := url.Parse(uri)
if err != nil || !in(uris.Scheme, []string{"ftp", "http", "https"}) {
    w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
    return
}
```

### 3.2 远端请求与流式透传
1. 构造发往目标服务器的 HTTP GET 请求。
2. 接收到远端响应后，通过 `copyHeader(w.Header(), resp.Header)` 将远端响应头完整写回客户端。
3. 返回对应的远端 HTTP 状态码 `w.WriteHeader(resp.StatusCode)`。
4. 保持连接以流式方式传输数据包，及时关闭 `resp.Body` 防止连接和句柄泄漏。

## 4. 预期收益 (Benefits)
1. **即开即用**：无需客户端配置复杂的系统级或应用级代理协议，使用纯 HTTP 路径拼接即可下载。
2. **低内存占用**：通过流式管道传输，即便中继超大文件也不会导致代理内存膨胀。
3. **协议透明**：客户端感受如同直接向上游服务器请求，文件名及文件类型完全保留。
