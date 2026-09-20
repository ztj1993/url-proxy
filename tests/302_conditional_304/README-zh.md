# 条件请求与 304 快速协商透传测试指南

本文档介绍如何测试 `url-proxy` 对 HTTP 条件请求（Conditional Requests）与 `304 Not Modified` 快速协商透传的支持。

## 1. 测试原理

验证客户端通过代理发起带有协商头（`If-Modified-Since` 或 `If-None-Match`）的请求时：
- 代理将条件头完整透传至上游源站。
- 当源站判定未修改并返回 `HTTP/1.1 304 Not Modified` 时：
  - 代理立即透传 304 响应与相关头部（如 `ETag`、`Last-Modified`）。
  - 不拉取任何 Body 数据（0 字节负载传输）。
  - 现存本地缓存文件完整无损，不会被覆盖或清空。
- 首次无条件请求时，正常返回 `200 OK` 并缓存文件。

## 2. 准备测试环境

在当前测试目录下，提供了模拟源站程序 `mock_server.go`（监听 `:9994` 端口）。

### 步骤 2.1：启动模拟源站服务器
在当前目录打开终端，运行：
```bash
go run mock_server.go
```
*预期输出：`测试用条件协商目标服务器已启动，监听端口 :9994`*

### 步骤 2.2：启动 url-proxy
打开**第二个**终端，进入当前测试目录后运行：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./cache
```
*预期输出：`http server: :8888`*

## 3. 执行测试

打开**第三个**终端，按顺序执行以下测试验证：

### 阶段一：首次请求（无条件头，应获取 200 OK 并写入缓存）
使用 `curl -i` 发起首次请求：
```bash
curl -i http://localhost:8888/http://localhost:9994/conditional-test
```
*预期响应*：
```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Etag: "packages-v1-hash"
Last-Modified: Sat, 20 Sep 2026 08:00:00 GMT
...

Package: test-pkg
Version: 1.0.0
Architecture: amd64
```
此时检查 `./cache` 目录，缓存文件已完整生成。

---

### 阶段二：携带 ETag 条件请求（304 快速协商透传）
携带上一次获取到的 ETag（`If-None-Match: "packages-v1-hash"`）发起请求：
```bash
curl -i -H 'If-None-Match: "packages-v1-hash"' http://localhost:8888/http://localhost:9994/conditional-test
```
*预期响应*：
```text
HTTP/1.1 304 Not Modified
Etag: "packages-v1-hash"
Last-Modified: Sat, 20 Sep 2026 08:00:00 GMT
```
*验证要点*：
1. HTTP 状态码为 `304 Not Modified`。
2. 响应体为空（0 字节 Body）。
3. 检查 `./cache` 中的文件，文件内容完好无损且修改时间未被空文件破坏。

---

### 阶段三：携带 Last-Modified 条件请求（304 快速协商透传）
携带时间戳（`If-Modified-Since: Sat, 20 Sep 2026 08:00:00 GMT`）发起请求：
```bash
curl -i -H 'If-Modified-Since: Sat, 20 Sep 2026 08:00:00 GMT' http://localhost:8888/http://localhost:9994/conditional-test
```
*预期响应*：同样返回 `HTTP/1.1 304 Not Modified` 且无 Body 数据。

---

### 阶段四：验证正向代理模式下的条件协商
结合功能 301（正向代理模式），使用 `curl -x` 发起条件请求：
```bash
curl -i -x http://localhost:8888 -H 'If-None-Match: "packages-v1-hash"' http://localhost:9994/conditional-test
```
*预期响应*：在正向代理模式下同样精准返回 `HTTP/1.1 304 Not Modified`。

## 4. 观察与日志验证

### 4.1 mock_server 终端日志
```text
[mock_server] 收到请求: GET /conditional-test, If-None-Match: , If-Modified-Since: 
[mock_server] 未命中条件，返回 200 OK 完整数据
[mock_server] 收到请求: GET /conditional-test, If-None-Match: "packages-v1-hash", If-Modified-Since: 
[mock_server] 命中缓存条件，返回 304 Not Modified (0 字节负载)
```

### 4.2 url-proxy 终端日志
```text
[1] req: http://localhost:9994/conditional-test
[1] job leader: http://localhost:9994/conditional-test
[1] job finished: http://localhost:9994/conditional-test
[2] req: http://localhost:9994/conditional-test
[2] job leader: http://localhost:9994/conditional-test
[2] job not modified (304): http://localhost:9994/conditional-test
[2] 304 response sent: http://localhost:9994/conditional-test
```
日志清晰表明：代理成功捕获到上游的 304 信号，直接短路通知并写回 304 响应，完全跳过下载流程。

## 5. 结论

通过上述测试，验证了 `url-proxy` 具备完整的 HTTP 条件请求协商能力，能高效与 APT 的索引文件协商机制无缝配合，大幅提升重复 `apt update` 时的执行速度并节省网络资源。
