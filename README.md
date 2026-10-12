# url-proxy

[中文文档](README-zh.md)

`url-proxy` is a versatile, lightweight, and high-performance HTTP/HTTPS proxy and local caching service designed for **accelerating large file downloads, coalescing concurrent requests, dynamic streaming response rewriting, and local enterprise caching**.

In addition to serving as a general-purpose download accelerator and caching middleware, it deeply integrates into modern development and DevOps pipelines, offering out-of-the-box closed-loop caching proxy solutions for operating system packages and language dependencies (such as APT, Alpine, PyPI, npm, Docker/OCI Registry v2, Cargo, Go, and Maven).

## Features

### 1. Core Proxy & Caching Engine

- **Request Coalescing (Single-Flight)**: When multiple clients request the same resource concurrently, only one upstream request is made. All clients share the downloaded stream, drastically saving bandwidth and preventing upstream overload.
- **Dual-Mode Proxy & Tunneling**:
  - Supports standard HTTP forward proxying (via `http_proxy` environment variables) and HTTPS CONNECT bi-directional TCP streaming via connection hijacking;
  - 100% compatible with path-prefix proxying (e.g., `http://proxy:8888/https://example.com/file.zip`).
- **HTTP Range 206 Resuming**: Serves requested Byte Ranges directly from local disk in milliseconds (zero upstream traffic) when cached; transparently relays Range requests and validators on cache miss.
- **Conditional Requests & 304 Passthrough**: Relays `If-Modified-Since` and `If-None-Match` upstream, short-circuiting 304 Not Modified responses with zero body transfer and zero disk I/O overhead.
- **Streaming Response Body Rewriter**: Replaces matched content (such as absolute URLs and cross-domain CDN links) in text streams on the fly, with dynamic `{PROXY_HOST}` and `{PROXY_SCHEME}` macro interpolation and dual-layer binary safety guards.
- **Dynamic Metadata Soft TTL & SWR Caching**: Serves fresh metadata directly from memory/local storage during the soft TTL window (zero upstream requests), shielding against traffic spikes and HTTP 429 rate limits; silently renews leases via Stale-While-Revalidate (SWR) conditional 304 revalidation upon expiration.
- **Idle Timeout Watchdog**: An automated background watchdog terminates hung upstream connections, preventing socket descriptor and memory leaks.
- **Safe Cross-Platform Storage**: Automatically escapes complex URL parameters and special characters, compatible with Windows, Linux, and other file systems.

### 2. Configuration Engine & Modular Decoupling

- **Declarative Configuration (`proxy.conf`)**: Centralizes domain forwarding (`forward`), path aliases (`alias`), bypass rules (`bypass`), cache whitelists (`cache`), type groups (`type`), and dynamic rewrites (`rewrite`).
- **Scoped Route Plugin Architecture (`RoutePlugin`)**: Allows binding ecosystem-specific optimizations (such as direct Docker blob disk serving) only to designated routes, isolating custom logic and preventing namespace contamination.

### 3. Out-of-the-Box Ecosystem Profiles

Provides production-ready configuration profiles and deep protocol adapters for heavy development and CI/CD dependency workflows:
- **Operating System Repositories**: APT (Debian/Ubuntu), Alpine Linux (apk);
- **Programming Language Registries**: Python (PyPI), Node.js (npm), Rust (Cargo/Crates), Go (GoProxy), Java (Maven);
- **Containers & OCI Registries**: Docker / OCI Registry v2 (Bearer token auth challenge relay, layer blob permanent caching with direct local disk distribution).

## Quick Start

### Binary

```bash
# Show command-line help
go run .\url-proxy.go --help

# Run with proxy.conf and custom cache directory
go run .\url-proxy.go -addr :8888 -conf proxy.conf -cache ./cache
```

