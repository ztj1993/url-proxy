# Docker Compose 核心功能与多生态全特性 E2E 端到端自动化测试套件

本目录提供 `url-proxy` 的 Docker Compose 端到端自动化测试套件（E2E Suite），包含测试镜像构建定义、容器网络编排、配置文件、测试用例调度及控制台格式化报告审计逻辑。

---

## 1. 概述与测试目标

测试套件旨在通过真实 Docker Compose 容器网络环境，针对真实公网源站全面验证 `url-proxy` 在各大语言包管理器与底层操作系统生态下的代理转发、响应体重写、断点续传、条件短路、高并发合并及磁盘落盘缓存能力：

1. **容器化独立隔离**：测试环境完全基于独立的 `tests/e2e/Dockerfile` 打包，下载缓存全部收敛在 `url-proxy` 容器内部 `/cache` 目录中，不与宿主机产生 bind mount 挂载污染；
2. **多生态双模式全覆盖**：覆盖 APT、Alpine、PyPI、npm、Go 5 大主流生态的**路径前缀/别名模式（Prefix Mode）**与**标准 HTTP 正向代理模式（HTTP Proxy Mode）**；
3. **高级特性与稳定性端到端验证**：
   - Maven Central Jar 包代理与二次请求 Cache-HIT 严格字节一致性；
   - Rust Cargo 稀疏索引重写、响应头脱敏安全清理与源码包代理；
   - HTTP Range 206 本地缓存分片直出与切片尺寸断言；
   - HTTP 304 条件协商本地快速短路与 SWR 304 条件续期；
   - 404 错误响应防污染零落盘与动态元数据（Release、APKINDEX 等）免缓存零落盘；
   - HEAD 请求自动转 GET 预热与防 0 字节损坏；
   - 磁盘 TTL 缓存保鲜期内命中与异构客户端 ETag 隔离保护；
   - Docker / OCI Registry v2 清单动态透传与分层（Blobs）持久化缓存；
   - 并发请求合并（Singleflight）：多客户端同时请求冷资源时 Leader 回源建缓存与 Follower 广播追读协同；
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
            [公网源站: PyPI / APT / Alpine / npm / Go / Maven / Cargo / Mock Docker]
