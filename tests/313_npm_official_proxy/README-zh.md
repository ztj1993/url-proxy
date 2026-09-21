# Feature 313: npm 官方镜像源代理本地测试与验证套件

本目录提供 Feature 313（npm 官方源全链路闭环代理）的本地独立测试环境与验证工具。

---

## 目录结构

- `mock_npm_server.go`: 模拟 npm 官方注册中心（`registry.npmjs.org`），监听 `9996` 端口。支持普通包、作用域包、点号包名以及单版本元数据接口与 `.tgz` 压缩包二进制下载。
- `proxy_npm.conf`: Feature 313 专用的测试代理配置文件。
- `README-zh.md`: 本说明文档（中文）。
- `README-en.md`: 英文说明文档。

---

## 快速启动验证

### 步骤 1：启动 Mock npm 上游服务

在终端 1 中运行：
```bash
go run tests/313_npm_official_proxy/mock_npm_server.go
```
看到输出 `Mock npm official registry server listening on :9996` 即表示启动成功。

### 步骤 2：启动 url-proxy 测试实例

在终端 2 中运行：
```bash
go run url-proxy.go -conf tests/313_npm_official_proxy/proxy_npm.conf -addr :8888
```

---

## 验证场景

### 1. 标准包元数据重写验证 (`lodash`)
```bash
curl -s http://127.0.0.1:8888/npm/lodash | grep tarball
```
**期望结果**：输出包含代理主机地址 `http://127.0.0.1:8888/npm/lodash/-/lodash-4.17.21.tgz`，官方域名 `https://registry.npmjs.org/` 已被成功重写。

### 2. 作用域包元数据重写验证 (`@types/node`)
```bash
curl -s http://127.0.0.1:8888/npm/@types/node | grep tarball
```
**期望结果**：输出重写为 `http://127.0.0.1:8888/npm/@types/node/-/node-20.11.0.tgz`。

### 3. 带点号包名元数据重写验证 (`socket.io`)
```bash
curl -s http://127.0.0.1:8888/npm/socket.io | grep tarball
```
**期望结果**：输出重写为 `http://127.0.0.1:8888/npm/socket.io/-/socket.io-4.7.2.tgz`。

### 4. Tarball 首次下载与本地持久化
```bash
curl -i http://127.0.0.1:8888/npm/lodash/-/lodash-4.17.21.tgz
```
**期望结果**：返回 HTTP 200，内容为 Mock 数据；代理日志显示 `job leader` 并成功原子重命名缓存文件。

### 5. Range 分片断点续传与 0 上游回源
```bash
curl -i -r 0-9 http://127.0.0.1:8888/npm/lodash/-/lodash-4.17.21.tgz
```
**期望结果**：返回 HTTP 206 Partial Content，直接由本地磁盘缓存服务，上游服务无任何新增请求日志。

---

## npm 真实客户端实机验证

在配置好生产 `proxy.conf` 后，可通过实际命令行进行测试：
```bash
# 临时指定 registry 进行安装
npm install --registry http://127.0.0.1:8888/npm/ --no-cache chalk
```
代理日志中将记录：
1. `req: npm/chalk` (动态元数据重写)
2. `req: npm/chalk/-/chalk-....tgz` (二进制包命中并持久化)
