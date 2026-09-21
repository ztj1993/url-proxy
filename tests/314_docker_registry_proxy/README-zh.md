# Feature 314: Docker / OCI 镜像 Registry v2 协议中继与分层缓存本地测试套件

本目录提供 Feature 314（Docker / OCI 镜像 Registry v2 协议中继与分层缓存）的本地独立测试环境与验证工具。

---

## 目录结构

- `mock_docker_server.go`: 模拟 Docker Registry v2 官方服务（`registry-1.docker.io`）及 Auth 鉴权服务，监听 `9995` 端口。支持 `/v2/` 探测、401 Bearer Token 握手挑战、Manifest 清单 304 条件协商、Layer Blob 307 CDN 重定向与 HEAD 原生加速。
- `proxy_docker.conf`: Feature 314 专用的测试代理配置文件。
- `README-zh.md`: 本说明文档（中文）。
- `README-en.md`: 英文说明文档。

---

## 快速启动验证

### 步骤 1：启动 Mock Docker Registry 上游服务

在终端 1 中运行：
```bash
go run tests/314_docker_registry_proxy/mock_docker_server.go
```
看到输出 `Mock Docker Registry v2 running on port :9995` 即表示启动成功。

### 步骤 2：启动 url-proxy 测试实例

在终端 2 中运行：
```bash
go run url-proxy.go -conf tests/314_docker_registry_proxy/proxy_docker.conf -addr :8888
```

---

## 验证场景与步骤

### 1. v2 探测与 401 Bearer Token 认证挑战透传
```bash
curl -i http://127.0.0.1:8888/v2/
```
**期望结果**：
- 返回 `HTTP/1.1 401 Unauthorized`；
- 响应头包含 `Www-Authenticate: Bearer realm="http://127.0.0.1:9995/token",service="127.0.0.1:9995"`；
- 响应头包含 `Docker-Distribution-Api-Version: registry/2.0`。

### 2. 模拟客户端获取 Bearer Token
```bash
curl -s http://127.0.0.1:9995/token
```
**期望结果**：返回 JSON Token（`{"token":"mock-docker-token", ...}`）。

### 3. 携带 Token 拉取 Manifest 清单（304 缓存协商与动态透传）
```bash
# 正常拉取清单
curl -i -H "Authorization: Bearer mock-docker-token" http://127.0.0.1:8888/v2/library/ubuntu/manifests/22.04

# 条件协商拉取 (If-None-Match)
curl -i -H "Authorization: Bearer mock-docker-token" -H 'If-None-Match: "ubuntu-2204-etag"' http://127.0.0.1:8888/v2/library/ubuntu/manifests/22.04
```
**期望结果**：
- 首次返回 HTTP 200 及包含分层 Digest 的 Manifest JSON；
- 条件请求返回 `HTTP/1.1 304 Not Modified`，不传输 Body。

### 4. 首次下载 Layer Blob（307 CDN 重定向自动跟随与本地落盘）
```bash
curl -i -H "Authorization: Bearer mock-docker-token" http://127.0.0.1:8888/v2/library/ubuntu/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
```
**期望结果**：
- 返回 HTTP 200 及分层二进制数据；
- 代理后台日志输出 `job leader`、`job download` 与 `job renamed successfully`；
- 缓存目录安全创建 `sha256%3A...` 本地文件。

### 5. 原生 HEAD 请求加速（0 上游请求回源）
```bash
curl -I http://127.0.0.1:8888/v2/library/ubuntu/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
```
**期望结果**：
- 直接返回 HTTP 200，并准确包含 `Content-Length` 与 `Accept-Ranges: bytes`；
- Mock 上游日志无任何新增请求，实现 0 回源。

### 6. Range 分片断点续传（0 上游请求回源）
```bash
curl -i -r 0-15 http://127.0.0.1:8888/v2/library/ubuntu/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
```
**期望结果**：
- 返回 `HTTP/1.1 206 Partial Content`，直接由本地磁盘缓存服务。

### 7. 路径别名模式兼容性验证 (`/docker/v2/`)
```bash
curl -i http://127.0.0.1:8888/docker/v2/
curl -i -H "Authorization: Bearer mock-docker-token" http://127.0.0.1:8888/docker/v2/library/ubuntu/manifests/22.04
```
**期望结果**：与 `/v2/` 镜像别名模式行为完全一致。
