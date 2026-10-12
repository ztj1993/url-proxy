# url-proxy

[English Documentation](README.md)

`url-proxy` 是一款通用、轻量且高效的 HTTP/HTTPS 正向与路径代理缓存服务，专为**大文件下载加速、高并发流量优化、流式动态响应体重写及局域网本地缓存**而设计。

系统既可作为通用的网络下载加速器与内网缓存中间件，也深度适配了现代研发运维基础设施，开箱即用支持操作系统软件包与主流编程语言依赖源（如 APT、Alpine、PyPI、npm、Docker/OCI Registry v2、Cargo、Go、Maven 等）的全链路闭环加速。

## 核心特性 (Features)

### 1. 通用代理与高性能缓存底座 (Core Engine)

- **并发请求合并 (Request Coalescing / Single-Flight)**: 当多个客户端同时请求下载同一资源时，系统仅向上游发起单次真实网络请求，所有客户端共享数据流，极大节省出口带宽并消除上游源站压力。
- **双模代理与透明中继 (Dual-Mode Proxy & Tunnel)**: 
  - 支持标准 HTTP 正向代理协议（如客户端 `http_proxy` 环境变量）与基于连接劫持的 HTTPS CONNECT 双向流式 TCP 隧道；
  - 100% 兼容优雅的路径前缀代理模式（如 `http://proxy:8888/https://example.com/file.zip`）。
- **HTTP Range 206 断点续传**: 本地缓存命中时毫秒级直接分发指定 Byte Range，实现零上游网络流量直出；未命中时流式透传分片与校验头部。
- **条件请求与 304 快速协商**: 向上游透传 `If-Modified-Since` 与 `If-None-Match`，拦截 304 Not Modified 响应并直接短路返回，零响应体传输，完全绕过磁盘 I/O 写入。
- **流式响应体动态重写引擎 (Response Body Rewriter)**: 支持在文本响应传输过程中毫秒级原地替换内容（如绝对 URL、跨域 CDN 地址），支持 `{PROXY_HOST}` 与 `{PROXY_SCHEME}` 宏变量动态插值，并内置二级二进制安全熔断机制，严防二进制文件误篡改。
- **动态元数据 Soft TTL 与 SWR 缓存**: 支持为高频动态元数据配置软保鲜期（Soft TTL），窗口内微秒级从本地返回以抵御 CI/CD 流量洪峰与 HTTP 429 限流；过期后利用 Stale-While-Revalidate（SWR）后台 304 条件协商静默续期。
- **断网/假死连接自动清理 (Idle Timeout Watchdog)**: 内置后台巡检机制，自动检测并断开长时间无数据传输的上游挂起连接，杜绝句柄与内存泄漏。
- **跨平台安全本地存储**: 自动对复杂 URL 参数与特殊字符进行合法化转义，兼容 Windows、Linux 等各类文件系统。

### 2. 统一配置引擎与架构解耦 (Configuration & Plugins)

- **集中式配置驱动 (`proxy.conf`)**: 采用声明式单行指令，集中纳管域名转发（`forward`）、路径别名（`alias`）、免缓存（`bypass`）、强缓存（`cache`）、类型别名（`type`）与动态重写（`rewrite`）。
- **路由作用域插件架构 (`RoutePlugin`)**: 支持为特定路由绑定特化处理逻辑（例如 `alias ... plugin:docker` 启用 Docker 分层本地直出加速），生态特化逻辑与核心引擎完全解耦，消除全局命名空间污染。

### 3. 开箱即用的典型生态加速 (Built-in Ecosystem Profiles)

针对研发交付与持续集成中的重度依赖场景，预置了覆盖八大主流生态的开箱即用代理规则与配置模板：
- **操作系统镜像源**: APT (Debian/Ubuntu)、Alpine Linux (apk)；
- **编程语言依赖源**: Python (PyPI)、Node.js (npm)、Rust (Cargo/Crates)、Go (GoProxy)、Java (Maven)；
- **容器与镜像制品**: Docker / OCI Registry v2（原生中继 Bearer Token 质询，镜像分层持久化强缓存与本地 0 上游直出）。

## 快速上手 (Quick Start)

### 二进制运行

```bash
# 查看帮助
go run .\url-proxy.go --help

# 默认加载当前目录下的 proxy.conf 并监听 8888 端口
go run .\url-proxy.go -addr :8888 -conf proxy.conf -cache ./cache
```

