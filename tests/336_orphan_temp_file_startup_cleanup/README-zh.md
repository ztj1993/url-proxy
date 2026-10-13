# Feature 336: 启动时孤儿临时文件扫描与自动清理测试套件

本目录提供 Feature 336（启动时孤儿临时文件扫描与自愈清理方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 336 专用测试配置文件，声明基础服务监听与转发规则。
- `mock_server.go`: 模拟上游软件包下载源站。
- `orphan_cleanup_test.go`: 核心自动化测试套件，覆盖临时文件特征命名精准识别（防误判）、历史过期孤儿文件自动清除、活跃临时文件与正常缓存文件安全保护。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/336_orphan_temp_file_startup_cleanup/...
```

---

## 3. 手工启动与验证

### 步骤 1：启动 Mock 上游服务
```bash
go run tests/336_orphan_temp_file_startup_cleanup/mock_server.go
```

### 步骤 2：构造历史孤儿文件
在本地缓存目录下创建测试临时文件，并修改其时间戳为 2 小时前：
```bash
touch cache/test.deb.1700000000000000000
touch -d "2 hours ago" cache/test.deb.1700000000000000000
```

### 步骤 3：启动 url-proxy 实例
```bash
go run url-proxy.go -conf tests/336_orphan_temp_file_startup_cleanup/proxy_test.conf -cache ./cache -addr :8888
```
观察控制台启动日志，确认出现 `orphan temp file cleanup completed: removed 1 stale files`，且孤儿文件被成功移除。