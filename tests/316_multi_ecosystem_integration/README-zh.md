# Feature 316: 多生态全链路端到端集成测试与预置模板

本目录提供 Feature 316（多生态全链路端到端集成测试与预置模板）的独立高仿真 Mock 集群、集成测试配置与自动化测试套件。

---

## 1. 目录结构

- `mock_multi_cluster.go`: 高仿真多源 Mock 服务集群，默认监听 `9990` 端口。单进程原生模拟 8 大生态官方上游（APT、Alpine、Go、Maven、PyPI、npm、Docker/OCI、Rust Cargo），内置精确的原子请求计数器与 `/stats` 监控端点。
- `proxy_integration.conf`: 专为集成测试设计的全生态统一配置文件，将 8 大生态路由精确重定向至本地 Mock 集群。
- `multi_cluster_test.go`: 验证 Mock 集群各协议端点契约与响应准确性的自动化测试套件。
- `README-zh.md`: 本说明文档（中文）。
- `README-en.md`: 英文说明文档。

---

## 2. 模拟的上游协议与端点一览

| 生态编号 | 目标生态 | 模拟官方上游端点 | 关键协议行为与测试点 |
| :---: | :--- | :--- | :--- |
| 1 | **APT** (Debian/Ubuntu) | `/ubuntu/dists/jammy/InRelease`<br>`/ubuntu/pool/main/h/hello/hello_2.10-2_amd64.deb` | `InRelease` 免缓存 (bypass) + 304 条件协商；`.deb` 强缓存与 Range 206 分片 |
| 2 | **Alpine** | `/alpine/v3.19/main/x86_64/APKINDEX.tar.gz`<br>`/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk` | `APKINDEX.tar.gz` 纯流式透传；`.apk` 强缓存与本地安全落盘 |
| 3 | **Go Modules** | `/goproxy/github.com/gin-gonic/gin/@v/list`<br>`.../@v/v1.9.1.mod`, `.info`, `.zip` | `/@v/list` 免缓存 (bypass)；`.mod` / `.info` / `.zip` 强缓存 |
| 4 | **Maven** | `/maven/org/example/demo/1.0.0/maven-metadata.xml`<br>`.../demo-1.0.0.pom`, `.../demo-1.0.0.jar` | `maven-metadata.xml` 动态透传；`.jar` / `.pom` 强缓存 |
| 5 | **PyPI** | `/pypi/simple/requests/`<br>`/pythonhosted/packages/requests-2.31.0-py3-none-any.whl` | HTML/JSON 索引动态重写（跨域 CDN 域名重定向到本地代理）；`.whl` 强缓存 |
| 6 | **npm** | `/npm/express`<br>`/npm/express/-/express-4.18.2.tgz` | package JSON 中 `dist.tarball` 动态重写；`.tgz` 强缓存 |
| 7 | **Docker / OCI** | `/docker/v2/`<br>`/docker/v2/library/nginx/manifests/latest`<br>`/docker/v2/library/nginx/blobs/sha256:...` | `/v2/` 401 Www-Authenticate 挑战；manifest 304 协商；Blob 强缓存与 Range 206 |
| 8 | **Rust Cargo** | `/crates-index/config.json`<br>`/crates-index/se/rd/serde`<br>`/crates-static/crates/serde/serde-1.0.197.crate` | `config.json` 动态重写 `dl` 端点；稀疏索引 304 协商；`.crate` 强缓存 |

---

## 3. 手工启动与联调测试

### 步骤 1：启动 Mock 集群
在终端 1 中运行：
```bash
go run tests/316_multi_ecosystem_integration/mock_multi_cluster.go -port 9990
```
控制台输出 `High-Fidelity Multi-Ecosystem Mock Cluster running on :9990` 即表示启动成功。

### 步骤 2：启动 url-proxy 测试实例
在终端 2 中运行：
```bash
go run url-proxy.go -conf tests/316_multi_ecosystem_integration/proxy_integration.conf -port 8888
```

### 步骤 3：验证各个生态代理表现

```bash
# 1. APT 验证
curl -i http://127.0.0.1:8888/ubuntu/dists/jammy/InRelease
curl -i http://127.0.0.1:8888/ubuntu/pool/main/h/hello/hello_2.10-2_amd64.deb

# 2. Alpine 验证
curl -i http://127.0.0.1:8888/alpine/v3.19/main/x86_64/APKINDEX.tar.gz
curl -i http://127.0.0.1:8888/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk

# 3. Go Modules 验证
curl -i http://127.0.0.1:8888/goproxy/github.com/gin-gonic/gin/@v/list
curl -i http://127.0.0.1:8888/goproxy/github.com/gin-gonic/gin/@v/v1.9.1.zip

# 4. Maven 验证
curl -i http://127.0.0.1:8888/maven/org/example/demo/1.0.0/maven-metadata.xml
curl -i http://127.0.0.1:8888/maven/org/example/demo/1.0.0/demo-1.0.0.jar

# 5. PyPI 动态重写验证
curl -i http://127.0.0.1:8888/pypi/simple/requests/

# 6. npm 动态重写验证
curl -i http://127.0.0.1:8888/npm/express

# 7. Docker Registry 握手与 Blob 验证
curl -i http://127.0.0.1:8888/docker/v2/
curl -i http://127.0.0.1:8888/docker/v2/library/nginx/blobs/sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

# 8. Cargo 动态重写验证
curl -i http://127.0.0.1:8888/crates-index/config.json
```

---

## 4. 自动化回归测试

运行本目录测试：
```bash
go test -v ./tests/316_multi_ecosystem_integration/...
```

运行全局端到端集成测试（包括单实例多生态并发无干扰压测）：
```bash
go test -v -run TestMultiEcosystemIntegrationPipeline .
```
