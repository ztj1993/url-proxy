# APT Package Caching & Proxy Relay Plan (Plan 300)

## 1. Background & Problem Analysis

### 1.1 Core Challenges
In multi-server node clusters, container build environments (e.g. Docker build), VM template initializations, and continuous integration (CI/CD runner) pipelines, systems frequently execute `apt-get update` and `apt-get install` commands. This creates several major bottlenecks:
- **Redundant Outbound Bandwidth Consumption**: Across multiple hosts or containers pulling identical system dependencies (such as `build-essential`, `linux-image-*`, `python3`), repetitive requests to public upstream mirrors waste significant bandwidth.
- **Unpredictable Build and Provisioning Latency**: Public mirrors often suffer from upstream throttling, regional latency spikes, and network jitter, slowing down deployment pipelines.
- **High Maintenance Overhead of Traditional Caches**: Specialized solutions like `apt-cacher-ng` or generic forward proxies like Squid require complex maintenance. Furthermore, when dealing with native HTTPS upstream mirrors, they either require intrusive root certificate distribution or fail to cache encrypted content.
- **Configuration Fragmentation & Hardcoding Risks**: Maintaining separate configuration files for host forwarding, aliases, and uncacheable rules leads to operational friction and CLI flag bloat. Hardcoding package extensions in code poses severe false-positive risks where packages are incorrectly marked uncacheable.

### 1.2 Objectives
Extend `url-proxy` by reusing its foundational strengths—Request Coalescing, Safe Path generation, atomic rename with reference counting, and idle timeout cleanup—to build a high-performance, lightweight, non-invasive, and completely configuration-driven caching and proxy relay service:
1. **Full Access Protocol Compatibility**: Simultaneously supports standard HTTP forward proxying (`Acquire::http::Proxy`), path short alias routing (`deb http://proxy/ubuntu ...`), and raw full-path proxying.
2. **Dynamic/Static Segregation & Anti-False-Positive Engine**: Pure streaming relay (Zero Disk I/O) for dynamic metadata eliminates `Hash Sum mismatch` errors, while absolute whitelist priority guarantees 100% reliable package disk caching.
3. **Advanced Protocol Relay**: Comprehensive support for conditional headers (`If-None-Match` / 304 fast passthrough) and Range requests (`206 Partial Content` resuming).
4. **Single Source of Truth Configuration**: A unified configuration file `proxy.conf` governs alias routing, gateway forwarding, bypass rules, and whitelist protection without hardcoded extensions.

---

## 2. Design Thinking & Decision Evolution

### 2.1 Topic 1: Client Access Architecture & Resolving the HTTPS Caching Dilemma
- **The Core Trade-off**: Should clients connect via standard HTTP forward proxying (`Acquire::http::Proxy`) or via URL path-based proxying (`http://proxy/http://...`)?
- **Analysis**:
  1. Under **standard forward proxying**, when a client connects to an `https://` upstream mirror (e.g. `https://deb.debian.org`), the client initiates an HTTP `CONNECT host:443` tunnel. Once established, end-to-end TLS encryption prevents the proxy from inspecting payloads or caching `.deb` packages (unless an enterprise self-signed CA root certificate is distributed to every client, which is operationally prohibitive).
  2. Under **HTTP plaintext client access with native HTTPS upstream relay**:
     ```text
     [Client] --- (HTTP Plaintext) ---> [url-proxy] --- (Native HTTPS) ---> [Official Mirror]
     ```
     - Clients connect to the proxy via standard HTTP, granting the proxy full visibility to parse headers and stream payloads;
     - The proxy acts as a legitimate HTTPS client toward the upstream mirror, **natively bypassing TLS interception hurdles and caching `.deb` files without any client-side certificates**.
- **Security Validation**:
  Debian and Ubuntu APT ecosystems rely natively on GPG signature verification for cryptographic integrity. Even over plaintext HTTP to a local proxy, the APT client strictly verifies every package's hash and signature, guaranteeing zero security degradation.
- **Decision**: Dual-mode proxy support—standard forward proxying for native HTTP sources and path short alias mode for clean, certificate-free local caching.

