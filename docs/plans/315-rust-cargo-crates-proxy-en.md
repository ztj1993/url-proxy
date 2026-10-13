# Feature 315: Rust Cargo Sparse Index & Crates Relay Specification

## 1. Goal

Since Rust 1.68, Cargo advocates for the **Sparse Index Protocol (RFC 2785)** over monolithic Git-based index repositories.
The sparse protocol uses standard HTTP GET requests:
1. **On-Demand Textual Index Retrieval**: Fetches individual crate version listings (e.g. `GET /ra/nd/rand`, one JSON line per version);
2. **Source Archive Download**: Downloads immutable `.crate` archives from a public CDN (`https://static.crates.io/crates/<crate>/<crate>-<version>.crate`).

**Feature 315 Goal**:
Provide native sparse index relaying and persistent `.crate` artifact caching for the Rust ecosystem in `url-proxy`:
1. **Dynamic Sparse Index Passthrough**: Relay index queries with HTTP 304 conditional revalidation for instant awareness of newly published crates;
2. **100% Persistent Caching**: Safe atomic storage and Single-Flight coalescing for `.crate` archives;
3. **Simple Client Config**: Seamless configuration via `sparse+http://` in `~/.cargo/config.toml`.

---

## 2. Architecture & Sequence

```mermaid
sequenceDiagram
    participant Cargo as cargo CLI
    participant Proxy as url-proxy (8888)
    participant Index as Official index.crates.io
    participant Static as Official static.crates.io

    Note over Cargo,Proxy: 1. Sparse Index Query
    Cargo->>Proxy: GET /crates-index/ra/nd/rand
    Proxy->>Index: GET /ra/nd/rand
    Index-->>Proxy: 200 OK (Multi-line JSON index)
    Proxy-->>Cargo: 200 OK (304 Revalidation & Passthrough)

    Note over Cargo,Proxy: 2. Crate Archive Download
    Cargo->>Proxy: GET /crates-static/crates/rand/rand-0.8.5.crate
    alt Crate Cached
        Note over Proxy: SafeUrlPath fast response
        Proxy-->>Cargo: 200 OK
    else Cold Download
        Proxy->>Static: GET /crates/rand/rand-0.8.5.crate
        Note over Proxy: Coalesced Fetch + Atomic Disk Write
        Static-->>Proxy: 200 OK
        Proxy-->>Cargo: 200 OK
    end
```

---

## 3. Configuration Syntax (`proxy.conf`)

```ini
# ==========================================
# Rust Cargo Sparse Index & Crates Configuration
# ==========================================

# 1. Index and static distribution aliases
alias crates-index https://index.crates.io
alias crates-static https://static.crates.io

# 2. Dynamic config.json rewriting (redirects dl download endpoint to local proxy)
rewrite /crates-index/config.json type:json https://static.crates.io/crates http://{PROXY_HOST}/crates-static/crates

# 3. Sparse index bypass (dynamic 304 revalidation)
bypass /crates-index/ *

# 4. Source archives cached permanently
cache /crates-static/ suffix .crate
cache * suffix .crate
```

---

## 4. Client Configuration

Configure `~/.cargo/config.toml` on client machines:

```toml
[registries.crates-io]
protocol = "sparse"

[source.crates-io]
replace-with = "url-proxy-sparse"

[source.url-proxy-sparse]
registry = "sparse+http://<proxy-ip>:8888/crates-index/"
```

---

## 5. Verification

1. **On-Demand Sparse Retrieval**: `curl -s http://127.0.0.1:8888/crates-index/config.json` succeeds and conforms to TTL rules;
2. **Dependency Compilation**: `cargo build` retrieves dependencies via proxy;
3. **100% Cache Hit on Re-run**: Purge client `~/.cargo/registry/cache` and rebuild to verify zero upstream WAN bandwidth with all artifacts returning `[CACHE-HIT]`.