#### Command-line Arguments
- `-addr`: HTTP server listening address, default is `:8888`
- `-cache`: Directory to store cached files, defaults to system temporary directory
- `-conf`: Unified proxy configuration file path, default is `proxy.conf` (automatically discovered from specified path, current working directory, or executable directory)
- `-idle-time`: Idle timeout threshold. If upstream sends no data within this period, connection is terminated. Default is `10m`

### Docker

#### Build Image
```bash
docker build -t ztj1993/url-proxy:latest .
```

#### Run Container
```bash
docker run -d --name=uproxy \
  --restart=always \
  -p 8888:8888 \
  -v "$PWD/proxy.conf":/srv/proxy.conf:ro \
  -v "$PWD/cache":/srv/cache \
  ztj1993/url-proxy:latest -addr :8888 -conf /srv/proxy.conf -cache /srv/cache
```

## Unified Configuration (`proxy.conf`)

`url-proxy` uses a declarative single-line directive configuration system managed through `proxy.conf`.

### Core Directives

| Directive | Syntax Format | Description |
| :--- | :--- | :--- |
| **`forward`** | `forward <Host> <TargetBaseURL>` | Domain-based rule forwarding based on request Host header |
| **`alias`** | `alias <Prefix> <UpstreamBaseURL> [plugin:<Name>]` | Path-based short alias routing, with optional ecosystem route plugins |
| **`bypass`** | `bypass <Scope> <Type> <Pattern> [ttl:<Duration>]` | Cache bypass rule for dynamic metadata, with optional soft TTL modifier |
| **`cache`** | `cache <Scope> <Type> <Pattern>` | Whitelist rule for permanent disk caching |
| **`type`** | `type <Name> [mime:<MIME>] [suffix:<Ext>]...` | Declares content type aliases and groups (e.g., `type pypi mime:text/html suffix:.json`) |
| **`rewrite`** | `rewrite <Scope> [TypeSpec] <Pattern> <Replacement>` | Dynamically rewrites matched text response bodies (supports `{PROXY_HOST}`) |

A production-ready `proxy.conf` covering general use cases and all eight ecosystems is available in the repository root for immediate use or reference.

## Multi-Ecosystem Client Configuration Guides

Detailed setup guides for popular package managers and runtimes are available:

| Ecosystem | Proxy Mode | Client Configuration Guide |
| :--- | :--- | :--- |
| **APT (Debian / Ubuntu)** | Path Alias / Standard Forward Proxy | [APT Client Setup Guide](docs/usage/apt-en.md) |
| **Alpine Linux (apk)** | Path Alias | [Alpine Client Setup Guide](docs/usage/alpine-en.md) |
| **Python (PyPI)** | Closed-Loop Proxy (pip / uv / poetry / pdm) | [PyPI Client Setup Guide](docs/usage/pypi-en.md) |
| **Node.js (npm)** | Closed-Loop Proxy (npm / yarn / pnpm) | [npm Client Setup Guide](docs/usage/npm-en.md) |
| **Docker / OCI Registry** | Registry v2 Relay & Direct Blob Serving (Docker / Containerd / Podman) | [Docker Client Setup Guide](docs/usage/docker-en.md) |
| **Rust (Cargo / Crates)** | RFC 2785 Sparse Index & Crate Caching | [Cargo Client Setup Guide](docs/usage/cargo-en.md) |
| **Go (GoProxy)** | Path Alias (`GOPROXY`) | [GoProxy Client Setup Guide](docs/usage/goproxy-en.md) |
| **Java (Maven)** | Path Alias (`settings.xml`) | [Maven Client Setup Guide](docs/usage/maven-en.md) |

For further details, refer to the [Ecosystem Guides Index](docs/usage/README-en.md).

## Testing & Verification

The project provides high-fidelity mock clusters and an end-to-end automated test pipeline:

- **Unit & Integration Tests**: Each feature module includes a standalone mock cluster under `tests/`.
- **Docker Compose Multi-Ecosystem E2E Tests**: Orchestrates 14 client container services to validate real-world package downloads:
  ```bash
  cd tests/e2e
  bash test.sh
