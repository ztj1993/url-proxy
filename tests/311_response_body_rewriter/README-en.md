# Feature 311: Streaming Response Body Rewriter Test Guide

This document describes how to verify the Streaming Response Body Rewriting Engine (Feature 311) of `url-proxy`.

> **Note**: All steps assume the terminal's **current working directory is this test directory** (`tests/311_response_body_rewriter/`).

---

## 1. Test Objectives & Verification Criteria

Verify the real-time dynamic body rewriting capability in `url-proxy`:
1. **HTML Hyperlink Rewriting**: For PyPI Simple indexes, replace `https://files.pythonhosted.org/` with `http://{PROXY_HOST}/pythonhosted/`;
2. **JSON Metadata Rewriting**: For npm metadata, replace `https://registry.npmjs.org/` in `dist.tarball` with `http://{PROXY_HOST}/npm/`;
3. **MIME-Type Filtering**: Binary payloads (`application/octet-stream`) remain strictly unmodified even if they contain pattern strings;
4. **Gzip Response Adaptation**: Upstream gzip responses are transparently decompressed, rewritten, stripped of `Content-Encoding`, and updated with proper `Content-Length`;
5. **Header Normalization**: Original `ETag` and `Content-MD5` headers are stripped to prevent client verification failure.

---

## 2. Setup Test Environment

The test directory contains:
- `mock_server.go`: Mock server listening on `127.0.0.1:9998`.
- `proxy_test.conf`: Test configuration with rewrite rules and aliases.

### Step 2.1: Start Mock Server
In **Terminal 1**:
```bash
go run mock_server.go
```
*Expected*: `mock server listening on 127.0.0.1:9998`

### Step 2.2: Start url-proxy
In **Terminal 2**:
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*Expected*: Logs show `proxy config loaded from explicit CLI argument: ./proxy_test.conf` with `3 rewrite rules`.

---

## 3. Execution & Verification

In **Terminal 3**, execute the following test requests:

### Test 1: PyPI HTML Index Rewriting & ETag Stripping
```bash
curl -i http://localhost:8888/test-pypi/simple/requests/
```
*Verify*:
- Hyperlinks updated to `http://localhost:8888/pythonhosted/packages/a/b/requests-2.31.0.whl`;
- No `ETag` in response headers;
- `Content-Length` matches rewritten body size.

### Test 2: npm JSON Metadata Rewriting
```bash
curl -i http://localhost:8888/test-npm/pkg
```
*Verify*:
- Tarball URL updated to `http://localhost:8888/npm/pkg/-/pkg-1.0.tgz`.

### Test 3: Binary Stream Safety (MIME Filter)
```bash
curl -s http://localhost:8888/test-pypi/binary/test.whl
```
*Verify*:
- Returns untampered binary stream: `RAW_BINARY_STREAM_WITH_https://files.pythonhosted.org/_KEEP_SAFE`.

### Test 4: Gzip Decompression & Rewriting
```bash
curl -i http://localhost:8888/test-pypi/gzip/index.html
```
*Verify*:
- `Content-Encoding: gzip` is removed;
- Plain text HTML contains rewritten link `http://localhost:8888/pythonhosted/gz.whl`.