### 2.2 Topic 2: Dynamic/Static Segregation & Anti-False-Positive Whitelisting
- **Technical Risks**:
  APT index files (`InRelease`, `Release`, `Packages.xz`) update frequently. If cached on disk, stale metadata causes client `Hash Sum mismatch` errors. Conversely, naive keyword matching (e.g. matching `packages` or `release`) causes false positives, erroneously bypassing caching for packages like `packages-base.deb` or `release-notes.tar.gz`.
- **Architectural Safeguards**:
  1. **Absolute Whitelist Priority (Whitelist Enforcement)**:
     Whitelist exceptions are declared in configuration via `cache` directives (e.g. `cache * suffix .deb`). Any request matching a `cache` rule is immediately forced to disk cache with highest priority, terminating bypass checks.
  2. **Context Scoping**:
     APT dynamic metadata resides strictly within the `/dists/` directory hierarchy, whereas packages reside within `/pool/`. Bypass rules are scoped strictly to `/dists/` to prevent rule leakage into `/pool/`.
  3. **Configuration-Driven, Zero Code Hardcoding**:
     Hardcoded extension arrays are completely eliminated from Go source code. All policies are driven by `proxy.conf`.

### 2.3 Topic 3: Protocol Integrity—Conditional 304 Passthrough & Range Resuming
- **Optimizations**:
  1. **Conditional Requests & 304 Fast Passthrough**:
     - Routine `apt update` sends `If-Modified-Since` and `If-None-Match (ETag)`.
     - The proxy relays these headers upstream. When upstream returns `304 Not Modified`, the proxy short-circuits 304 directly back to the client, expediting update routines with zero payload transfer.
  2. **Range Requests & 206 Partial Content Resuming**:
     - If network interruptions occur, clients send `Range: bytes=start-`.
     - If a complete cache exists locally, `http.ServeFile` serves the slice directly from disk (zero upstream requests);
     - If uncompleted, the proxy streams `206 Partial Content` directly from upstream without saving partial slices to disk.

### 2.4 Topic 4: Unified Configuration System & Directive Syntax
- **Consolidation**:
  Replacing fragmented `.conf` files (`forwarding.conf`, `nocache.conf`), a single unified configuration file `proxy.conf` is established:
  - Clean **single-line directive syntax** with explicit action keywords (`alias`, `forward`, `bypass`, `cache`);
  - Unified `-conf <path>` CLI flag with multi-tier automatic discovery across current working directory and binary directory.

### 2.5 Topic 5: Path Representation & Single Cache Source of Truth
- **Architecture**:
  - **Short Alias Mode (Recommended)**:
    Clean `sources.list` configuration: `deb http://proxy:8888/ubuntu noble main`. The proxy rewrites `/ubuntu` to `http://archive.ubuntu.com/ubuntu`.
  - **Single Source of Truth Disk Caching**:
    Because dynamic rewriting occurs at request entry, disk cache keys (`SafeUrlPath`) are derived from the canonical upstream URL. Both alias and raw URL modes **hit the exact same local cache file**, preventing duplicate downloads and disk waste.

### 2.6 Topic 6: Minimalist Storage & Lifecycle Management
- **KISS Philosophy**:
  No complex in-memory LRU engine. Cache files are persisted under structured, human-readable directory trees. Disk capacity management is delegated to standard OS utilities (e.g. `find ... -atime +30 -delete`), ensuring maximum simplicity and stability.

---

## 3. Technical Implementation Details

### 3.1 Unified Configuration Syntax (`proxy.conf`)

Single-line directives separated by whitespace or tabs. Empty lines and lines starting with `#` are ignored:

| Directive | Syntax | Description | Feature |
| :--- | :--- | :--- | :--- |
| **`alias`** | `alias <short_alias> <upstream_base_url>` | Declares first-segment path alias and dynamic URL rewriting | **306** |
| **`forward`** | `forward <source_host> <gateway_base_url>` | Declares upstream host gateway or proxy redirection | **103** |
| **`bypass`**<br>*(or `nocache`)* | `bypass <Scope> <Type> <Pattern>` | Declares dynamic uncacheable resources; pure streaming relay | **305** |
| **`cache`** | `cache <Scope> <Type> <Pattern>` | Declares mandatory disk cache / whitelist exception (highest priority) | **305** |

#### Scope Parameter Definitions
- `*`: Global scope; matches all domains and paths;
- `/prefix/`: Path prefix scope; active when request path contains the prefix (e.g. `/dists/`);
- `domain.com`: Domain scope; active when target host matches;
- `domain.com/prefix/`: Composite domain + path scope.

