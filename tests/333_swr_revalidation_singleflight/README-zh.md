# Feature 333: 磁盘 TTL 资源并发刷新与请求合并测试套件

本目录提供 Feature 333（磁盘 TTL 资源并发刷新与请求合并方案，防过期击穿）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 333 专用测试配置文件，声明挂载 `ttl:10s` 的动态元数据免缓存规则。
- `mock_server.go`: 模拟带延迟的上游 APT InRelease 动态源站，用于精确捕捉高并发合并状态与回源次数。
- `revalidation_test.go`: 核心自动化测试套件，覆盖 30 并发 304 探测合一、30 并发 200 更新单流下载、Leader 中途断连隔离与源站故障并发 Stale-Fallback。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/333_swr_revalidation_singleflight/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/333_swr_revalidation_singleflight/mock_server.go
```
服务将在 `127.0.0.1:9997` 监听。

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/333_swr_revalidation_singleflight/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **高并发 304 请求合并验证**：
   使用并发压测工具（如 hey 或 ab）同时发起 30 个请求，上游计数严格仅增加 1 次。
2. **高并发 200 更新单流下载验证**：
   内容更新后并发请求，上游仅发生 1 次下载流，所有客户端均接收到最新数据。