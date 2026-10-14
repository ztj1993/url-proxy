# Docker Compose 核心功能与多生态全特性 E2E 端到端自动化测试套件

本目录提供 `url-proxy` 的 Docker Compose 端到端自动化测试套件（E2E Suite），包含测试镜像构建定义、容器网络编排、配置文件、测试用例调度及控制台格式化报告审计逻辑。

---

## 1. 概述与测试目标

测试套件旨在通过真实 Docker Compose 容器网络环境，针对真实公网源站全面验证 `url-proxy` 在各大语言包管理器与底层操作系统生态下的代理转发、响应体重写、断点续传、条件短路及磁盘落盘缓存能力：

1. **容器化独立隔离**：测试环境完全基于独立的 `tests/e2e/Dockerfile` 打包，下载缓存全部收敛在 `url-proxy` 容器内部 `/cache` 目录中，不与宿主机产生 bind mount 挂载污染；
2. **多生态双模式全覆盖**：覆盖 APT、Alpine、PyPI、npm、Go 5 大主流生态的**路径前缀/别名模式（Prefix Mode）**与**标准 HTTP 正向代理模式（HTTP Proxy Mode）**；
3. **高级特性端到端验证**：支持 Maven Central Jar 包代理、Rust Cargo 稀疏索引重写与源码包代理、HTTP Range 206 切片分片续传以及 HTTP 304 条件协商短路；
4. **独立用例防干扰**：各生态的双接入模式采用互不重合的轻量依赖包（如 `tree` vs `ed`），确保每项测试均为独立的冷拉取落盘；
5. **格式化终端报告**：测试完成后在控制台自动打印纯文本 ASCII 测试看板，精准审计用例耗时、落盘/隧道状态及通过结论。

---

## 2. 架构设计与接入矩阵

```text
                                  [客户端接入层]
                                        |
        +-------------------------------+-------------------------------+
        |                                                               |
  模式一: 路径前缀/别名模式                                       模式二: 标准 HTTP 正向代理模式
(如 GET /ubuntu/pool/... HTTP/1.1)                             (如 GET http://archive.ubuntu.com/pool/... HTTP/1.1)
        |                                                               |
        +-------------------------------+-------------------------------+
                                        |
                                        v
                           +-------------------------+
                           | url-proxy 测试容器服务  |
                           | 端口: 8888              |
                           | 配置文件: proxy.conf    |
                           | 内部缓存: /cache       |
                           +-------------------------+
                                        |
                                        v (回源与缓存共用)
            [公网源站: PyPI / APT / Alpine / npm / Go / Maven / Cargo]
```

---

## 3. 全量 14 项 E2E 测试矩阵

| 用例编号 | 生态/特性 | 测试模式/特性 | 核心测试命令 | 预期结论与断言 |
| :---: | :---: | :---: | :--- | :--- |
| 1 | **APT** | 路径前缀模式 | `apt download tree` | `.deb cached (tree)` |
| 2 | **APT** | 标准 HTTP 代理 | `apt download ed` | `.deb cached (ed)` |
| 3 | **Alpine** | 路径前缀模式 | `apk fetch tree` | `.apk cached (tree)` |
| 4 | **Alpine** | 标准 HTTP 代理 | `apk fetch sed` | `.apk cached (sed)` |
| 5 | **PyPI** | 路径前缀模式 | `pip install six` | `.whl cached (six)` |
| 6 | **PyPI** | 标准 HTTP 代理 | `pip install certifi` | `HTTPS Tunnel` |
| 7 | **npm** | 路径前缀模式 | `npm install is-sorted` | `.tgz cached (is-sorted)` |
| 8 | **npm** | 标准 HTTP 代理 | `npm install is-number` | `HTTPS Tunnel` |
| 9 | **Go** | GOPROXY 模式 | `go download pkg/errors` | `.zip/.mod cached (errors)` |
| 10 | **Go** | 标准 HTTP 代理 | `go download google/uuid` | `.zip/.mod cached (uuid)` |
| 11 | **Maven** | 官方源代理 | `curl slf4j-api.jar` | `.jar cached (slf4j)` |
| 12 | **Cargo** | 稀疏索引与包代理 | `curl config.json & serde.crate` | `Rewritten & .crate cached` |
| 13 | **Range** | 206 Partial Content | `curl -r 0-499 InRelease` | `206 Partial Content` |
| 14 | **Conditional**| 304 Not Modified | `curl -H "If-None-Match: ..." InRelease` | `304 Not Modified` |