#### Match Type Parameter Definitions
- `exact`: Exact filename match (case-insensitive, e.g. `exact InRelease`);
- `prefix`: Filename prefix match (case-insensitive, e.g. `prefix Packages`);
- `suffix`: Filename suffix match (case-insensitive, e.g. `suffix .deb`);
- `regex`: Regular expression match against full path or filename.

---

### 3.2 Dynamic Cache Bypass & Anti-False-Positive Engine (`url-proxy.go`)

```
                      Incoming Client Request
                                 │
                                 ▼
                   Iterate Rules in proxy.conf
                                 │
                  Matches any Action == "cache" rule?
                                / \
                         Yes   /   \  No
                              /     \
                             ▼       ▼
                     [Force Cache]   Matches any Action == "bypass" rule?
                     (Exit Bypass)         / \
                                    Yes   /   \  No
                                         /     \
                                        ▼       ▼
                            [Streaming Relay]  [Default Cache]
                            (Zero Disk I/O)
```

1. **Absolute Whitelist Priority**: Matches on `Action == "cache"` immediately enforce disk caching;
2. **Zero Disk I/O Streaming**: Requests matching `bypass` relay conditional headers upstream. Upstream 304s are directly short-circuited; 200 responses are streamed via `io.Copy(w, resp.Body)` with **zero `.tmp` creation and zero disk writes**;
3. **Default Behavior**: Unmatched requests proceed with standard request coalescing and disk caching.

---

### 3.3 Path Short Alias Routing & Dynamic Rewriting Engine (`url-proxy.go`)

1. If `uris.Scheme` is empty or not in `prefixes` (`{"ftp", "http", "https"}`), extract the first path segment `aliasKey`;
2. If `targetBase, ok := aliases[aliasKey]` matches:
   - Normalize and join `targetBase` with `subPath` using a single `/`;
   - Preserve client query strings (`r.URL.RawQuery`);
   - Dynamically rewrite to an absolute URI and re-parse as `url.URL`;
3. If no alias matches, return `404 Not Found`;
4. The rewritten URI seamlessly enters all downstream pipelines (bypass evaluation, 304 revalidation, Range resuming, disk caching).

---

## 4. Modular Implementation Roadmap & Deliverables

All 7 modular features have been implemented, verified via automated test suites, and independently committed:

| Feature | Feature Name | Core Capability | Deliverables & Verification | Associated Commit |
| :---: | :--- | :--- | :--- | :--- |
| **301** | **Standard Forward HTTP Proxy Protocol** | Request-line parsing and HTTPS CONNECT tunneling; supports `Acquire::http::Proxy`. | • `docs/features/301-standard-forward-proxy-{zh,en}.md`<br>• `tests/301_forward_proxy/` smoke tests | `ce31ce5` `feat(proxy): support standard HTTP forward proxy protocol (301)` |
| **302** | **Conditional Requests & 304 Passthrough** | Relays `If-Modified-Since` and `If-None-Match`, short-circuiting 304s with zero payload transfer. | • `docs/features/302-conditional-304-{zh,en}.md`<br>• `tests/302_conditional_304/` 304 passthrough tests | `a9ad53e` `feat(proxy): support conditional requests and 304 fast passthrough (302)` |
| **303** | **Range Requests & 206 Resuming** | Relays `Range` headers and `206 Partial Content`; serves cached slices directly from disk. | • `docs/features/303-range-requests-{zh,en}.md`<br>• `tests/303_range_requests/` 206 slice tests | `31613ba` `feat(proxy): support Range requests and 206 Partial Content resuming (303)` |
| **304** | **Unified Proxy Configuration System** | Single source of truth `proxy.conf` with clean single-line directives and `-conf` discovery. | • `docs/features/304-unified-configuration-{zh,en}.md`<br>• `tests/304_unified_config/` CLI & probe tests | `9bd2a94` `feat: implement unified proxy configuration system (Feature 304)` |
| **305** | **Configurable Cache Bypass & Anti-False-Positive** | Scoped `/dists/` streaming bypass (Zero Disk I/O) and absolute whitelist priority protection. | • `docs/features/305-selective-cache-bypass-{zh,en}.md`<br>• `tests/305_selective_cache/` bypass & whitelist tests | `7449ed3` `feat(cache): implement configurable cache bypass rules and anti-false-positive engine (305)` |
| **306** | **Path Short Alias Routing & Dynamic Rewriting** | Resolves first-segment aliases, dynamically rewrites URLs, and unifies disk cache keys. | • `docs/features/306-path-alias-routing-{zh,en}.md`<br>• `tests/306_path_alias/` alias rewrite tests | `bcb63e6` `feat(routing): support path-based short alias routing and dynamic URL rewriting (306)` |
| **307** | **APT Proxy End-to-End Integration Verification** | High-fidelity mock APT repository, 7 lifecycle integration scenarios, and live traffic auditing. | • `docs/features/307-apt-integration-{zh,en}.md`<br>• `tests/307_apt_integration/` end-to-end integration tests | `314ed48` `test(apt): add end-to-end integration test suite and profile templates for APT (307)` |

