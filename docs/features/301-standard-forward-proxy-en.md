# Standard Forward HTTP Proxy Protocol Support Design Plan

## 1. Goal
Upgrade `url-proxy` into a fully compliant Standard Forward HTTP Proxy server. Allow standard HTTP clients (e.g., `curl -x`, APT `Acquire::http::Proxy`, Docker daemon HTTP proxy, and system environment variable `http_proxy`) to direct traffic to `url-proxy` out-of-the-box without modifying target URLs, while benefiting from backend request coalescing and local disk caching.

## 2. Core Mechanism
Adopts **Absolute URI Request Parsing** and **CONNECT Tunneling**:

1. **Absolute URI Parsing**:
   - When a client is configured to use a forward HTTP proxy, its HTTP request line contains the absolute target URL (e.g., `GET http://archive.ubuntu.com/ubuntu/... HTTP/1.1`).
   - The proxy detects `r.URL.IsAbs()` or `r.URL.Host != ""`, extracts the target URL into the core relay/caching engine, and transparently falls back to path-prefix proxy mode (`GET /http://...`) if not present, maintaining 100% backward compatibility.
2. **HTTPS CONNECT Tunneling**:
   - When a client accesses `https://` resources through the forward proxy, it initiates a handshake via `CONNECT host:443 HTTP/1.1`.
   - The proxy hijacks the underlying TCP connection via `http.Hijacker`, establishes an outbound TCP connection to the upstream target, writes `200 Connection Established`, and relays bidirectional raw bytes via `io.Copy`.

## 3. Technical Solution

### 3.1 Adaptive Target URI Extraction
```go
// Extract target URI: compatible with both standard forward proxy and path-prefix mode
var uri string
if r.URL.IsAbs() || (r.URL.Host != "" && r.URL.Scheme != "") {
    // Standard forward proxy mode: GET http://example.com/path HTTP/1.1
    uri = r.URL.String()
} else if r.URL.Host != "" {
    scheme := "http"
    if r.TLS != nil {
        scheme = "https"
    }
    uri = fmt.Sprintf("%s://%s%s", scheme, r.URL.Host, r.URL.RequestURI())
} else if len(r.URL.Path) > 1 {
    // Path prefix mode: GET /http://example.com/path HTTP/1.1
    uri = r.URL.Path[1:]
    if r.URL.RawQuery != "" {
        uri += "?" + r.URL.RawQuery
    }
}
```

### 3.2 HTTPS CONNECT Tunnel Implementation
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

## 4. Benefits
1. **Zero Client URL Invasiveness**: APT requires only a single line in `/etc/apt/apt.conf.d/01proxy` (`Acquire::http::Proxy "http://proxy:8888";`) with no changes to `/etc/apt/sources.list`.
2. **Universal Tooling Compatibility**: Seamlessly works with standard `export http_proxy=http://proxy:8888`, supporting `curl -x`, `wget`, Git, Docker, and standard developer tools.
3. **Smooth Backward Compatibility**: Existing path-prefix invocation (`/http://...`) continues to work without modification.
