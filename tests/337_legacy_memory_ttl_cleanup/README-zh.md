# Feature 337: 废弃内存软 TTL 机制清理与时效缓存收敛测试套件

本目录提供 Feature 337（废弃内存软 TTL 机制清理与时效缓存收敛方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 337 专用测试配置文件，声明带 TTL 的时效缓存与普通免缓存路由规则。
- `mock_server.go`: 模拟上游元数据服务，支持 304 条件协商与动态接口响应。
- `ttl_cleanup_test.go`: 核心自动化测试套件，验证精简依赖注入后的代理服务器正常运行、带 TTL 规则统一由磁盘时效缓存接管、普通免缓存纯流式透传不落盘及非 GET 请求安全穿透。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/337_legacy_memory_ttl_cleanup/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/337_legacy_memory_ttl_cleanup/mock_server.go -port 19337
```

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/337_legacy_memory_ttl_cleanup/proxy_test.conf -cache ./cache -addr :8888
```

### 步骤 3：验证带 TTL 资源的磁盘时效管线
```bash
# 首次请求，回源并落盘
curl -i http://localhost:8888/repo/InRelease
# 应返回 200 OK，包含 X-Cache-Status: MISS-DISK-TTL

# 保鲜期内二次请求
curl -i http://localhost:8888/repo/InRelease
# 应返回 200 OK，包含 X-Cache-Status: HIT-DISK-TTL
```
