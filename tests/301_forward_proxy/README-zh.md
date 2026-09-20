# 标准 HTTP 正向代理协议测试指南

本文档介绍如何测试 `url-proxy` 的标准 HTTP 正向代理协议（Standard Forward HTTP Proxy Protocol）支持。

## 1. 测试原理

验证客户端无需在 URL 路径中添加任何代理前缀，仅通过标准代理配置（如 `curl -x`、`http_proxy` 环境变量、或 APT 代理指令 `Acquire::http::Proxy`），直接将流量发往 `url-proxy`：
- 客户端发送标准绝对 URI 请求行：`GET http://localhost:9996/proxy-test HTTP/1.1`
- `url-proxy` 能够自动识别并解析目标地址，执行正常的上游拉取、内容中继与缓存落盘。
- 同时验证原有的路径模式（`GET /http://...`）依然完全兼容。

## 2. 准备测试环境

在当前目录下，我们提供了一个简单的 Go 程序 `mock_server.go` 来模拟目标 Web 服务器。

### 步骤 2.1：启动模拟目标服务器
打开一个终端，运行以下命令启动服务器：
```bash
go run mock_server.go
```
*预期输出：`测试用标准代理目标服务器已启动，监听端口 :9996`*

### 步骤 2.2：启动 url-proxy
打开**第二个**终端，在项目根目录下运行：
```bash
go run url-proxy.go -addr :8888
```
*预期输出：`http server: :8888`*

## 3. 执行测试

打开**第三个**终端，分别使用标准正向代理模式与路径兼容模式执行测试：

### 方式一：使用 curl -x 指定正向代理
```bash
curl -i -x http://localhost:8888 http://localhost:9996/proxy-test
```

### 方式二：使用 http_proxy 环境变量
```bash
http_proxy=http://localhost:8888 curl -i http://localhost:9996/proxy-test
```

### 方式三：验证路径前缀兼容性
```bash
curl -i http://localhost:8888/http://localhost:9996/proxy-test
```

## 4. 观察与验证

### 4.1 curl 响应验证
检查终端输出：
```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
X-Forward-Proxy-Mock: true
...

url-proxy standard forward proxy test: PASSED
```
*验证要点*：
1. 客户端直接请求目标地址 `http://localhost:9996/proxy-test` 并通过 `-x` 指向代理，成功获取返回。
2. 响应头 `X-Forward-Proxy-Mock: true` 完整透传。

### 4.2 url-proxy 终端日志验证
观察代理输出：
```text
[1] req: http://localhost:9996/proxy-test
[1] job leader: http://localhost:9996/proxy-test
[1] job download: http://localhost:9996/proxy-test
[1] job finished: http://localhost:9996/proxy-test
```
代理成功提取出完整的 `http://localhost:9996/proxy-test` 目标，并正常执行请求调度与缓存管理。

## 5. 结论
通过上述测试，您可以验证 `url-proxy` 能够原生支持标准 HTTP 正向代理协议，使得包括 APT（通过 `/etc/apt/apt.conf.d/01proxy`）在内的标准系统组件能够零侵入接入代理。
