# Feature 312: PyPI Official Mirror Closed-Loop Proxy Testing Guide

This guide explains how to verify that `url-proxy` acts as a complete caching proxy for official Python PyPI repositories (Feature 312).

> **Note**: This document assumes your terminal's **current working directory** is `tests/312_pypi_official_proxy/`.

---

## 1. Principle & Objectives

Official PyPI architecture decouples metadata indexing from artifact delivery:
- Index endpoint: `https://pypi.org/simple/<pkg>/`
- Artifact CDN: `https://files.pythonhosted.org/packages/...`

When `pip` requests an index, the returned HTML or JSON contains absolute CDN URLs. With Feature 312:
1. **PEP 503 (HTML) Dynamic Rewriting**: Replaces `https://files.pythonhosted.org/` in anchor tags with the proxy's `/pythonhosted/` path, while preserving the `#sha256=...` package verification hash.
2. **PEP 691 (JSON) Dynamic Rewriting**: Replaces `files[].url` values when the client requests `Accept: application/vnd.pypi.simple.v1+json`.
3. **Artifact 100% Disk Caching**: Subsequent wheel downloads route through `/pythonhosted/packages/.../*.whl` and enter Single-Flight coalescing and disk caching.
4. **Zero-WAN Secondary Cache Hits**: Re-installing the package retrieves data directly from local disk.

---

## 2. Environment Setup

### Step 2.1: Start Mock Upstream Server
Open **Terminal 1** and run:
```bash
go run mock_pypi_server.go
```
*Expected output*: `Mock PyPI and pythonhosted CDN server listening on :9997`

### Step 2.2: Launch url-proxy
Open **Terminal 2** and start `url-proxy` with the test configuration:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_pypi.conf
```
*Expected output*: Log message confirming `proxy_pypi.conf` loaded with aliases and rewrite rules.

---

## 3. Test Execution

Open **Terminal 3** and execute the verification requests:

### Test 1: PEP 503 HTML Simple Index Rewriting
```bash
curl -i http://localhost:8888/pypi/simple/requests/
```
*Verification points*:
- Response header `Content-Type: text/html; charset=utf-8` without upstream `ETag`.
- Body link is rewritten to:
  `http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl#sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- No occurrences of `https://files.pythonhosted.org/`.

### Test 2: PEP 691 JSON Simple API Rewriting
```bash
curl -i -H "Accept: application/vnd.pypi.simple.v1+json" http://localhost:8888/pypi/simple/requests/
```
*Verification points*:
- Header `Content-Type: application/vnd.pypi.simple.v1+json`.
- In JSON body:
  `"url": "http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl"`.

### Test 3: Wheel Download & Disk Persistence
```bash
curl -i http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl
```
*Verification points*:
- Returns `200 OK` and mock binary content.
- Inspect `./test_cache/` to confirm persistence at:
  `./test_cache/http-127.0.0.1-9997/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl`.

### Test 4: Secondary Download Cache Hit
Run the download again:
```bash
curl -i http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl
```
*Verification points*:
- Header contains `Accept-Ranges: bytes`.
- Terminal 1 (Mock Server) produces no new access logs (0 upstream traffic).

---

## 4. Production pip Verification

```bash
pip install requests --index-url http://127.0.0.1:8888/pypi/simple/ --trusted-host 127.0.0.1
```
Logs confirm all `.whl` downloads route through `http://127.0.0.1:8888/pythonhosted/` with 100% caching.
