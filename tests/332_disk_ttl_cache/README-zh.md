# Feature 332: 基于磁盘文件与 mtime 的时效缓存与条件更新测试套件

本目录提供 Feature 332（基于磁盘文件与 mtime 的时效缓存与条件更新方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 332 专用测试配置文件，声明挂载 `ttl:10s` 的动态元数据免缓存规则。
- `mock_server.go`: 模拟上游 APT InRelease 动态源站，支持 200 OK 内容更新、304 条件协商与 502 模拟故障。
- `disk_ttl_test.go`: 核心自动化测试套件，覆盖保鲜期命中、304 时间戳续期、内容更新原子覆盖、源站故障 Stale-Fallback 与重启持久化。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/332_disk_ttl_cache/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/332_disk_ttl_cache/mock_server.go
```
服务将在 `127.0.0.1:9996` 监听。

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/332_disk_ttl_cache/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **首次拉取与落盘验证**：
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9996/repo/InRelease
   ```
   查看响应头 `X-Cache-Status: MISS-DISK-TTL`，检查本地缓存目录确认生成磁盘文件。
2. **保鲜期命中测试**：
   在 10 秒内再次请求，查看响应头 `X-Cache-Status: HIT-DISK-TTL`，上游无回源请求。
3. **过期条件续期与容灾测试**：
   10 秒后请求，上游返回 304，响应头显示 `X-Cache-Status: REVALIDATED-304` 且文件修改时间自动刷新。