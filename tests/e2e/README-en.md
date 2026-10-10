# Docker Compose Core Features & Multi-Ecosystem E2E Automated Test Suite

This directory provides the Docker Compose End-to-End Automated Test Suite (E2E Suite) for `url-proxy`, including test image build definitions, container network orchestration, proxy configurations, test execution dispatchers, and terminal report inspection logic.

---

## 1. Overview & Verification Goals

The test suite aims to verify `url-proxy` in a real Docker Compose network environment against public upstream repositories across major language package managers and Linux distributions:

1. **Isolated Container Architecture**: The test proxy is built using `tests/e2e/Dockerfile`. All downloaded files are stored strictly inside `/cache` within the container, preventing host bind mount pollution;
2. **Dual-Mode Coverage**: Covers both **Path Prefix / Alias Mode** and **Standard HTTP Forward Proxy Mode** across 5 major ecosystems (APT, Alpine, PyPI, npm, Go);
3. **Advanced Feature End-to-End Testing**: Validates Maven Central Jar proxying, Rust Cargo sparse index endpoint rewriting & `.crate` package proxying, HTTP Range 206 partial content fetching, and HTTP 304 conditional request short-circuiting;
4. **Decoupled Test Targets**: Prefix Mode and Forward Proxy Mode use distinct lightweight packages (e.g., `tree` vs. `ed`), ensuring each test case triggers an independent cold download;
5. **Clean Terminal Reporting**: Outputs a monochrome ASCII test report summarizing duration, cache/tunnel status, and pass/fail conclusions.

---

## 2. Architecture & Dual-Mode Matrix

```text
                                  [Client Integration]
                                           |
           +-------------------------------+-------------------------------+
           |                                                               |
     Mode 1: Path Prefix / Alias Mode                             Mode 2: Standard HTTP Forward Proxy
  (e.g., GET /ubuntu/pool/... HTTP/1.1)                        (e.g., GET http://archive.ubuntu.com/pool/... HTTP/1.1)
           |                                                               |
           +-------------------------------+-------------------------------+
                                           |
                                           v
                              +-------------------------+
                              | url-proxy Service       |
                              | Port: 8888              |
                              | Config: proxy.conf      |
                              | Storage: /cache         |
                              +-------------------------+
                                           |
                                           v (Upstream Fetch)
            [Public Upstreams: PyPI / APT / Alpine / npm / Go / Maven / Cargo]
```

---

## 3. Full 14-Item E2E Test Matrix

| Eco ID | Ecosystem | Test Mode / Feature | Core Command | Verification & Assertions |
| :---: | :---: | :---: | :--- | :--- |
| 1 | **APT** | Prefix Mode | `apt download tree` | `.deb cached (tree)` |
| 2 | **APT** | Forward Proxy | `apt download ed` | `.deb cached (ed)` |
| 3 | **Alpine** | Prefix Mode | `apk fetch tree` | `.apk cached (tree)` |
| 4 | **Alpine** | Forward Proxy | `apk fetch sed` | `.apk cached (sed)` |
| 5 | **PyPI** | Prefix Mode | `pip install six` | `.whl cached (six)` |
| 6 | **PyPI** | Forward Proxy | `pip install certifi` | `HTTPS Tunnel` |
| 7 | **npm** | Prefix Mode | `npm install is-sorted` | `.tgz cached (is-sorted)` |
| 8 | **npm** | Forward Proxy | `npm install is-number` | `HTTPS Tunnel` |
| 9 | **Go** | GOPROXY Mode | `go download pkg/errors` | `.zip/.mod cached (errors)` |
| 10 | **Go** | Forward Proxy | `go download google/uuid` | `.zip/.mod cached (uuid)` |
| 11 | **Maven** | Central Proxy | `curl slf4j-api.jar` | `.jar cached (slf4j)` |
| 12 | **Cargo** | Sparse & Crates | `curl config.json & serde.crate` | `Rewritten & .crate cached` |
| 13 | **Range** | 206 Partial Content | `curl -r 0-499 InRelease` | `206 Partial Content` |
| 14 | **Conditional**| 304 Not Modified | `curl -H "If-None-Match: ..." InRelease` | `304 Not Modified` |

---

## 4. Directory Structure

- `Dockerfile`: Dedicated test image build file;
- `docker-compose.yml`: Container orchestration for `url-proxy` and 14 test clients;
- `proxy.conf`: Unified proxy configuration file;
- `test.sh`: Automated test runner and report generator script;
- `README-zh.md`: Chinese documentation;
- `README-en.md`: English documentation.

---

## 5. Running the Tests

In a WSL or Linux terminal, switch to the project root directory and run:

```bash
bash tests/e2e/test.sh
```

---

## 6. Expected Inspection Report

Upon successful completion, the terminal will display a report similar to:

```text
==================================================================================================
                                url-proxy E2E Inspection Report                                   
==================================================================================================
Ecosystem    Mode             Command                    Duration   Cache / Status             Result
--------------------------------------------------------------------------------------------------
APT          Prefix Mode      apt download tree          22.69s     .deb cached (tree)         [PASS]
APT          HTTP Proxy       apt download ed            15.27s     .deb cached (ed)           [PASS]
Alpine       Prefix Mode      apk fetch tree             2.90s      .apk cached (tree)         [PASS]
Alpine       HTTP Proxy       apk fetch sed              2.23s      .apk cached (sed)          [PASS]
PyPI         Prefix Mode      pip install six            17.59s     .whl cached (six)          [PASS]
PyPI         HTTP Proxy       pip install certifi        23.70s     HTTPS Tunnel               [PASS]
npm          Prefix Mode      npm install is-sorted      5.79s      .tgz cached (is-sorted)    [PASS]
npm          HTTP Proxy       npm install is-number      3.77s      HTTPS Tunnel               [PASS]
Go           GOPROXY Mode     go download pkg/errors     2.12s      .zip/.mod cached (errors)  [PASS]
Go           HTTP Proxy       go download google/uuid    2.19s      .zip/.mod cached (uuid)    [PASS]
Maven        Central Proxy    fetch slf4j-api.jar        1.85s      .jar cached (slf4j)        [PASS]
Cargo        Sparse & Crates  config rewrite & serde     2.04s      Rewritten & .crate cached  [PASS]
Range        HTTP 206 Status  curl range 0-499           0.82s      206 Partial Content        [PASS]
Conditional  HTTP 304 Status  curl conditional ETag      0.76s      304 Not Modified           [PASS]
--------------------------------------------------------------------------------------------------
Total: 14 | Passed: 14 | Failed: 0 | Overall Health: HEALTHY
==================================================================================================
```
