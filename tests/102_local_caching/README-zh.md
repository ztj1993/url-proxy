# 本地文件缓存机制测试指南

本文档介绍如何测试 `url-proxy` 的本地文件缓存（Local File Caching）功能。

## 1. 测试原理

验证 `url-proxy` 的首次回源缓存与二次命中机制：
1. **首次请求（Cache Miss）**：代理向上游服务器请求资源，将数据流式返回给客户端并落盘保存至 `-cache` 目录。
2. **二次请求（Cache Hit）**：客户端再次请求相同 URL 时，代理检测到本地缓存完整存在，直接从本地磁盘读取并返回，**不再向上游服务器发起任何请求**。

## 2. 准备测试环境

在当前目录下，我们提供了一个简单的 Go 程序 `caching_server.go` 来模拟上游服务器并统计请求次数。

### 步骤 2.1：启动模拟上游服务器
打开一个终端，运行以下命令启动服务器：
```bash
go run caching_server.go
```
*预期输出：`测试用缓存验证上游服务器已启动，监听端口 :9992`*

### 步骤 2.2：启动 url-proxy（指定专属测试缓存目录）
打开**第二个**终端，在项目根目录下运行（可自定义一个临时的本地测试缓存目录，如 `./.test_cache`）：
```bash
mkdir -p .test_cache
go run url-proxy.go -addr :8888 -cache ./.test_cache
```
*预期输出：`cache dir: ./.test_cache`*

## 3. 执行测试

打开**第三个**终端，依次执行两次相同的下载请求：

1. **第一次请求（触发上游下载与缓存生成）**：
   ```bash
   curl -i http://localhost:8888/http://localhost:9992/cache-file
   ```

2. **第二次请求（触发本地缓存命中）**：
   ```bash
   curl -i http://localhost:8888/http://localhost:9992/cache-file
   ```

## 4. 观察与验证

### 4.1 上游服务器终端验证（核心验证）
观察 `caching_server` 终端日志：
- 第一次请求时输出：
  ```text
  [...] [上游服务器] 收到下载请求 #1
  ```
- 第二次请求时：**无任何新日志输出**。
- 可通过 curl 检查上游总请求数：
  ```bash
  curl http://localhost:9992/stats
  # 输出: upstream_requests: 1
  ```
  证明第二次请求完全由代理本地缓存提供，未产生任何上游流量。

### 4.2 url-proxy 终端日志验证
观察代理输出：
```text
# 第一次请求（下载并落盘）
[1] req: http://localhost:9992/cache-file
[1] job leader: http://localhost:9992/cache-file
[1] job download: http://localhost:9992/cache-file
[1] job finished: http://localhost:9992/cache-file
[1] job renamed successfully: ...

# 第二次请求（直接命中缓存）
[2] req: http://localhost:9992/cache-file
[2] job leader: http://localhost:9992/cache-file
[2] job cached: http://localhost:9992/cache-file
```

### 4.3 缓存目录文件检查
检查 `.test_cache` 目录，确认缓存文件已正确落盘且大小完整：
```bash
ls -la .test_cache/http-localhost-9992/
```

## 5. 结论
通过上述测试，您可以验证 `url-proxy` 能够准确识别并利用本地磁盘缓存，有效屏蔽后续重复请求。
