# Feature 334: 上游 HTTP 连接池调优与超时加固测试套件

本目录提供 Feature 334（上游 HTTP 连接池调优与超时加固方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 334 专用测试配置文件，声明基础服务监听与转发规则。
- `mock_server.go`: 模拟上游慢速响应头及大体积流式正文服务。
- `transport_tuning_test.go`: 核心自动化测试套件，覆盖连接池参数初始化校验、上游响应头假死超时（ResponseHeaderTimeout）中断、大体积流式传输不被误杀保护。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/334_upstream_http_transport_tuning/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/334_upstream_http_transport_tuning/mock_server.go
```
服务将在 `127.0.0.1:9998` 监听。

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/334_upstream_http_transport_tuning/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **假死响应头超时验证**：
   请求 `http://127.0.0.1:8888/http://127.0.0.1:9998/slow-header`，验证代理在响应头超时时能够主动断开连接返回错误，杜绝协程挂死。
2. **正常长连接复用验证**：
   高频发起请求，通过 netstat 或 ss 命令观察连接复用情况，确认无大量临时端口 TIME_WAIT 积压。