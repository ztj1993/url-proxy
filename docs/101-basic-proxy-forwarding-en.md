# Basic Transparent Proxy Forwarding Design Plan

## 1. Goal
Provide a lightweight, high-performance transparent HTTP/HTTPS/FTP download proxy service. Clients can download remote files by simply appending the target resource URL to the proxy server URL.

## 2. Core Mechanism
Adopts **Transparent URL Routing** and **Streaming Relay**:

- **URL Scheme Validation**: Automatically parses the target URI from the request path and validates supported protocols (`http`, `https`, `ftp`).
- **Streaming Transmission**: Initiates the remote download via Go standard library `net/http` and pipes data to the client using stream copying, avoiding loading entire files into memory.
- **Metadata Transparency**: Forwards remote HTTP headers (`Content-Type`, `Content-Length`, `Content-Disposition`, etc.) and status codes directly back to the client.

## 3. Technical Solution

### 3.1 Request Routing and Validation
```go
uri := r.URL.Path[1:]
uris, err := url.Parse(uri)
if err != nil || !in(uris.Scheme, []string{"ftp", "http", "https"}) {
    w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
    return
}
```

### 3.2 Remote Request and Streaming
1. Construct the HTTP GET request directed at the target resource.
2. Upon receiving the response, copy remote headers back to the client using `copyHeader(w.Header(), resp.Header)`.
3. Return the upstream HTTP status code via `w.WriteHeader(resp.StatusCode)`.
4. Stream body bytes through the pipe, ensuring `resp.Body` is cleanly closed on completion.

## 4. Benefits
1. **Zero-Configuration Client**: No complex proxy client configuration needed; plain HTTP path appending is sufficient.
2. **Low Memory Footprint**: Streaming delivery prevents memory spikes even when handling multi-gigabyte files.
3. **Transparent Delivery**: Downstream clients receive identical file names, MIME types, and headers as if requesting upstream directly.
