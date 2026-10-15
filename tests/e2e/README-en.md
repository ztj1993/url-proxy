# Docker Compose Multi-Ecosystem Full-Feature E2E Automated Test Suite

This directory provides the end-to-end automated test suite (E2E Suite) for `url-proxy` using Docker Compose, including test image definitions, container network orchestration, configuration, test dispatching, and formatted terminal inspection reports.

---

## 1. Overview & Test Objectives

The test suite validates `url-proxy` across major language package managers and operating system ecosystems in a real Docker Compose network environment against live upstream repositories:

1. **Isolated Containerization**: Tests run in an isolated environment built via `tests/e2e/Dockerfile`, with all cached artifacts confined to `/cache` inside the `url-proxy` container without host bind-mount pollution;
2. **Dual-Mode Multi-Ecosystem Coverage**: Covers APT, Alpine, PyPI, npm, and Go across both **Prefix Mode** and **HTTP Forward Proxy Mode**;
3. **Advanced Governance & Concurrency End-to-End Validation**:
   - Maven Central Jar proxy and second-download bit-identical Cache-HIT;
   - Rust Cargo sparse index rewriting, header cleansing, and crate caching;
   - HTTP Range 206 local cache slice direct extraction and size assertions;
   - HTTP 304 conditional request local fast short-circuit and SWR 304 renewal;
   - Non-200 404 anti-pollution zero-disk and metadata (Release, APKINDEX, etc.) zero-disk bypass;
   - HEAD request method conversion pre-warming against 0-byte corruption;
   - Disk TTL fresh HIT and client ETag isolation guard;
   - Docker / OCI Registry v2 manifest dynamic bypass and layer blob caching;
   - Concurrent Request Coalescing (Singleflight): Leader upstream fetch + Follower broadcast streaming;
4. **Collision-Free Isolated Test Cases**: Dual modes in each ecosystem use distinct, lightweight packages (e.g., `tree` vs. `ed`) to guarantee independent cold cache fetches;
5. **Formatted Inspection Report**: Generates a clean ASCII table on completion, auditing elapsed time, cache status, and pass/fail conclusions.

---

## 2. Architecture & Access Matrix

```text
                                  [Client Ingress Layer]
                                             |
        +------------------------------------+-----------------------------------+
        |                                                                        |
  Mode 1: Path Prefix / Alias Mode                                Mode 2: Standard HTTP Forward Proxy
(e.g., GET /ubuntu/pool/... HTTP/1.1)                          (e.g., GET http://archive.ubuntu.com/pool/...)
        |                                                                        |
        +------------------------------------+-----------------------------------+
                                             |
                                             v
                                +-------------------------+
                                | url-proxy Test Service  |
                                | Port: 8888              |
                                | Config: proxy.conf      |
                                | Cache: /cache           |
                                +-------------------------+
                                             |
                                             v (Upstream Relay & Caching)
            [Upstream: PyPI / APT / Alpine / npm / Go / Maven / Cargo / Mock Docker]
```

---

## 3. Complete 23-Item E2E Test Matrix

| No. | Ecosystem / Feature | Test Mode / Feature | Core Command | Expected Result & Assertion |
| :---: | :---: | :---: | :--- | :--- |
| 1 | **APT** | Prefix Mode | `apt download tree` | `.deb cached (tree)` |
| 2 | **APT** | HTTP Proxy | `apt download ed` | `.deb cached (ed)` |
| 3 | **Alpine** | Prefix Mode | `apk fetch tree` | `.apk cached (tree)` |
| 4 | **Alpine** | HTTP Proxy | `apk fetch sed` | `.apk cached (sed)` |
| 5 | **PyPI** | Prefix Mode | `pip install six` | `.whl cached (six)` |
| 6 | **PyPI** | HTTP Proxy | `pip install certifi` | `HTTPS Tunnel` (Audits CONNECT log) |
| 7 | **npm** | Prefix Mode | `npm install is-sorted` | `.tgz cached (is-sorted)` |
| 8 | **npm** | HTTP Proxy | `npm install is-number` | `HTTPS Tunnel` (Audits CONNECT log) |
| 9 | **Go** | GOPROXY Mode | `go download pkg/errors` | `.zip/.mod cached (errors)` (no list leak) |
| 10 | **Go** | HTTP Proxy | `go download google/uuid` | `.zip/.mod cached (uuid)` |
| 11 | **Maven** | Central Proxy | `fetch slf4j-api.jar` | `.jar cached (slf4j)` |
| 12 | **Cargo** | Sparse & Crates | `curl config.json & serde.crate` | `Rewritten & .crate cached` |
| 13 | **Range** | 206 Partial Content | `curl -r 0-499 slf4j-api.jar` | `206 Partial Content (Cache)` (local cache slice) |
| 14 | **Conditional**| 304 Not Modified | `curl -H "If-Modified-Since: ..." InRelease` | `304 Not Modified (Local)` (local short-circuit) |
| 15 | **Cache-HIT** | Second Download | `verify HIT & identical` | `HIT & Bit-Identical` |
| 16 | **Anti-Pollute** | 404 Zero-Disk | `curl 404 fake-artifact` | `Zero-Disk (Clean)` |
| 17 | **HEAD-Guard** | HEAD Conversion | `HEAD probe & GET warm` | `Pre-warmed (>0 Bytes)` |
| 18 | **Rewrite** | Header Cleansing | `strip ETag/Digest & len` | `Sanitized & Length OK` |
| 19 | **Disk-TTL** | Fresh HIT & Guard | `TTL hit & ETag isolate` | `HIT-DISK-TTL (Isolated)` |
| 20 | **Docker** | OCI Registry v2 | `manifest bypass & blob` | `Manifest Bypass & Layer Hit` |
| 21 | **Singleflight**| Request Merge | `concurrent fetch & wait` | `Leader Coalesced & Broadcast` |
| 22 | **SWR-Renew** | 304 TTL Renewal | `expired 304 & touch mtime` | `REVALIDATED-304 & Renewed` |
| 23 | **Bypass-Guard**| Zero-Disk Meta | `verify bypass non-cached` | `Zero-Disk (Metadata Clean)` |

