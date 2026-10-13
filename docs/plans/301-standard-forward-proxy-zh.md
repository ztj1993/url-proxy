# 标准 HTTP 正向代理协议支持设计方案

## 1. 功能目标 (Goal)
将 `url-proxy` 升级为兼容标准 HTTP 正向代理协议（Standard Forward HTTP Proxy Protocol）的服务端。允许标准 HTTP 客户端（如 `curl -x`、APT 的 `Acquire::http::Proxy`、Docker Daemon 代理以及操作系统全局环境变量 `http_proxy`）无需修改任何 URL 目标地址，开箱即用直接将流量导向 `url-proxy`，并享受底层的请求合并与本地缓存加速。

## 2. 核心机制 (Core Mechanism)
采用 **请求行绝对 URI 识别 (Absolute URI Parsing)** 与 **CONNECT 隧道透传 (CONNECT Tunneling)**：

1. **绝对 URI 自动解析 (Absolute URI Parsing)**：
   - 当客户端配置为 HTTP 正向代理时，发出的 HTTP 请求行直接携带完整的绝对目标地址（如 `GET http://archive.ubuntu.com/ubuntu/... HTTP/1.1`）。
   - 代理自动探测 `r.URL.IsAbs()` 或 `r.URL.Host != ""`，若匹配则提取完整目标 URL 进入核心中继与缓存引擎；若不匹配则平滑回退至原有的路径前缀代理模式（`GET /http://...`），保持 100% 向下兼容。
2. **HTTPS CONNECT 隧道中继 (CONNECT Tunneling)**：
   - 当客户端通过标准正向代理访问 `https://` 目标时，会向代理发出 `CONNECT host:443 HTTP/1.1` 握手请求。
   - 代理通过 `http.Hijacker` 接管底层 TCP 连接，向上游目标建立 TCP 连接并返回 `200 Connection Established`，随后在双向之间进行透明的字节管道中继（双向 `io.Copy`），保障 HTTPS 流量的顺畅直通。

## 3. 技术方案 (Technical Solution)

### 3.1 目标 URI 兼容性提取
```go
// 提取目标 URI：兼容标准 HTTP 正向代理模式与路径前缀代理模式
var uri string
if r.URL.IsAbs() || (r.URL.Host != "" && r.URL.Scheme != "") {
    // 标准正向代理模式: GET http://example.com/path HTTP/1.1
    uri = r.URL.String()
} else if r.URL.Host != "" {
    scheme := "http"
    if r.TLS != nil {
        scheme = "https"
    }
    uri = fmt.Sprintf("%s://%s%s", scheme, r.URL.Host, r.URL.RequestURI())
} else if len(r.URL.Path) > 1 {
    // 路径前缀代理模式: GET /http://example.com/path HTTP/1.1
    uri = r.URL.Path[1:]
    if r.URL.RawQuery != "" {
        uri += "?" + r.URL.RawQuery
    }
}
```

### 3.2 HTTPS CONNECT 隧道实现
```go
if r.Method == http.MethodConnect {
    destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
    if err != nil {
        http.Error(w, err.Error(), http.StatusServiceUnavailable)
        return
    }
    hijacker, ok := w.(http.Hijacker)
    if !ok {
        http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
        _ = destConn.Close()
        return
    }
    clientConn, _, err := hijacker.Hijack()
    if err != nil {
        http.Error(w, err.Error(), http.StatusServiceUnavailable)
        _ = destConn.Close()
        return
    }
    _, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
    go func() {
        defer destConn.Close()
        defer clientConn.Close()
        _, _ = io.Copy(destConn, clientConn)
    }()
    go func() {
        defer destConn.Close()
        defer clientConn.Close()
        _, _ = io.Copy(clientConn, destConn)
    }()
    return
}
```

## 4. 预期收益 (Benefits)
1. **客户端零侵入**：APT 仅需配置一行 `Acquire::http::Proxy "http://proxy:8888";`，完全无需改写 `/etc/apt/sources.list` 中的任何源地址。
2. **工具链通用**：支持标准环境变量 `export http_proxy=http://proxy:8888`，兼容 `curl -x`、`wget -e http_proxy=...`、Git、Docker 等绝大多数开发运维工具。
3. **完全平滑过渡**：现有基于 URL 路径拼接（`/http://...`）的所有调用方式与自动化脚本不受任何影响。