```

---

## 3. 全量 23 项 E2E 测试矩阵

| 用例编号 | 生态/特性 | 测试模式/特性 | 核心测试命令 | 预期结论与断言 |
| :---: | :---: | :---: | :--- | :--- |
| 1 | **APT** | 路径前缀模式 | `apt download tree` | `.deb cached (tree)` |
| 2 | **APT** | 标准 HTTP 代理 | `apt download ed` | `.deb cached (ed)` |
| 3 | **Alpine** | 路径前缀模式 | `apk fetch tree` | `.apk cached (tree)` |
| 4 | **Alpine** | 标准 HTTP 代理 | `apk fetch sed` | `.apk cached (sed)` |
| 5 | **PyPI** | 路径前缀模式 | `pip install six` | `.whl cached (six)` |
| 6 | **PyPI** | 标准 HTTP 代理 | `pip install certifi` | `HTTPS Tunnel` (审计 CONNECT 记录) |
| 7 | **npm** | 路径前缀模式 | `npm install is-sorted` | `.tgz cached (is-sorted)` |
| 8 | **npm** | 标准 HTTP 代理 | `npm install is-number` | `HTTPS Tunnel` (审计 CONNECT 记录) |
| 9 | **Go** | GOPROXY 模式 | `go download pkg/errors` | `.zip/.mod cached (errors)` (无 list 污染) |
| 10 | **Go** | 标准 HTTP 代理 | `go download google/uuid` | `.zip/.mod cached (uuid)` |
| 11 | **Maven** | 官方源代理 | `curl slf4j-api.jar` | `.jar cached (slf4j)` |
| 12 | **Cargo** | 稀疏索引与包代理 | `curl config.json & serde.crate` | `Rewritten & .crate cached` |
| 13 | **Range** | 206 Partial Content | `curl -r 0-499 slf4j-api.jar` | `206 Partial Content (Cache)` (本地缓存切片直出) |
| 14 | **Conditional**| 304 Not Modified | `curl -H "If-Modified-Since: ..." InRelease` | `304 Not Modified (Local)` (本地快速短路) |
| 15 | **Cache-HIT** | 二次请求一致性 | `verify HIT & identical` | `HIT & Bit-Identical` |
| 16 | **Anti-Pollute** | 404 错误防污染 | `curl 404 fake-artifact` | `Zero-Disk (Clean)` |
| 17 | **HEAD-Guard** | HEAD 转 GET 预热 | `HEAD probe & GET warm` | `Pre-warmed (>0 Bytes)` |
| 18 | **Rewrite** | 响应头脱敏清洗 | `strip ETag/Digest & len` | `Sanitized & Length OK` |
| 19 | **Disk-TTL** | 保鲜期命中与 ETag 隔离 | `TTL hit & ETag isolate` | `HIT-DISK-TTL (Isolated)` |
| 20 | **Docker** | OCI Registry v2 代理 | `manifest bypass & blob cache` | `Manifest Bypass & Layer Hit` |
| 21 | **Singleflight**| 并发请求合并 | `concurrent fetch & broadcast` | `Leader Coalesced & Broadcast` |
| 22 | **SWR-Renew** | 304 条件续期 | `expired 304 & touch mtime` | `REVALIDATED-304 & Renewed` |
| 23 | **Bypass-Guard**| 动态元数据零落盘 | `verify bypass non-cached` | `Zero-Disk (Metadata Clean)` |

---

## 4. 文件结构说明

- `Dockerfile`: 测试专用镜像构建定义文件（包含 `url-proxy` 与 `mock-docker` 构建）；
- `docker-compose.yml`: 包含 `url-proxy`、`mock-docker` 及 23 组测试客户端容器的编排定义；
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
Ecosystem    Mode             Command                    Duration   Cache / Status               Result
--------------------------------------------------------------------------------------------------
APT          Prefix Mode      apt download tree          22.69s     .deb cached (tree)           [PASS]
APT          HTTP Proxy       apt download ed            15.27s     .deb cached (ed)             [PASS]
Alpine       Prefix Mode      apk fetch tree             2.90s      .apk cached (tree)           [PASS]
Alpine       HTTP Proxy       apk fetch sed              2.23s      .apk cached (sed)            [PASS]
PyPI         Prefix Mode      pip install six            17.59s     .whl cached (six)            [PASS]
PyPI         HTTP Proxy       pip install certifi        23.70s     HTTPS Tunnel                 [PASS]
npm          Prefix Mode      npm install is-sorted      5.79s      .tgz cached (is-sorted)      [PASS]
npm          HTTP Proxy       npm install is-number      3.77s      HTTPS Tunnel                 [PASS]
Go           GOPROXY Mode     go download pkg/errors     2.12s      .zip/.mod cached (errors)    [PASS]
Go           HTTP Proxy       go download google/uuid    2.19s      .zip/.mod cached (uuid)      [PASS]
Maven        Central Proxy    fetch slf4j-api.jar        1.85s      .jar cached (slf4j)          [PASS]
Cargo        Sparse & Crates  config rewrite & serde     2.04s      Rewritten & .crate cached    [PASS]
Range        HTTP 206 Status  curl range 0-499           0.82s      206 Partial Content (Cache)  [PASS]
Conditional  HTTP 304 Status  curl conditional 304       0.76s      304 Not Modified (Local)     [PASS]
Cache-HIT    Second Download  verify HIT & identical     1.20s      HIT & Bit-Identical          [PASS]
Anti-Pollute 404 Zero-Disk    curl 404 fake-artifact     0.45s      Zero-Disk (Clean)            [PASS]
HEAD-Guard   Method Conversion HEAD probe & GET warm     2.35s      Pre-warmed (>0 Bytes)        [PASS]
Rewrite      Header Cleansing strip ETag/Digest & len    1.15s      Sanitized & Length OK        [PASS]
Disk-TTL     Fresh HIT & Guard TTL hit & ETag isolate    1.30s      HIT-DISK-TTL (Isolated)      [PASS]
Docker       OCI Registry v2  manifest bypass & blob     0.95s      Manifest Bypass & Layer Hit  [PASS]
Singleflight Request Merge    concurrent fetch & wait    1.45s      Leader Coalesced & Broadcast [PASS]
SWR-Renew    304 TTL Renewal  expired 304 & touch mtime  4.20s      REVALIDATED-304 & Renewed    [PASS]
Bypass-Guard Zero-Disk Meta   verify bypass non-cached   1.10s      Zero-Disk (Metadata Clean)   [PASS]
--------------------------------------------------------------------------------------------------
Total: 23 | Passed: 23 | Failed: 0 | Overall Health: HEALTHY
==================================================================================================
```