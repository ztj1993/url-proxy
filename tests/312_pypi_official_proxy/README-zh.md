# Feature 312: PyPI 官方镜像源全链路闭环代理测试指南

本文档介绍如何验证 `url-proxy` 对 Python 官方 PyPI 镜像源的全链路闭环代理与本地持久化缓存（Feature 312）。

> **说明**：本文档假定终端的**当前工作目录为本测试目录**（`tests/312_pypi_official_proxy/`）。

---

## 1. 测试原理与验证目标

Python 官方 PyPI 在设计上采用索引与数据分离架构：
- 索引入口：`https://pypi.org/simple/<pkg>/`
- 文件分发 CDN：`https://files.pythonhosted.org/packages/...`

当 `pip` 客户端发起依赖安装时，返回的 HTML / JSON 索引包含 `files.pythonhosted.org` 绝对链接。通过 Feature 312：
1. **PEP 503 (HTML) 动态重写**：将超链接中的 `https://files.pythonhosted.org/` 改写为代理自身的 `/pythonhosted/` 路径，同时完整保留 `#sha256=...` 包校验哈希；
2. **PEP 691 (JSON) 动态重写**：当请求头携带 `Accept: application/vnd.pypi.simple.v1+json` 时，返回的元数据 JSON 中 `files[].url` 同样被精准重写；
3. **分发包 100% 本地落盘缓存**：客户端紧接着向代理发起的 `/pythonhosted/packages/.../*.whl` 请求，安全进入 Single-Flight 并发合并并在本地安全目录落盘；
4. **二次下载 100% 缓存命中**：再次拉取同一 Wheel 安装包时，代理直接从本地磁盘响应（0 上游网络开销）。

---

## 2. 准备测试环境

### 步骤 2.1：启动模拟源站服务器
打开**终端 1**，在当前测试目录下运行：
```bash
go run mock_pypi_server.go
```
*预期输出*：`Mock PyPI and pythonhosted CDN server listening on :9997`

### 步骤 2.2：启动 url-proxy 服务
打开**终端 2**，指定缓存目录并加载本特性的测试配置：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_pypi.conf
```
*预期输出*：打印包含 `proxy config file loaded: ./proxy_pypi.conf`，并加载对应的 aliases 与 rewrite rules。

---

## 3. 执行测试步骤

打开**终端 3**，按序执行以下测试请求：

### 测试 1：PEP 503 HTML 索引超链接重写
```bash
curl -i http://localhost:8888/pypi/simple/requests/
```
*验证要点*：
- 响应头：`Content-Type: text/html; charset=utf-8`，且**原 ETag 已被剥离**；
- 响应体中的 Wheel 链接已改写为：
  `http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl#sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- 原始跨域域名 `https://files.pythonhosted.org/` 已被完全替换。

### 测试 2：PEP 691 JSON 元数据 API 重写
```bash
curl -i -H "Accept: application/vnd.pypi.simple.v1+json" http://localhost:8888/pypi/simple/requests/
```
*验证要点*：
- 响应头：`Content-Type: application/vnd.pypi.simple.v1+json`；
- JSON 响应体中的 `url` 字段已改写为：
  `"url": "http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl"`。

### 测试 3：首次下载 Wheel 包并验证本地落盘
```bash
curl -i http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl
```
*验证要点*：
- 返回 `200 OK` 及 Wheel 模拟二进制数据；
- 检查本地 `./test_cache/` 目录，确认已生成对应安全落盘文件：
  `./test_cache/http-127.0.0.1-9997/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl`。

### 测试 4：二次请求 100% 本地缓存命中
再次执行请求：
```bash
curl -i http://localhost:8888/pythonhosted/packages/a/b/c/requests-2.31.0-py3-none-any.whl
```
*验证要点*：
- 响应头部包含 `Accept-Ranges: bytes`；
- 终端 1（Mock 源站）**未产生任何新日志**，证明请求完全由代理服务直接读取本地磁盘响应。

---

## 4. 生产环境 pip 客户端使用验证

配置生产环境 `proxy.conf` 后，可使用真实 pip 客户端验证：
```bash
# 临时命令行指定代理镜像源
pip install requests --index-url http://127.0.0.1:8888/pypi/simple/ --trusted-host 127.0.0.1
```
观察 `url-proxy` 运行日志，即可确认所有 `.whl` 下载均被成功拘束在 `http://127.0.0.1:8888/pythonhosted/`，实现 100% 缓存加速！
