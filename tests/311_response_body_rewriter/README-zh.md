# Feature 311: 响应体流式动态重写测试指南

本文档介绍如何测试 `url-proxy` 的响应体流式动态重写引擎（Feature 311）。

> **说明**：本文档假定终端的**当前工作目录为本测试目录**（`tests/311_response_body_rewriter/`）。

---

## 1. 测试目标与原理

验证 `url-proxy` 在转发过程中对文本响应体执行毫秒级流式动态重写的核心能力：
1. **HTML 文本超链接重写**：针对 PyPI Simple 索引，将 `https://files.pythonhosted.org/` 替换为 `http://{PROXY_HOST}/pythonhosted/`，确保后续下载流转在代理内；
2. **JSON 元数据绝对链接重写**：针对 npm 元数据，将 `dist.tarball` 中的 `https://registry.npmjs.org/` 替换为 `http://{PROXY_HOST}/npm/`；
3. **MIME 类型过滤防误伤**：对二进制包（`application/octet-stream`）即使内容碰巧包含目标模式串，也严禁任何修改；
4. **Gzip 压缩响应自适应**：上游返回 Gzip 压缩文本时，透明解压、重写、修正 `Content-Length` 并剥离 `Content-Encoding`；
5. **协议头自适应修复**：剥离原上游 `ETag` 与 `Content-MD5`，自适应更新 `Content-Length`。

---

## 2. 准备测试环境

本测试目录下提供了以下组件：
- `mock_server.go`：模拟上游 PyPI/npm 响应的 Mock 服务（监听 `127.0.0.1:9998`）。
- `proxy_test.conf`：测试用统一配置文件（声明重写规则与短别名）。

### 步骤 2.1：启动模拟源站服务器
在当前测试目录打开**终端 1**，运行：
```bash
go run mock_server.go
```
*预期输出*：`mock server listening on 127.0.0.1:9998`

### 步骤 2.2：启动 url-proxy
在当前测试目录打开**终端 2**，指定缓存目录并加载测试配置：
```bash
go run ../../url-proxy.go -addr :8888 -cache ./test_cache -conf ./proxy_test.conf
```
*预期输出*：包含 `proxy config loaded from explicit CLI argument: ./proxy_test.conf`，并打印包含 `3 rewrite rules`。

---

## 3. 执行测试步骤

打开**终端 3**，按序执行以下测试请求：

### 测试 1：PyPI HTML 索引重写与 ETag 剥离
```bash
curl -i http://localhost:8888/test-pypi/simple/requests/
```
*验证要点*：
- 响应体中的链接已变为：`http://localhost:8888/pythonhosted/packages/a/b/requests-2.31.0.whl`；
- 响应头中**不存在 ETag**；
- 响应头中的 `Content-Length` 与实际返回字节数一致。

### 测试 2：npm JSON 元数据重写
```bash
curl -i http://localhost:8888/test-npm/pkg
```
*验证要点*：
- 返回的 JSON 中，`tarball` 地址已变为：`http://localhost:8888/npm/pkg/-/pkg-1.0.tgz`。

### 测试 3：二进制数据安全保护 (MIME 过滤)
```bash
curl -s http://localhost:8888/test-pypi/binary/test.whl
```
*验证要点*：
- 返回完整未被篡改的原始二进制内容：`RAW_BINARY_STREAM_WITH_https://files.pythonhosted.org/_KEEP_SAFE`。

### 测试 4：Gzip 响应解压与重写
```bash
curl -i http://localhost:8888/test-pypi/gzip/index.html
```
*验证要点*：
- 响应头中不含 `Content-Encoding: gzip`；
- 响应体为明文 HTML，且包含被重写的 `http://localhost:8888/pythonhosted/gz.whl`。
