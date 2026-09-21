# Feature 313: npm Official Registry Proxy Simulation & Test Suite

This directory provides a standalone local testing and simulation environment for Feature 313 (Official npm registry closed-loop proxy).

---

## Directory Structure

- `mock_npm_server.go`: Simulates the official npm registry (`registry.npmjs.org`), listening on port `9996`. Supports standard packages, scoped packages, dotted package names, single-version metadata queries, and `.tgz` binary downloads.
- `proxy_npm.conf`: Dedicated proxy configuration file for Feature 313 testing.
- `README-zh.md`: Chinese test walkthrough guide.
- `README-en.md`: English test walkthrough guide.

---

## Quick Start

### Step 1: Start Mock npm Registry Server

In Terminal 1:
```bash
go run tests/313_npm_official_proxy/mock_npm_server.go
```
The server will log `Mock npm official registry server listening on :9996`.

### Step 2: Start url-proxy Instance

In Terminal 2:
```bash
go run url-proxy.go -conf tests/313_npm_official_proxy/proxy_npm.conf -addr :8888
```

---

## Test Verification Scenarios

### 1. Standard Package Metadata Rewriting (`lodash`)
```bash
curl -s http://127.0.0.1:8888/npm/lodash | grep tarball
```
**Expected**: The output contains the proxy address `http://127.0.0.1:8888/npm/lodash/-/lodash-4.17.21.tgz`.

### 2. Scoped Package Metadata Rewriting (`@types/node`)
```bash
curl -s http://127.0.0.1:8888/npm/@types/node | grep tarball
```
**Expected**: Rewritten to `http://127.0.0.1:8888/npm/@types/node/-/node-20.11.0.tgz`.

### 3. Dotted Package Name Metadata Rewriting (`socket.io`)
```bash
curl -s http://127.0.0.1:8888/npm/socket.io | grep tarball
```
**Expected**: Rewritten to `http://127.0.0.1:8888/npm/socket.io/-/socket.io-4.7.2.tgz`.

### 4. Tarball First Download & Disk Persistence
```bash
curl -i http://127.0.0.1:8888/npm/lodash/-/lodash-4.17.21.tgz
```
**Expected**: HTTP 200 with payload, safely renamed and cached on disk.

### 5. Range Request Cache Hit with Zero Upstream Hits
```bash
curl -i -r 0-9 http://127.0.0.1:8888/npm/lodash/-/lodash-4.17.21.tgz
```
**Expected**: HTTP 206 Partial Content served directly from disk cache with zero additional requests to upstream.
