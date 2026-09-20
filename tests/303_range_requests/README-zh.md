# Range 请求与 206 断点续传测试指南

本文档介绍如何测试 `url-proxy` 对 HTTP Range 请求（RFC 7233 / RFC 9110）及 `206 Partial Content` 断点续传特性的支持。

## 1. 测试原理

验证客户端通过代理发起带有切片范围（`Range: bytes=start-end`）的请求时：
1. **缓存未命中 (Cache Miss)**：
   - 代理如实将 `Range` 请求头透传给上游源站。
   - 源站返回 `HTTP/1.1 206 Partial Content`，代理将数据纯流式回传给客户端。
   - **关键安全验证**：该 206 局部切片绝不作为完整缓存文件写入磁盘，防止脏切片污染。
2. **缓存命中 (Cache Hit)**：
   - 在全量文件已缓存后，客户端再次发起 Range 切片请求。
   - 代理自动从本地缓存文件中毫秒级截取对应字节，直接返回 `206 Partial Content` 及 `Content-Range`，**无需向源站发起任何请求**。
3. **越界范围 (Out-of-Bounds)**：
   - 客户端请求超出文件大小的切片（如 `bytes=500-600`）时，正确返回 `HTTP/1.1 416 Requested Range Not Satisfiable`。
4. **正向代理模式兼容性**：
   - 结合功能 301，在 `curl -x` 正向代理模式下同样能精准获取 206 分片。

## 2. 准备测试环境

在当前测试目录下，提供了模拟源站程序 `mock_server.go`（监听 `:9993` 端口，预置 100 字节数据）。

### 步骤 2.1：启动模拟源站服务器
在当前测试目录打开终端，运行：
```bash
go run mock_server.go
```
*预期输出：`测试用 Range 断点续传源站已启动，监听端口 :9993`*

### 步骤 2.2：启动 url-proxy
打开**第二个**终端，进入当前测试目录后运行：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./cache
```
*预期输出：`http server: :8888`*

## 3. 执行测试

打开**第三个**终端，按顺序执行以下步骤：

### 阶段一：缓存未命中时的 Range 请求（纯流式透传）
向尚未缓存的资源请求前 50 字节（`0-49`）：
```bash
curl -i -r 0-49 http://localhost:8888/http://localhost:9993/range-test
```
*预期响应*：
```text
HTTP/1.1 206 Partial Content
Accept-Ranges: bytes
Content-Length: 50
Content-Range: bytes 0-49/100
...

01234567890123456789012345678901234567890123456789
```
*检查点*：检查 `./cache` 目录，确认**没有**生成任何损坏的 50 字节缓存文件。

---

### 阶段二：下载全量文件并建立缓存
发起全量 GET 请求：
```bash
curl -i http://localhost:8888/http://localhost:9993/range-test
```
*预期响应*：返回 `200 OK`，`Accept-Ranges: bytes`，`Content-Length: 100`，本地 `./cache` 成功生成 100 字节的完整缓存文件。

---

### 阶段三：缓存命中时的本地 Range 切片（秒级返回且零上游流量）
针对已缓存的完整文件，请求中间 20 字节（`10-29`）：
```bash
curl -i -r 10-29 http://localhost:8888/http://localhost:9993/range-test
```
*预期响应*：
```text
HTTP/1.1 206 Partial Content
Accept-Ranges: bytes
Content-Length: 20
Content-Range: bytes 10-29/100
...

01234567890123456789
```
*检查点*：观察 `mock_server` 终端，**没有任何新的请求日志**，证明完全由代理本地缓存直接高效切片响应。

---

### 阶段四：测试越界请求（416 状态码）
请求超出 100 字节的范围（`500-600`）：
```bash
curl -i -r 500-600 http://localhost:8888/http://localhost:9993/range-test
```
*预期响应*：
```text
HTTP/1.1 416 Requested Range Not Satisfiable
Content-Range: bytes */100
```

---

### 阶段五：正向代理模式下的 Range 断点续传
使用 `curl -x` 正向代理模式请求前 10 字节（`0-9`）：
```bash
curl -i -x http://localhost:8888 -r 0-9 http://localhost:9993/range-test
```
*预期响应*：正确返回 `HTTP/1.1 206 Partial Content`，`Content-Length: 10`。

## 4. 观察与日志验证

### url-proxy 日志输出：
```text
[1] req: http://localhost:9993/range-test
[1] range relay to upstream: http://localhost:9993/range-test (bytes=0-49)
[2] req: http://localhost:9993/range-test
[2] job leader: http://localhost:9993/range-test
[2] job finished: http://localhost:9993/range-test
[3] req: http://localhost:9993/range-test
[3] range hit local cache: http://localhost:9993/range-test (bytes=10-29)
```

## 5. 结论
`url-proxy` 完整具备 HTTP Range 请求与 206 Partial Content 断点续传能力，能有效为大文件断网恢复与多段并发切片拉取提供原生级底层支撑。