---

## 4. 文件结构说明

- `Dockerfile`: 测试专用镜像构建定义文件；
- `docker-compose.yml`: 包含 `url-proxy` 及 14 组测试客户端容器的编排定义；
- `proxy.conf`: 全生态统一代理配置文件；
- `test.sh`: 自动化测试一键调度、审计与检测报告生成脚本；
- `README-zh.md`: 本说明文档（中文）；
- `README-en.md`: 英文说明文档。

---

## 5. 执行测试方法

在 WSL 或 Linux 终端中，切换至项目根目录并运行：

```bash
bash tests/e2e/test.sh
```

---

## 6. 预期终端检测报告

测试成功完成后，终端将输出类似于以下的结构化检测看板：

```text
==================================================================================================
                                url-proxy E2E Inspection Report                                   
==================================================================================================
Ecosystem    Mode             Command                    Duration   Cache / Status             Result
--------------------------------------------------------------------------------------------------
APT          Prefix Mode      apt download tree          22.69s     .deb cached (tree)         [PASS]
APT          HTTP Proxy       apt download ed            15.27s     .deb cached (ed)           [PASS]
Alpine       Prefix Mode      apk fetch tree             2.90s      .apk cached (tree)         [PASS]
Alpine       HTTP Proxy       apk fetch sed              2.23s      .apk cached (sed)          [PASS]
PyPI         Prefix Mode      pip install six            17.59s     .whl cached (six)          [PASS]
PyPI         HTTP Proxy       pip install certifi        23.70s     HTTPS Tunnel               [PASS]
npm          Prefix Mode      npm install is-sorted      5.79s      .tgz cached (is-sorted)    [PASS]
npm          HTTP Proxy       npm install is-number      3.77s      HTTPS Tunnel               [PASS]
Go           GOPROXY Mode     go download pkg/errors     2.12s      .zip/.mod cached (errors)  [PASS]
Go           HTTP Proxy       go download google/uuid    2.19s      .zip/.mod cached (uuid)    [PASS]
Maven        Central Proxy    fetch slf4j-api.jar        1.85s      .jar cached (slf4j)        [PASS]
Cargo        Sparse & Crates  config rewrite & serde     2.04s      Rewritten & .crate cached  [PASS]
Range        HTTP 206 Status  curl range 0-499           0.82s      206 Partial Content        [PASS]
Conditional  HTTP 304 Status  curl conditional 304       0.76s      304 Not Modified           [PASS]
Cache-HIT    Second Download  verify HIT & identical     1.20s      HIT & Bit-Identical        [PASS]
Anti-Pollute 404 Zero-Disk    curl 404 fake-artifact     0.45s      Zero-Disk (Clean)          [PASS]
HEAD-Guard   Method Conversion HEAD probe & GET warm     2.35s      Pre-warmed (>0 Bytes)      [PASS]
Rewrite      Header Cleansing strip ETag/Digest & len    1.15s      Sanitized & Length OK      [PASS]
Disk-TTL     Fresh HIT & Guard TTL hit & ETag isolate    1.30s      HIT-DISK-TTL (Isolated)    [PASS]
--------------------------------------------------------------------------------------------------
Total: 19 | Passed: 19 | Failed: 0 | Overall Health: HEALTHY
==================================================================================================
```
