# Feature 331: 非 200 响应脏缓存防污染与纯流式透传测试套件

本目录提供 Feature 331（非 200 响应脏缓存防污染与纯流式透传方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 331 专用测试配置文件，声明基础服务监听、缓存路径及路由转发规则。
- `mock_server.go`: 模拟上游源站服务，支持返回 404 Not Found、500 Internal Server Error、502 Bad Gateway 以及 200 OK 有效制品。
- `anti_pollution_test.go`: Feature 331 核心自动化测试套件，覆盖 404 零落盘、500/502 故障隔离与恢复、200 OK 正常落盘及并发请求安全透传。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/331_negative_caching_anti_pollution/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/331_negative_caching_anti_pollution/mock_server.go
```
服务将在 `127.0.0.1:9995` 监听。

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/331_negative_caching_anti_pollution/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **404 响应零落盘验证**：
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9995/repo/not-found.deb
   ```
   验证客户端收到 HTTP 404 状态码及错误正文，检查本地缓存目录确认无任何文件创建。
2. **500 故障与恢复验证**：
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9995/repo/server-error.deb
   ```
   验证客户端如实收到 500 状态码，本地无缓存文件残留。
3. **200 OK 正常制品下载与缓存验证**：
   ```bash
   curl -i http://127.0.0.1:8888/http://127.0.0.1:9995/repo/valid-pkg.deb
   ```
   验证客户端完整接收有效二进制制品包，本地缓存成功落盘且内容哈希匹配。