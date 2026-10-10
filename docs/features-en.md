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