---

## 5. Production Configuration Template (`proxy.conf`)

```text
# ==============================================================================
# url-proxy Production APT Unified Configuration (proxy.conf)
# ==============================================================================

# 1. Path Short Alias Routing
alias ubuntu http://archive.ubuntu.com/ubuntu
alias ubuntu-security http://security.ubuntu.com/ubuntu
alias debian https://deb.debian.org/debian
alias debian-security https://deb.debian.org/debian-security

# 2. Host Forwarding Rules
forward registry-1.docker.io http://mirror.local/docker
forward github.com https://ghproxy.example.com

# 3. Whitelist Exceptions (forces disk caching; absolute highest priority)
cache * suffix .deb
cache * suffix .udeb
cache * suffix .ddeb
cache * suffix .tar.gz
cache * suffix .tar.xz
cache * suffix .tar.zst
cache * suffix .tar.bz2
cache * suffix .dsc

# 4. APT Dynamic Metadata Cache Bypass (pure streaming relay, zero disk writes)
bypass /dists/ exact InRelease
bypass /dists/ exact Release
bypass /dists/ exact Release.gpg
bypass /dists/ prefix Packages
bypass /dists/ prefix Sources
bypass /dists/ prefix Translation-
bypass /dists/ regex ^.*/by-hash/.*$
```

---

## 6. Client Configuration Guide

### 6.1 Mode A: Short Alias Mode (Recommended)

1. **Ubuntu Setup (`/etc/apt/sources.list`)**:
   ```text
   deb http://<proxy-ip>:8888/ubuntu jammy main restricted universe multiverse
   deb http://<proxy-ip>:8888/ubuntu jammy-updates main restricted universe multiverse
   deb http://<proxy-ip>:8888/ubuntu-security jammy-security main restricted universe multiverse
   ```

2. **Debian Setup (`/etc/apt/sources.list`)**:
   ```text
   deb http://<proxy-ip>:8888/debian bookworm main non-free-firmware
   deb http://<proxy-ip>:8888/debian bookworm-updates main non-free-firmware
   deb http://<proxy-ip>:8888/debian-security bookworm-security main non-free-firmware
   ```

### 6.2 Mode B: Standard Forward Proxy Mode (`Acquire::http::Proxy`)

Create `/etc/apt/apt.conf.d/01proxy`:
```text
Acquire::http::Proxy "http://<proxy-ip>:8888";
```
- Preserves native `/etc/apt/sources.list` configuration without edits.

### 6.3 Mode C: Raw Full-Path Mode (Zero Proxy Setup)

Append full repository URLs directly to proxy address:
```text
deb http://<proxy-ip>:8888/http://archive.ubuntu.com/ubuntu jammy main
deb http://<proxy-ip>:8888/https://deb.debian.org/debian bookworm main
```

---

## 7. Summary & Future Roadmap

With Features 301 through 307 complete, `url-proxy` provides an enterprise-ready, robust, high-performance APT caching solution.

This foundational architecture naturally extends to other ecosystems:
- **Docker Container Image Acceleration**:
  - `manifests`: volatile metadata using `bypass` for streaming validation;
  - `blobs/sha256:...`: immutable content-addressed layers using `cache` with request coalescing.
- **Language Package Registries (PyPI / NPM / Maven)**:
  - Index pages via `bypass`;
  - Package artifacts (`.whl`, `.tgz`, `.jar`) via `cache` whitelist rules.
