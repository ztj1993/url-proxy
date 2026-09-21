# 路径短别名路由与动态重写测试指南

本文档介绍如何测试 `url-proxy` 的路径短别名路由与动态重写能力（Feature 306）。

> **说明**：本文档所有操作步骤均假定终端的**当前工作目录为本测试目录**（`tests/306_path_alias/`）。

---

## 1. 测试目标与原理

验证 `url-proxy` 结合统一配置文件 `proxy.conf` 的路径别名重写与多管线联动能力：
1. **短别名动态重写与免缓存中继**：客户端请求 `http://localhost:8888/my-ubuntu/dists/jammy/InRelease`，代理自动重写为 `http://127.0.0.1:9996/upstream/ubuntu/dists/jammy/InRelease` 并执行实时流式透传，本地磁盘**绝不落盘（0 字节）**。
2. **短别名重写与条件 304 快速协商**：携带 `If-None-Match: "v1.0"` 请求短别名元数据，代理准确短路透传上游 `304 Not Modified`。
3. **短别名重写与安装包落盘缓存**：客户端请求 `http://localhost:8888/my-ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb`，代理重写并拉取上游包文件，**100% 成功落盘缓存至本地磁盘**。
4. **多别名独立路由隔离**：请求 `http://localhost:8888/my-debian/dists/bookworm/Release`，准确重写至 Debian 上游端点。
5. **未定义别名错误防御**：请求未定义的别名（如 `/invalid-alias/foo`），服务端返回 `404 Not Found`。

---

## 2. 准备测试环境

本测试目录下提供了以下组件：
- `mock_server.go`：模拟上游镜像源服务器（监听 `127.0.0.1:9996` 端口）。
- `proxy_test.conf`：测试用统一配置文件（声明了 `my-ubuntu` 与 `my-debian` 短别名）。

### 步骤 2.1：启动模拟源站服务器
在当前测试目录打开**终端 1**，运行：
```bash
go run mock_server.go
```
*预期输出*：`mock server listening on 127.0.0.1:9996`

### 步骤 2.2：启动 url-proxy
在当前测试目录打开**终端 2**，指定缓存目录为 `./test_cache` 并加载测试配置：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*预期输出*：包含 `proxy config loaded from explicit CLI argument: ./proxy_test.conf` 及 `http server: :8888`。

---

## 3. 执行测试步骤

打开**终端 3**，按序执行以下请求验证：

### 测试阶段一：短别名重写与免缓存元数据
连续两次请求短别名动态 `InRelease`：
```bash
curl -s http://localhost:8888/my-ubuntu/dists/jammy/InRelease
curl -s http://localhost:8888/my-ubuntu/dists/jammy/InRelease
```
*观察与验证*：
1. 两次响应中的 `Sequence` 分别递增为 1 和 2，证明动态透传成功；
2. 检查 `./test_cache` 目录，确认**没有任何 InRelease 缓存文件生成**（0 字节落地）。

### 测试阶段二：短别名 304 条件协商
携带 ETag 发起协商请求：
```bash
curl -i -H 'If-None-Match: "v1.0"' http://localhost:8888/my-ubuntu/dists/jammy/InRelease
```
*预期响应*：
```text
HTTP/1.1 304 Not Modified
ETag: "v1.0"
```
*验证结论*：上游 304 快速协商被代理实时透明短路透传给客户端。

### 测试阶段三：短别名安装包落盘缓存
请求安装包：
```bash
curl -i http://localhost:8888/my-ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb
```
*观察与验证*：
1. 响应返回 `HTTP/1.1 200 OK` 及二进制内容；
2. 检查 `./test_cache` 目录，确认该 `.deb` 文件已**成功落盘缓存**。

### 测试阶段四：多别名隔离验证
请求另一别名定义的 Debian 元数据：
```bash
curl -s http://localhost:8888/my-debian/dists/bookworm/Release
```
*观察与验证*：
1. 响应返回 `Origin: Debian`，证明准确命中对应上游；
2. 因命中 `exact Release` 免缓存规则，本地磁盘零落盘。

### 测试阶段五：未知别名防御
请求未配置的别名：
```bash
curl -i http://localhost:8888/not-exist-alias/some/file
```
*预期响应*：
```text
HTTP/1.1 404 Not Found
```

---

## 4. 总结
本套件全面验证了路径短别名路由解析、动态 URL 规范化重写、与免缓存/落盘缓存的无缝联动，以及未知别名安全阻断。
