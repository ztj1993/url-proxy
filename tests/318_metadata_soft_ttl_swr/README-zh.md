# Feature 318: 动态元数据软过期与 SWR 缓存测试套件

本目录提供 Feature 318（动态元数据软过期与 SWR 缓存机制，Bypass TTL 扩展）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 318 专用测试配置文件，声明挂载 `ttl:2s` 的 `bypass` 规则与常规无 TTL 的 `bypass` 对照规则。
- `mock_server.go`: 模拟上游 PyPI Simple、npm 包元数据及 APT 仓库元数据，支持 ETag 与 304 条件协商。
- `soft_ttl_test.go`: Feature 318 自动化测试套件。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/318_metadata_soft_ttl_swr/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/318_metadata_soft_ttl_swr/mock_server.go
```
服务将在 `127.0.0.1:9998` 监听。

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/318_metadata_soft_ttl_swr/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **PyPI 软过期与保鲜测试**：
   ```bash
   curl -i http://127.0.0.1:8888/pypi/simple/pkg/
   ```
   连续执行 10 次，查看响应头 `X-Cache-Status: HIT-SOFT-TTL`，上游回源计数保持为 1。
2. **过期后 304 静默续期**：
   等待 2 秒后再次请求，代理自动带 `If-None-Match` 发起条件查询，响应头返回 `X-Cache-Status: REVALIDATED-304`。
3. **查看回源统计**：
   ```bash
   curl http://127.0.0.1:9998/stats
   ```
