# Feature 338: 并发下载与时效缓存广播管线统合测试套件

本目录提供 Feature 338（并发下载与时效缓存广播管线统合方案）的本地独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 338 专用测试配置文件，声明静态规则与 `ttl:5s` 的动态元数据规则。
- `mock_server.go`: 模拟上游源站（支持静态二进制包、动态时效元数据 304/500/200 及 404 错误）。
- `unified_pipeline_test.go`: 核心自动化测试套件，全面覆盖静态包单飞并发合并、时效资源并发 304 续期广播共享、上游 500 故障 STALE-FALLBACK 降级共享及非 200 零落盘防污染。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/338_unified_download_pipeline/...
```

---

## 3. 测试覆盖场景

1. **静态二进制制品单飞并发下载合并 (`TestStaticDownloadCoalescing`)**：
   - 20 个客户端并发拉取同一静态文件，上游源站严格仅记录 1 次请求；
   - 20 个客户端均完整接收数据，临时文件安全原子重命名落盘。
2. **动态元数据并发 304 续期广播共享 (`TestDiskTTLRevalidationCoalescing`)**：
   - 25 个并发请求已过期的动态元数据，上游仅收到 1 次条件重验证；
   - 共享 304 结果并刷新本地文件 `mtime`，所有客户端均收到带有 `REVALIDATED-304` 标头的 200 响应。
3. **上游 500 故障时的旧文件降级容灾 (`TestDiskTTLStaleFallback`)**：
   - 15 个并发请求遇到上游 500 故障，平稳触发 `STALE-FALLBACK` 广播共享并返回本地旧缓存。
4. **非 200 纯流式透传与零落盘防污染 (`TestNon200PassthroughAntiPollution`)**：
   - 针对 404 响应流式透传，验证磁盘零落盘、零临时文件残留。
