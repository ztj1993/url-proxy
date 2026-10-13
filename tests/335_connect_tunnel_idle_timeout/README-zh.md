# Feature 335: CONNECT 隧道空闲超时与半关闭回收测试套件

本目录提供 Feature 335（CONNECT 隧道滑动空闲超时自愈与 TCP 半关闭优雅回收方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 335 专用测试配置文件，声明基础服务监听与转发规则。
- `mock_server.go`: 模拟目标 TCP 服务端，支持标准双向 Echo、延迟响应以及单向半关闭（Half-Close）协议交互。
- `connect_tunnel_test.go`: 核心自动化测试套件，覆盖静默僵尸隧道超时自动回收、活跃传输动态刷新 Deadline、单向半关闭（CloseWrite）数据完整性接收。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/335_connect_tunnel_idle_timeout/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 目标服务
```bash
go run tests/335_connect_tunnel_idle_timeout/mock_server.go
```

### 步骤 2：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/335_connect_tunnel_idle_timeout/proxy_test.conf -addr :8888
```

### 步骤 3：验证场景
1. **CONNECT 隧道握手**：
   使用 curl 通过代理建立 HTTPS 隧道：
   ```bash
   curl -v -p -x http://127.0.0.1:8888 https://httpbin.org/get
   ```
2. **静默超时验证**：
   建立隧道后保持完全静默，观察代理在空闲超时后能够主动释放双向 Socket 连接与协程。