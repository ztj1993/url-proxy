# 基础透明代理转发测试指南

本文档介绍如何测试 `url-proxy` 的基础透明中继与 Header 透传能力。

## 1. 测试原理

验证 `url-proxy` 是否能够作为透明管道将客户端请求正确转发至上游服务器：
- 上游返回特定的响应头（如 `X-Custom-Header`、`Content-Type`）及固定内容。
- 客户端通过代理请求后，应能完整接收到与上游完全一致的响应头、状态码与响应体。

## 2. 准备测试环境

在当前目录下，我们提供了一个简单的 Go 程序 `mock_server.go` 来模拟上游服务器。

### 步骤 2.1：启动模拟上游服务器
打开一个终端，运行以下命令启动服务器：
```bash
go run mock_server.go
```
*预期输出：`测试用基础上游服务器已启动，监听端口 :9991`*

### 步骤 2.2：启动 url-proxy
打开**第二个**终端，在项目根目录下运行：
```bash
go run url-proxy.go -addr :8888
```
*预期输出：`http server: :8888`*

## 3. 执行测试

打开**第三个**终端，使用 `curl -i` 通过代理请求目标上游资源：
```bash
curl -i http://localhost:8888/http://localhost:9991/test-file
```

## 4. 观察与验证

### 4.1 curl 响应验证
检查终端输出的 HTTP 响应报文：
```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Server: Mock-Basic-Server/1.0
X-Custom-Header: ProxyTestPass
...

url-proxy basic transparent proxy test: PASSED
```
*验证要点*：
1. HTTP 状态码为 `200 OK`。
2. 自定义响应头 `X-Custom-Header: ProxyTestPass` 成功透传。
3. 响应内容完整无损。

### 4.2 url-proxy 终端日志验证
观察代理输出的日志：
```text
[1] req: http://localhost:9991/test-file
[1] job leader: http://localhost:9991/test-file
[1] job download: http://localhost:9991/test-file
[1] job finished: http://localhost:9991/test-file
```

## 5. 结论
通过上述测试，您可以验证 `url-proxy` 的基础透明代理与响应头中继功能工作正常。
