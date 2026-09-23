# Feature 315: Rust Cargo 稀疏索引与 Crates 官方源代理测试套件

本目录提供 Feature 315（Rust Cargo 稀疏索引与 Crates 官方源闭环代理）的本地独立测试环境与验证工具。

---

## 目录结构

- `mock_cargo_server.go`: 模拟 `index.crates.io`（RFC 2785 稀疏索引）与 `static.crates.io`（Crate 源码包分发），监听 `9994` 端口。支持 `/config.json`、多级稀疏索引查询、304 缓存协商以及 `.crate` 源码包下载。
- `proxy_cargo.conf`: Feature 315 专用的测试代理配置文件。
- `README-zh.md`: 本说明文档（中文）。
- `README-en.md`: 英文说明文档。

---

## 快速启动验证

### 步骤 1：启动 Mock Cargo 上游服务

在终端 1 中运行：
```bash
go run tests/315_rust_cargo_proxy/mock_cargo_server.go
```
看到输出 `Mock Cargo & Crates Server running on port :9994` 即表示启动成功。

### 步骤 2：启动 url-proxy 测试实例

在终端 2 中运行：
```bash
go run url-proxy.go -conf tests/315_rust_cargo_proxy/proxy_cargo.conf -addr :8888
```

---

## 验证场景与步骤

### 1. 验证 `config.json` 动态响应体重写与闭环重定向
```bash
curl -i http://127.0.0.1:8888/crates-index/config.json
```
**期望结果**：
- 返回 `HTTP/1.1 200 OK`；
- JSON 响应体中的 `"dl"` 字段被自动替换为本地代理地址：`"dl": "http://127.0.0.1:8888/crates-static/crates"`；
- 原始外网地址 `https://static.crates.io/crates` 已被替换；
- 上游 `ETag` 已被安全剥离，`Content-Length` 重新计算。

### 2. 验证稀疏索引查询与 304 快速协商
```bash
# 首次查询 rand 包索引 (200 OK)
curl -i http://127.0.0.1:8888/crates-index/ra/nd/rand

# 条件协商查询 (304 Not Modified)
curl -i -H 'If-None-Match: "mock-index-etag-123"' http://127.0.0.1:8888/crates-index/ra/nd/rand
```
**期望结果**：
- 首次返回 HTTP 200 及 NDJSON 索引；
- 条件协商返回 `HTTP/1.1 304 Not Modified`，不传输多余包体，上游即刻感知。

### 3. 验证短包名分层规范 (1, 2, 3 字符包名)
```bash
curl -i http://127.0.0.1:8888/crates-index/1/a
curl -i http://127.0.0.1:8888/crates-index/2/ab
curl -i http://127.0.0.1:8888/crates-index/3/a/abc
```
**期望结果**：均能正确中继并流式返回索引。

### 4. 首次下载 `.crate` 源码包（本地原子落盘缓存）
```bash
curl -i http://127.0.0.1:8888/crates-static/crates/rand/rand-0.8.5.crate
```
**期望结果**：
- 返回 HTTP 200 及二进制数据；
- 代理后台日志输出 `job leader`、`job download` 并原子重命名至最终缓存文件。

### 5. 本地缓存命中与 Range 206 断点续传（0 上游请求回源）
```bash
# Range 分片拉取
curl -i -r 0-15 http://127.0.0.1:8888/crates-static/crates/rand/rand-0.8.5.crate

# 检查上游请求数
curl -s http://127.0.0.1:9994/stats
```
**期望结果**：
- 返回 `HTTP/1.1 206 Partial Content`，直接由本地磁盘缓存服务；
- `crate_requests` 计数器保持为 1，无任何多余上游回源。
