# 可配置免缓存规则与防误杀机制测试指南

本文档介绍如何测试 `url-proxy` 的可配置免缓存规则与防误杀白名单机制（Feature 305）。

> **说明**：本文档所有操作步骤均假定终端的**当前工作目录为本测试目录**（`tests/305_selective_cache/`）。

---

## 1. 测试目标与原理

验证 `url-proxy` 结合统一配置文件 `proxy.conf` 的动静分离与防误杀能力：
1. **动态元数据免缓存 (Bypass)**：请求命中 `bypass /dists/ exact InRelease` 时，执行实时流式透传，本地磁盘缓存中**绝不创建任何文件**。
2. **白名单防误杀强制落盘 (Cache)**：请求 `/pool/main/p/packages-base/packages-base_1.0_amd64.deb`，虽然名称包含 `packages` 且配置中存在 `bypass /dists/ prefix Packages`，但由于命中了 `cache * suffix .deb`，享有最高优先级，**100% 成功落盘缓存**。
3. **上下文作用域隔离 (Scoping)**：请求 `/pool/main/r/release-info/Release`，虽然文件名为 `Release`，但因其不在 `/dists/` 前缀下，不会触发 bypass，**正常落盘缓存**。
4. **条件协商透传**：针对免缓存资源发起 `If-None-Match` 请求，验证上游返回 304 时代理准确短路透传 304。

---

## 2. 准备测试环境

本测试目录下提供了以下组件：
- `mock_server.go`：模拟 APT 源站服务器（监听 `127.0.0.1:9995` 端口）。
- `proxy_test.conf`：测试用统一配置文件。

### 步骤 2.1：启动模拟源站服务器
在当前测试目录打开**终端 1**，运行：
```bash
go run mock_server.go
```
*预期输出*：`mock server listening on 127.0.0.1:9995`

### 步骤 2.2：启动 url-proxy
在当前测试目录打开**终端 2**，指定缓存目录为 `./test_cache` 并加载测试配置：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*预期输出*：包含 `proxy config loaded from explicit CLI argument: ./proxy_test.conf` 及 `http server: :8888`。

---

## 3. 执行测试步骤

打开**终端 3**，按序执行以下请求验证：

### 测试阶段一：动态元数据免缓存与实时性
连续两次请求动态 `InRelease`：
```bash
curl -s http://localhost:8888/http://127.0.0.1:9995/dists/jammy/InRelease
curl -s http://localhost:8888/http://127.0.0.1:9995/dists/jammy/InRelease
```
*观察与验证*：
1. 两次响应中的 `Sequence` 分别递增为 1 和 2，内容完全实时；
2. 检查 `./test_cache` 目录，确认**没有任何 InRelease 缓存文件生成**（0 字节落地）。

### 测试阶段二：白名单防误杀强制落盘
请求名称包含 `packages` 敏感词的真实软件包：
```bash
curl -i http://localhost:8888/http://127.0.0.1:9995/pool/main/p/packages-base/packages-base_1.0_amd64.deb
```
*观察与验证*：
1. 响应返回 `HTTP/1.1 200 OK` 及 `Accept-Ranges: bytes`；
2. 检查 `./test_cache` 目录，确认该 `.deb` 文件已**成功落盘缓存**。

### 测试阶段三：上下文路径作用域隔离
请求位于 `/pool/` 下的普通说明文件 `Release`：
```bash
curl -i http://localhost:8888/http://127.0.0.1:9995/pool/main/r/release-info/Release
```
*观察与验证*：
1. 该文件虽名为 `Release`，但不在 `/dists/` 作用域内；
2. 检查 `./test_cache` 目录，确认该文件**成功落盘缓存**。

### 测试阶段四：免缓存资源的 304 条件协商
携带 ETag 发起协商请求：
```bash
curl -i -H 'If-None-Match: "v1.0"' http://localhost:8888/http://127.0.0.1:9995/dists/jammy/InRelease
```
*预期响应*：
```text
HTTP/1.1 304 Not Modified
ETag: "v1.0"
...
```
*验证结论*：上游 304 快速协商被代理实时透明短路透传给客户端。

---

## 4. 总结
本套件全面验证了配置化免缓存流式透传、白名单强制落盘防误杀、上下文作用域隔离以及透明 304 协商能力。