---

## 4. File Structure

- `Dockerfile`: Test image build definition (builds both `url-proxy` and `mock-docker`);
- `docker-compose.yml`: Service orchestration for `url-proxy`, `mock-docker`, and 23 test client containers;
- `proxy.conf`: Multi-ecosystem unified proxy configuration;
- `test.sh`: One-click automated test runner, validator, and reporter;
- `README-zh.md`: Chinese documentation;
- `README-en.md`: English documentation.

---

## 5. Execution

Run the following command from the project root in WSL or Linux:

```bash
bash tests/e2e/test.sh
```

---

## 6. Expected Inspection Report

```text
==================================================================================================
                                url-proxy E2E Inspection Report                                   
==================================================================================================
Ecosystem    Mode             Command                    Duration   Cache / Status               Result
--------------------------------------------------------------------------------------------------
APT          Prefix Mode      apt download tree          22.69s     .deb cached (tree)           [PASS]
APT          HTTP Proxy       apt download ed            15.27s     .deb cached (ed)             [PASS]
Alpine       Prefix Mode      apk fetch tree             2.90s      .apk cached (tree)           [PASS]
Alpine       HTTP Proxy       apk fetch sed              2.23s      .apk cached (sed)            [PASS]
PyPI         Prefix Mode      pip install six            17.59s     .whl cached (six)            [PASS]
PyPI         HTTP Proxy       pip install certifi        23.70s     HTTPS Tunnel                 [PASS]
npm          Prefix Mode      npm install is-sorted      5.79s      .tgz cached (is-sorted)      [PASS]
npm          HTTP Proxy       npm install is-number      3.77s      HTTPS Tunnel                 [PASS]
Go           GOPROXY Mode     go download pkg/errors     2.12s      .zip/.mod cached (errors)    [PASS]
Go           HTTP Proxy       go download google/uuid    2.19s      .zip/.mod cached (uuid)      [PASS]
Maven        Central Proxy    fetch slf4j-api.jar        1.85s      .jar cached (slf4j)          [PASS]
Cargo        Sparse & Crates  config rewrite & serde     2.04s      Rewritten & .crate cached    [PASS]
Range        HTTP 206 Status  curl range 0-499           0.82s      206 Partial Content (Cache)  [PASS]
Conditional  HTTP 304 Status  curl conditional 304       0.76s      304 Not Modified (Local)     [PASS]
Cache-HIT    Second Download  verify HIT & identical     1.20s      HIT & Bit-Identical          [PASS]
Anti-Pollute 404 Zero-Disk    curl 404 fake-artifact     0.45s      Zero-Disk (Clean)            [PASS]
HEAD-Guard   Method Conversion HEAD probe & GET warm     2.35s      Pre-warmed (>0 Bytes)        [PASS]
Rewrite      Header Cleansing strip ETag/Digest & len    1.15s      Sanitized & Length OK        [PASS]
Disk-TTL     Fresh HIT & Guard TTL hit & ETag isolate    1.30s      HIT-DISK-TTL (Isolated)      [PASS]
Docker       OCI Registry v2  manifest bypass & blob     0.95s      Manifest Bypass & Layer Hit  [PASS]
Singleflight Request Merge    concurrent fetch & wait    1.45s      Leader Coalesced & Broadcast [PASS]
SWR-Renew    304 TTL Renewal  expired 304 & touch mtime  4.20s      REVALIDATED-304 & Renewed    [PASS]
Bypass-Guard Zero-Disk Meta   verify bypass non-cached   1.10s      Zero-Disk (Metadata Clean)   [PASS]
--------------------------------------------------------------------------------------------------
Total: 23 | Passed: 23 | Failed: 0 | Overall Health: HEALTHY
==================================================================================================
```