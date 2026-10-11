# Feature 317: 路由作用域插件化与 Docker 分层缓存解耦测试套件

本目录提供 Feature 317（路由作用域插件化与 Docker 分层缓存解耦）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 317 专用测试代理配置文件，显式为 `docker` 和 `v2` 别名挂载 `plugin:docker`，并配置普通隔离别名 `plain` 和 `generic`。
- `mock_server.go`: 模拟上游服务，分别提供 Docker Registry v2 握手与分层响应，以及普通 `plain` 路由响应，并记录各端点的回源命中统计。
- `scoped_plugins_test.go`: Feature 317 自动化测试套件。覆盖 Docker 插件正向直出、未绑定插件的负向作用域隔离、以及配置文件修饰符语法解析。
- `README-zh.md`: 本说明文档（中文）。
- `README-en.md`: 英文说明文档。

---

## 2. 自动化测试执行

在项目根目录下运行如下命令执行 Feature 317 全量自动化测试：

```bash
go test -v -count=1 ./tests/317_scoped_route_plugins/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/317_scoped_route_plugins/mock_server.go
```
服务将在 `127.0.0.1:9997` 监听。

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/317_scoped_route_plugins/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **正向 Docker 插件路由**：
   ```bash
   curl -i http://127.0.0.1:8888/docker/v2/
   curl -i http://127.0.0.1:8888/docker/v2/library/test/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
   ```
2. **负向非 Docker 作用域隔离**：
   ```bash
   curl -i http://127.0.0.1:8888/plain/repo/blobs/sha256:7b1a8c9e1234567890abcdef1234567890abcdef1234567890abcdef12345678
   ```
   验证其返回 `plain-non-docker-payload-data-9876543210`，绝不触发 Docker 插件直出。
3. **查看统计指标**：
   ```bash
   curl http://127.0.0.1:9997/stats
   ```
