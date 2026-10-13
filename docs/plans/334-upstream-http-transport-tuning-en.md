# Feature 334: Upstream HTTP Transport Pool Tuning & Timeout Hardening

## 1. Goal & Background

### 1.1 Problem Analysis
Across the Proxy Server Module's outbound HTTP workflows—including coalesced static file downloads (`handleDownload`), dynamic metadata bypass and SWR revalidation (`handleBypass`), HTTP Range slicing (`handleRange`), and HEAD probes (`handleHead`)—all outbound requests currently invoke Go's global `http.DefaultClient`. Under high-concurrency proxy workloads, this default configuration introduces two major bottlenecks:
1. **Tiny Per-Host Idle Connection Pool (`MaxIdleConnsPerHost = 2`) Causes Connection Churn**:
   - Go's `http.DefaultTransport` sets `MaxIdleConnsPerHost` to just **2**.
   - When multiple clients concurrently fetch packages or metadata from the same upstream registry (e.g., `archive.ubuntu.com`, `registry.npmjs.org`, `files.pythonhosted.org`), only 2 persistent Keep-Alive connections are retained per host after requests complete. All excess connections are immediately torn down, forcing subsequent requests to repeatedly perform TCP three-way handshakes and expensive TLS handshakes—inflating Time-To-First-Byte (TTFB) and exhausting local ephemeral ports (`TIME_WAIT`).
2. **Missing `ResponseHeaderTimeout` Causes Indefinite Goroutine Hangs**:
   - `http.DefaultTransport` leaves `ResponseHeaderTimeout` unset (`0`, waiting indefinitely).
   - If an upstream server accepts a TCP connection and receives the HTTP request headers but stalls before sending back the HTTP response status line and headers, the request goroutine blocks forever because body streaming (and the download idle watchdog) has not yet started.

### 1.2 Feature Objectives
As part of `Plan 330 (System Stability Hardening & Cache Governance Plan)`, `Feature 334` replaces `http.DefaultClient` with a dedicated, high-concurrency outbound HTTP transport inside the Proxy Server Module:
1. **High-Concurrency Persistent Connection Pool**: Increase `MaxIdleConns` to `1000` and `MaxIdleConnsPerHost` to `100` (a 50x improvement over the default), maximizing HTTP/1.1 Keep-Alive and HTTP/2 connection reuse;
2. **End-to-End Connection & Response Header Timeouts**: Explicitly configure TCP dial timeout (`10s`), TLS handshake timeout (`10s`), idle connection timeout (`90s`), and `ResponseHeaderTimeout` (`30s`) to prevent hung upstream sockets from stalling worker goroutines;
3. **Instance-Scoped Dependency Isolation**: Encapsulate the configured `*http.Client` inside the `Server` struct, eliminating reliance on package-level global `http.DefaultClient` state.

---

## 2. Architecture & Parameter Design

### 2.1 Comparison Between Default and Tuned Transport Parameters

| Transport Parameter | `http.DefaultTransport` Default | `Feature 334` Production Value | Engineering Benefit |
| :--- | :---: | :---: | :--- |
| **`MaxIdleConns`** | `100` | **`1000`** | Sustains persistent connection pools across all 8 registry ecosystems |
| **`MaxIdleConnsPerHost`** | `2` | **`100`** | Eliminates connection teardown churn and repeated TLS handshakes under concurrent loads |
| **`IdleConnTimeout`** | `90s` | **`90s`** | Reclaims idle upstream sockets cleanly |
| **`DialContext Timeout`** | `30s` | **`10s`** | Fails fast when an upstream IP is unreachable or blackholed |
| **`TLSHandshakeTimeout`** | `10s` | **`10s`** | Prevents indefinite stalls during degraded TLS handshakes |
| **`ResponseHeaderTimeout`** | **`0` (Unlimited)** | **`30s`** | **Critical Hardening**: Aborts requests if upstream fails to send response headers within 30s, while still allowing multi-gigabyte body streams to run as long as data flows |
| **`ExpectContinueTimeout`** | `1s` | **`1s`** | Preserves fast 100-Continue negotiation |

> **Design Note**: We specifically configure `Transport.ResponseHeaderTimeout` (which only bounds the wait time for response headers) rather than `http.Client.Timeout` (which caps the entire request duration including body transfer and would prematurely kill large Docker blob or OS package downloads). Mid-stream stalls during body downloads remain guarded by the Downloader Module's idle watchdog.

---

## 3. Detailed Module Design

### 3.1 Proxy Server Module
Add a dedicated `httpClient *http.Client` field to `Server` and provide `NewUpstreamTransport()`:

```go
// NewUpstreamTransport constructs an http.Transport tuned for high-concurrency registry proxying
func NewUpstreamTransport() *http.Transport {
    return &http.Transport{
        Proxy: http.ProxyFromEnvironment,
        DialContext: (&net.Dialer{
            Timeout:   10 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
        ForceAttemptHTTP2:     true,
        MaxIdleConns:          1000,
        MaxIdleConnsPerHost:   100,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
    }
}
```

Initialize `s.httpClient = &http.Client{Transport: NewUpstreamTransport()}` inside `NewServer`, and replace all `http.DefaultClient.Do(req)` calls across the download, bypass, range, and head handlers with `s.httpClient.Do(req)`.

---

## 4. Verification & Test Criteria

1. **Transport Parameter Verification**:
   - Unit-test `NewUpstreamTransport()` and `Server` initialization to verify `MaxIdleConnsPerHost`, `MaxIdleConns`, `ResponseHeaderTimeout`, and `TLSHandshakeTimeout` match specification values.
2. **Stalled Upstream Response Header Timeout Verification**:
   - Start a mock upstream server that accepts TCP connections and reads the request but never writes response headers;
   - Configure a short `ResponseHeaderTimeout` (e.g., `100ms`) on the proxy server and verify the request fails promptly upon timeout without leaking blocked goroutines.
3. **Uninterrupted Multi-Chunk Body Streaming Verification**:
   - Verify that once an upstream server sends `200 OK` headers within `ResponseHeaderTimeout`, a streaming body whose total transfer duration exceeds `ResponseHeaderTimeout` completes cleanly without interruption.
