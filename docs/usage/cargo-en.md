# Rust Cargo Sparse Index & Crates Official Proxy Guide (Cargo Caching Guide)

This document describes how to configure `url-proxy` as a high-performance local/LAN caching proxy for the official Rust Crates.io Sparse Index Protocol (RFC 2785) and crate source archives (`https://static.crates.io`).

---

## 1. Background & Principle

Starting with Rust 1.68, Cargo recommends using the **Sparse Index Protocol (RFC 2785)** instead of the monolithic Git index repository:
1. **Lightweight On-Demand Index Queries**: Cargo queries metadata for individual dependencies via simple HTTP GET requests (NDJSON format, e.g. `GET /crates-index/ra/nd/rand`). Files are only a few kilobytes and support HTTP 304 conditional revalidation;
2. **Download Endpoint Declaration (`config.json`)**:
   - The index root contains `config.json`, which declares the crate download template: `"dl": "https://static.crates.io/crates"`;
   - If transmitted untouched, Cargo clients will connect directly to `static.crates.io`, bypassing the local proxy and failing to cache `.crate` files;
3. **Closed-Loop Proxying (Feature 311 & 315)**:
   - `url-proxy` uses its dynamic response body rewriting engine to automatically replace `"dl"` in `config.json` with the local proxy endpoint: `"dl": "http://<proxy-host>:<port>/crates-static/crates"`;
   - All subsequent `.crate` package downloads (immutable tar.gz archives) flow through the proxy's `/crates-static/` alias;
   - The proxy performs **Single-Flight request coalescing** and **permanent local disk caching**, delivering **0 upstream bandwidth** and **line-speed LAN downloads** for subsequent builds across developers and CI runners.

```text
[cargo CLI] --- (GET /crates-index/config.json) ---> [url-proxy] ---> [index.crates.io]
     ▲                                                    │                   │
     │                                           [Dynamic Body Rewriter]       │
     │                                 (Replace dl with /crates-static/crates)
     │                                                    │                   │
     └───────── (Return rewritten config.json) ───────────┴───────────────────┘
     │
     ▼ (Query crate versions: Streaming bypass & 304 revalidation)
[cargo CLI] --- (GET /crates-index/ra/nd/rand) ----> [url-proxy] ---> [index.crates.io]
     │
     ▼ (Download .crate archive: Flow to proxy & 100% cached)
[cargo CLI] --- (GET /crates-static/crates/rand/rand-0.8.5.crate) ---> [url-proxy]
                                                                           │
                                                               [Permanent Disk Cache]
                                                               [Single-Flight Coalescing]
                                                               [Range 206 Resumption]
```

---

## 2. Server Configuration (`proxy.conf`)

Add the following configuration block to `proxy.conf`:

```ini
# ==============================================================================
# 7. Rust Cargo Sparse Index & Crates Closed-Loop Proxy (Feature 315)
# Client config: In ~/.cargo/config.toml, set registry = "sparse+http://<proxy>:8888/crates-index/"
# ==============================================================================
alias crates-index https://index.crates.io
alias crates-static https://static.crates.io

# Dynamic rewrite for config.json: Redirect crates download endpoint to local proxy
rewrite /crates-index/config.json type:json https://static.crates.io/crates http://{PROXY_HOST}/crates-static/crates

# Sparse index metadata dynamic bypass (streaming passthrough with 304 revalidation)
bypass /crates-index/ *

# Crates source package 100% permanent caching
cache /crates-static/ suffix .crate
cache * suffix .crate
```

---

## 3. Client Configuration

Assume `url-proxy` is running on your LAN at `192.168.1.100:8888`.

### 3.1 Global User Configuration (`~/.cargo/config.toml`)

Edit `~/.cargo/config.toml` (on Windows, `%USERPROFILE%\.cargo\config.toml`):

```toml
[source.crates-io]
replace-with = "url-proxy-sparse"

[source.url-proxy-sparse]
registry = "sparse+http://192.168.1.100:8888/crates-index/"
```

> [!TIP]
> The `sparse+http://` prefix is mandatory to instruct Cargo to use the HTTP Sparse Index Protocol instead of a Git repository.

### 3.2 Project-Level Configuration (`.cargo/config.toml`)

To scope the proxy configuration to a single repository, create `.cargo/config.toml` inside the project root with the same content.

### 3.3 CI/CD & Environment Variables

In CI/CD environments or Docker containers, inject the configuration via environment variables without creating files:

```bash
export CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse
export CARGO_SOURCE_CRATES_IO_REPLACE_WITH=url-proxy-sparse
export CARGO_SOURCE_URL_PROXY_SPARSE_REGISTRY="sparse+http://192.168.1.100:8888/crates-index/"
```

---

## 4. Verification & Diagnostics

### 4.1 Verify `config.json` Rewriting
```bash
curl -s http://192.168.1.100:8888/crates-index/config.json
```
**Expected Output**:
```json
{
  "dl": "http://192.168.1.100:8888/crates-static/crates",
  "api": "https://crates.io"
}
```
The `"dl"` field points to the local proxy's `/crates-static/crates` route.

### 4.2 Real Project Build
Create and build a test project:
```bash
cargo new hello-cargo && cd hello-cargo
cargo add rand tokio serde
cargo build
```
Proxy logs will confirm:
- `crates-index/...` requests are bypassed dynamically;
- `crates-static/crates/...` packages are downloaded by the leader and safely cached atomically on disk.

### 4.3 Offline & Cache Speedup Test
Clear client local cache and rebuild:
```bash
rm -rf target/
rm -rf ~/.cargo/registry/cache/*
cargo build
```
**Result**: All `.crate` files download at wire speed directly from the proxy cache with **0 external upstream bandwidth**.
