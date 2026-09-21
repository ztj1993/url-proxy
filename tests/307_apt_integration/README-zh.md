# APT 代理全链路端到端集成验证测试指南

本文档介绍如何对 `url-proxy` 进行 APT 代理全链路端到端集成验证（Feature 307）。

> **说明**：本文档所有操作步骤均假定终端的**当前工作目录为本测试目录**（`tests/307_apt_integration/`）。

---

## 1. 验证目标与全链路流水线

本测试套件将前期实现的 6 项核心能力进行全链路综合集成检验：
1. **冷启动 `apt update`（动态元数据直通）**：拉取 `InRelease` 与 `Packages.xz`，验证动态更新且本地磁盘 0 字节写入；
2. **热协商 `apt update`（304 极速短路）**：携带 ETag 发起更新检查，验证上游 304 零负载透传；
3. **冷启动 `apt install`（首节点拉取落盘）**：首个客户端拉取 `hello.deb`，验证上游产生 1 次下载且安全落盘；
4. **热缓存 `apt install`（次节点本地秒发）**：后续客户端拉取相同安装包，验证上游下载计数不变（100% 本地缓存命中）；
5. **跨模式缓存共用**：通过短别名（`/ubuntu/...`）与通过标准正向代理（`archive.ubuntu.com`）访问同一资源，共享同一份本地磁盘缓存；
6. **Range 断点续传**：断点拉取大包切片，验证 `206 Partial Content` 状态与分块字节；
7. **白名单防误杀**：验证包含 `packages` 敏感词的软件包 100% 成功落盘。

---

## 2. 准备测试环境

本测试目录下提供了以下组件：
- `mock_apt_repo.go`：高仿真 APT 镜像服务器（监听 `127.0.0.1:9997`），内置实时指标审计端点 `/stats`。
- `proxy_test.conf`：全特性生产级测试配置文件。

### 步骤 2.1：启动模拟 APT 仓库服务器
在当前测试目录打开**终端 1**，运行：
```bash
go run mock_apt_repo.go
```
*预期输出*：`mock APT repository server listening on 127.0.0.1:9997`

### 步骤 2.2：启动 url-proxy
在当前测试目录打开**终端 2**，指定缓存目录为 `./test_cache` 并加载测试配置：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*预期输出*：包含 `proxy configuration summary` 及 `http server: :8888`。

---

## 3. 执行测试步骤

打开**终端 3**，按序执行以下请求验证：

### 阶段一：仿真 `apt update`（冷拉取与热 304）
1. **冷更新**：拉取元数据
   ```bash
   curl -s http://localhost:8888/ubuntu/dists/jammy/InRelease
   curl -s http://localhost:8888/ubuntu/dists/jammy/main/binary-amd64/Packages.xz >/dev/null
   ```
   *验证*：检查 `./test_cache`，确认元数据文件均**未在本地落盘**。
2. **热更新（304 协商）**：携带 ETag 再次发起请求
   ```bash
   curl -i -H 'If-None-Match: "inrelease-v1.0"' http://localhost:8888/ubuntu/dists/jammy/InRelease
   ```
   *预期响应*：返回 `HTTP/1.1 304 Not Modified`，上游传输负载为 0。

### 阶段二：仿真 `apt install`（冷落盘与热命中）
1. **首节点安装（冷拉取）**：
   ```bash
   curl -i http://localhost:8888/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb -o hello_client1.deb
   ```
   *验证*：检查 `./test_cache`，确认 `hello_2.10-2ubuntu2_amd64.deb` **成功完整落盘**。
2. **次节点安装（热命中）**：
   ```bash
   curl -i http://localhost:8888/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb -o hello_client2.deb
   ```
   *验证*：通过审计端点确认上游下载次数依然保持为 1，完全由本地缓存供给：
   ```bash
   curl -s http://127.0.0.1:9997/stats
   ```

### 阶段三：跨接入模式共享同一缓存
通过标准正向代理模式请求官方原生域名：
```bash
curl -x http://localhost:8888 -s http://archive.ubuntu.com/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb -o hello_client3.deb
```
*验证*：直接命中阶段二生成的缓存，上游下载计数不递增。

### 阶段四：Range 切片断点续传
请求前 500 字节：
```bash
curl -i -r 0-499 http://localhost:8888/ubuntu/pool/main/h/hello/hello_2.10-2ubuntu2_amd64.deb
```
*预期响应*：返回 `HTTP/1.1 206 Partial Content` 及 `Content-Range: bytes 0-499/10240`。

### 阶段五：白名单防误杀验证
拉取包含 `packages` 敏感词的软件包：
```bash
curl -i http://localhost:8888/ubuntu/pool/main/p/packages-tool/packages-tool_1.0_amd64.deb
```
*验证*：检查 `./test_cache`，确认该包在白名单最高优先级保护下**100% 安全落盘**。

---

## 4. 总结与指标审计
访问 `http://127.0.0.1:9997/stats` 审查整体流量消耗，全面验证了免缓存防陈旧、304 快速短路、安装包高命中率与跨模式缓存复用。
