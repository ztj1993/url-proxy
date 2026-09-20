# 跨平台安全缓存路径机制测试指南

本文档介绍如何测试 `url-proxy` 的安全本地缓存路径生成机制（Safe Cache Path Generation）。

## 1. 测试原理

验证 `url-proxy` 的 `getSafeUrlPath` 函数对复杂 URL 的本地文件系统映射：
1. **端口冒号替换**：目标 URL 为 `http://localhost:9995/...`，生成的缓存第一级目录为 `http-localhost-9995`，替换冒号以兼容 Windows 文件系统。
2. **Query 参数隔离与编码**：针对同一接口 `/api/item?v=1` 和 `/api/item?v=2` 分别发起请求，应生成互不干扰的独立缓存文件（如 `item%3Fv%3D1` 与 `item%3Fv%3D2`）。
3. **斜杠结尾归一化**：针对 `/folder/` 请求，应保存为名为 `index` 的本地文件。

## 2. 准备测试环境

在当前目录下，我们提供了一个简单的 Go 程序 `path_test_server.go`。

### 步骤 2.1：启动模拟上游服务器
打开一个终端，运行以下命令启动服务器：
```bash
go run path_test_server.go
```
*预期输出：`测试用安全路径验证服务器已启动，监听端口 :9995`*

### 步骤 2.2：启动 url-proxy（指定专属测试缓存目录）
打开**第二个**终端，在项目根目录下运行：
```bash
mkdir -p .test_path_cache
go run url-proxy.go -addr :8888 -cache ./.test_path_cache
```
*预期输出：`cache dir: ./.test_path_cache`*

## 3. 执行测试

打开**第三个**终端，依次执行以下三组不同特征的请求：

1. **测试带参请求 1**：
   ```bash
   curl -s http://localhost:8888/http://localhost:9995/api/item?v=1
   ```

2. **测试带参请求 2（同路径不同参）**：
   ```bash
   curl -s http://localhost:8888/http://localhost:9995/api/item?v=2
   ```

3. **测试目录结尾请求**：
   ```bash
   curl -s http://localhost:8888/http://localhost:9995/folder/
   ```

## 4. 观察与验证

检查 `.test_path_cache` 目录下的生成结构：
```bash
# 查看生成的缓存目录层级与文件名
find .test_path_cache -type f
```

### 预期文件结构：
```text
.test_path_cache/http-localhost-9995/api/item%3Fv%3D1
.test_path_cache/http-localhost-9995/api/item%3Fv%3D2
.test_path_cache/http-localhost-9995/folder/index
```

*验证要点*：
1. **Windows 兼容性**：根级目录名为 `http-localhost-9995`，端口前的冒号 `:` 被成功替换为短横线 `-`，在 Windows 系统上不会抛出路径非法异常。
2. **参数独立隔离**：`item?v=1` 和 `item?v=2` 分别对应独立的本地文件，内容互不污染。
3. **缺省文件名处理**：`/folder/` 请求被正确命名为 `index` 文件。

## 5. 结论
通过上述测试，您可以验证 `url-proxy` 的安全缓存路径机制在跨平台、多参数及特殊 URL 场景下均能稳定可靠地进行文件存储与隔离。
