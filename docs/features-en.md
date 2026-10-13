# url-proxy Supported Features

## Core Proxy Capabilities

- Basic transparent proxy forwarding
- Local file caching
- Cross-platform safe cache path generation
- Standard HTTP forward proxy protocol (CONNECT tunnel)
- Conditional requests & 304 fast negotiation passthrough
- Range requests & 206 partial content / resume support
- Concurrent download coalescing (Request Coalescing)
- Idle timeout auto-cleanup

## Routing & Configuration

- Unified proxy configuration file system (`proxy.conf`)
- Host-based forwarding rules
- Path alias routing & dynamic URL rewriting
- Configurable cache-bypass rules (whitelist / blacklist arbitration)

## Response Body Processing

- Streaming dynamic response body rewriter
  - MIME type matching
  - File extension matching
  - Custom type aliases (`type` directive)

## Ecosystem Integrations

- **APT**
  - Path alias mode
  - Standard forward proxy protocol mode
- **Alpine**
  - Path alias mode
- **PyPI**
  - Simple Index dynamic passthrough
  - Wheel / sdist persistent caching
  - Absolute link auto-rewrite (HTML + PEP 691 JSON)
- **npm**
  - Package metadata dynamic passthrough
  - Tarball persistent caching
  - Tarball URL auto-rewrite in package metadata
- **Docker / OCI**
  - Registry v2 protocol relay
  - Image manifest dynamic passthrough
  - Image layer (Blobs) persistent caching
- **Rust Cargo**
  - Sparse Index dynamic passthrough
  - `config.json` download endpoint auto-rewrite
  - `.crate` package persistent caching
- **Go / Maven**
  - Path alias mode

## Architecture & Engineering Layering

- **Codebase Modular Decoupling & Domain Layering Refactoring (Feature 321)**
  - Decouple monolithic core logic into 7 cohesive `internal` domain packages
  - Object-oriented encapsulation eliminating package-level mutable global state
  - Backward-compatible lightweight root entrypoint with native Go unit testing

## Stability Hardening & Cache Governance (Stage 330)

- **Non-200 Cache Anti-Pollution & Direct Streaming Passthrough (Feature 331)**
  - Enforce temporary file creation and atomic renaming if and only if upstream returns 200 OK, preventing 4xx/5xx error pages from poisoning disk caches
  - Stream non-200 error payloads directly to clients via `io.Copy` with zero heap allocation and immediate connection release
  - Double-defense assertion in Downloader `TryRename` destroying non-200 temp files and refusing renames
- **Disk TTL Cache with mtime Freshness & Conditional Revalidation (Feature 332)**
  - 100% disk-backed storage for TTL-governed dynamic metadata, eliminating heap pressure and OOM risks
  - Zero-memory freshness evaluation via filesystem modification time (`time.Since(mtime) < TTL`)
  - Automatic `If-Modified-Since` conditional requests upon expiration; 304 response touches `mtime` via `os.Chtimes` with zero payload bandwidth
  - Automatic `STALE-FALLBACK` resilience during upstream network errors or 5xx outages
- **Disk TTL Concurrent Revalidation & Request Coalescing (Feature 333)**
  - Route concurrent expired probes through the Downloader manager, strictly consolidating requests into a single Leader probe
  - Broadcast 304 lease extensions, 200 update streams, and fault fallbacks across all waiting Followers to prevent revalidation stampedes
  - Decouple upstream requests from client contexts so Leader aborts do not disrupt waiting Followers
- **Upstream HTTP Transport Pool Tuning & Timeout Hardening (Feature 334)**
  - Expand connection pool to `MaxIdleConns: 1000` and `MaxIdleConnsPerHost: 100` (50x increase) to maximize persistent Keep-Alive reuse
  - Enforce `ResponseHeaderTimeout: 30s` to prevent indefinite goroutine hangs on stalled origins while permitting large body streaming
  - Encapsulate HTTP transport at instance level, eliminating package-level `http.DefaultClient` coupling
- **CONNECT Tunnel Idle Timeout & Half-Close Recovery (Feature 335)**
  - Wrap HTTPS CONNECT tunnels in rolling idle deadlines (default 5m) to reclaim zombie sockets and goroutines
  - Propagate TCP half-close (`CloseWrite`) gracefully to eliminate premature full-duplex connection truncation
  - Encapsulate tunnel lifecycle with deterministic socket cleanup via `sync.WaitGroup`
- **Startup Orphan Temporary File Sweep & Auto-Cleanup (Feature 336)**
  - Exact pattern matching for nanosecond timestamp extensions (`.<18~20 digits>`) with zero false positives against valid packages
  - Asynchronous background sweep on startup to purge crash-leftover temporary files older than 1 hour without blocking port binding