#### 命令行参数说明
- `-addr`: HTTP 服务器监听地址，默认 `:8888`
- `-cache`: 缓存文件存储目录，默认使用系统临时目录
- `-conf`: 统一代理配置文件路径，默认 `proxy.conf`（支持在指定路径、当前工作目录及可执行文件所在目录多级自动发现）
- `-idle-time`: 空闲超时时间，当上游服务器超过此时间未返回新数据时强制断开连接，默认 `10m`

### Docker 容器运行

#### 构建镜像
```bash
docker build -t ztj1993/url-proxy:latest .
```

#### 启动容器
```bash
docker run -d --name=uproxy \
  --restart=always \
  -p 8888:8888 \
  -v "$PWD/proxy.conf":/srv/proxy.conf:ro \
  -v "$PWD/cache":/srv/cache \
  ztj1993/url-proxy:latest -addr :8888 -conf /srv/proxy.conf -cache /srv/cache
```

## 统一配置系统 (`proxy.conf`)

`url-proxy` 采用声明式的单行指令配置系统，标准配置文件为 `proxy.conf`。

### 核心指令说明

| 指令 | 语法格式 | 说明 |
| :--- | :--- | :--- |
| **`forward`** | `forward <Host> <TargetBaseURL>` | 域名规则转发，按请求 Host 将流量导向指定上游 |
| **`alias`** | `alias <Prefix> <UpstreamBaseURL> [plugin:<Name>]` | 路径短别名路由映射，可选绑定生态特化插件（如 `plugin:docker`） |
| **`bypass`** | `bypass <Scope> <Type> <Pattern> [ttl:<Duration>]` | 免持久化流式直通规则，支持可选的 Soft TTL 保鲜时长修饰符 |
| **`cache`** | `cache <Scope> <Type> <Pattern>` | 持久化强缓存白名单规则 |
| **`type`** | `type <Name> [mime:<MIME>] [suffix:<Ext>]...` | 声明内容类型别名与分组（如 `type pypi mime:text/html suffix:.json`） |
| **`rewrite`** | `rewrite <Scope> [TypeSpec] <Pattern> <Replacement>` | 对命中作用域与类型的文本响应体执行动态替换（支持 `{PROXY_HOST}` 宏） |

仓库根目录下已内置涵盖通用场景与八大生态的最佳实践生产配置 `proxy.conf`，可直接使用或参考定制。

## 典型生态接入指南 (Ecosystem Guides)

针对各主流软件包管理系统与运行时环境，已提供详尽的客户端接入与镜像源配置指引：

| 生态环境 | 支持形态 | 客户端接入指南 |
| :--- | :--- | :--- |
| **APT (Debian / Ubuntu)** | 路径短别名模式 / 标准正向代理模式 | [APT 客户端配置指南](docs/usage/apt-zh.md) |
| **Alpine Linux (apk)** | 路径短别名模式 | [Alpine 客户端配置指南](docs/usage/alpine-zh.md) |
| **Python (PyPI)** | 闭环代理模式 (pip / uv / poetry / pdm) | [PyPI 客户端配置指南](docs/usage/pypi-zh.md) |
| **Node.js (npm)** | 闭环代理模式 (npm / yarn / pnpm) | [npm 客户端配置指南](docs/usage/npm-zh.md) |
| **Docker / OCI Registry** | Registry v2 中继与分层直出 (Docker / Containerd / Podman) | [Docker 客户端配置指南](docs/usage/docker-zh.md) |
| **Rust (Cargo / Crates)** | RFC 2785 稀疏索引与静态包加速 | [Cargo 客户端配置指南](docs/usage/cargo-zh.md) |
| **Go (GoProxy)** | 路径短别名模式 (`GOPROXY`) | [GoProxy 客户端配置指南](docs/usage/goproxy-zh.md) |
| **Java (Maven)** | 路径短别名模式 (`settings.xml`) | [Maven 客户端配置指南](docs/usage/maven-zh.md) |

更多详细指引请参阅 [生态使用指南索引](docs/usage/README-zh.md)。

## 自动化测试与验证 (Testing)

项目配套了覆盖全生态的高保真测试集群与端到端自动化验证流水线：

- **单元与集成测试**: 各功能模块均配有独立的 Mock 测试集群（位于 `tests/` 目录下）。
- **Docker Compose E2E 自动化测试**: 编排 14 个测试客户端容器，提供真实生态环境的端到端回归校验：
  ```bash
  cd tests/e2e
  bash test.sh
  ```
  详见 [E2E 测试指南](tests/e2e/README-zh.md)。
