# 统一代理配置文件系统测试指南

本文档介绍如何测试 `url-proxy` 的统一代理配置文件系统（Unified Proxy Configuration System）。

> **说明**：本文档所有操作步骤均假定终端的**当前工作目录为本测试目录**（`tests/304_unified_config/`）。

---

## 1. 测试目标与原理

验证 `url-proxy` 的统一配置加载与多级探测机制：
1. **多指令统一解析**：能够准确解析 `alias`、`forward`、`bypass`、`cache` 单行指令。
2. **命令行显式指定 (`-conf`)**：通过 `-conf proxy_test.conf` 加载自定义配置文件，并成功驱动域名转发重定向。
3. **工作目录自动探测**：当工作目录下存在 `proxy.conf` 时，无需 `-conf` 参数即可自动识别并加载。

---

## 2. 准备测试环境

本测试目录下提供了以下组件：
- `mock_server.go`：模拟镜像网关服务器（监听 `127.0.0.1:9994` 端口）。
- `proxy_test.conf`：测试用统一配置文件。

### 步骤 2.1：启动模拟镜像网关服务器
在当前测试目录打开**终端 1**，运行：
```bash
go run mock_server.go
```
*预期输出*：`mock server listening on :9994`

---

## 3. 执行测试步骤

### 测试场景 A：通过 `-conf` 命令行参数加载指定配置
打开**终端 2**，在当前测试目录下运行 `url-proxy` 并指定 `-conf`：
```bash
go run ../../url-proxy.go -addr :8888 -conf ./proxy_test.conf
```
*观察终端 2 日志*：
```text
proxy config loaded from explicit CLI argument: ./proxy_test.conf
proxy configuration summary [./proxy_test.conf]: 2 aliases, 2 forwards, 4 cache/bypass rules
http server: :8888
```

打开**终端 3**，发起请求触发 `forward` 规则引流：
```bash
curl -i http://localhost:8888/http://example.internal/test/package.deb
```
*预期响应*：
```text
HTTP/1.1 200 OK
Content-Type: text/plain
X-Forwarded-Mock: true

unified proxy forwarding test passed: URI=/mirror/http://example.internal/test/package.deb
```
*验证结论*：通过 `-conf` 加载的配置成功驱动代理引流至 mock_server。按 `Ctrl+C` 停止终端 2 的代理服务。

---

### 测试场景 B：当前工作目录自动探测 `proxy.conf`
在当前测试目录下将测试配置复制为标准的 `proxy.conf`：
```bash
cp proxy_test.conf proxy.conf
```
在终端 2 启动 `url-proxy`（不带 `-conf` 参数）：
```bash
go run ../../url-proxy.go -addr :8888
```
*观察终端 2 日志*：
```text
proxy config file loaded: proxy.conf
proxy configuration summary [proxy.conf]: 2 aliases, 2 forwards, 4 cache/bypass rules
http server: :8888
```
清理临时文件并按 `Ctrl+C` 停止代理：
```bash
rm proxy.conf
```
*验证结论*：系统准确在当前工作目录下探测并自动加载了 `proxy.conf`。

---

## 4. 总结
本套件全面覆盖了统一配置文件的解析能力、命令行覆盖能力与就地探测能力。
