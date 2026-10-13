# APT 软件包缓存与代理中继方案 (APT Package Caching & Proxy Relay Plan)

## 1. 方案背景与问题洞察 (Background & Problem Analysis)

### 1.1 核心痛点
在多服务器节点、容器构建集群（如 Docker build）、虚拟机模板初始化或持续集成（CI/CD runner）环境中，系统频繁执行 `apt-get update` 与 `apt-get install` 操作。由此产生了一系列痛点：
- **重复消耗外网出口带宽**：不同宿主机或容器在拉取相同的系统依赖（如 `build-essential`、`linux-image-*`、`python3` 等基础包）时，重复向上游公网镜像源发起请求，造成出口带宽极度浪费。
- **构建与拉取延迟不可控**：公网镜像源易受网络波动、跨境延迟或单连接限速影响，导致多节点部署流水线严重受阻。
- **传统缓存方案维护成本高**：像 `apt-cacher-ng` 或通用反向代理 `squid`，配置繁琐，且在面对原生 HTTPS 上游镜像时，要么需要侵入式安装根证书，要么无法对加密流进行内容级缓存。
- **多配置文件碎片化与硬编码风险**：若分别维护转发、别名与免缓存规则文件，极易导致运维割裂与 CLI 参数膨胀；若在代码中硬编码文件后缀，极易引发敏感名称软件包被误判免缓存（误杀）的问题。

### 1.2 建设目标
扩展 `url-proxy`，在充分复用其既有的请求合并（Request Coalescing）、安全路径（Safe Path）、引用计数原子重命名与空闲超时巡检等底层能力的基础上，将其打造成一个专为 APT 打造的高性能、轻量级、免侵入、纯配置驱动的代理中继与缓存加速服务：
1. **全协议接入兼容**：同时支持标准 HTTP 正向代理协议（`Acquire::http::Proxy`）、路径短别名模式（`deb http://proxy/ubuntu ...`）与原生完整路径模式；
2. **动静分离与防误杀**：动态元数据实时流式直通（Zero Disk I/O）杜绝 Hash Sum Mismatch，白名单规则绝对优先保障软件包 100% 稳定落盘；
3. **协议高阶特性透传**：完整支持 `If-None-Match` / 304 快速协商与 `Range` / 206 断点续传；
4. **单真理源统一配置**：通过根目录主配置文件 `proxy.conf` 集中纳管别名路由、域名网关、免缓存规则与白名单保护，彻底移除代码写死硬编码。

---

## 2. 核心技术思考与架构决策演进 (Design Thinking & Decision Evolution)

### 2.1 思考一：客户端接入方式与 HTTPS 缓存的天然破局
- **讨论焦点**：APT 是否应该采用标准正向代理协议（`Acquire::http::Proxy`），还是沿用现有的 URL 路径拼接模式（`http://proxy/http://...`）？
- **深度洞察与权衡**：
  1. 如果采用**标准正向代理协议**，当客户端面对 `https://` 的源（如 Debian 官方源 `https://deb.debian.org`）时，客户端会向代理发送 `CONNECT host:443` 隧道请求。该请求建立后，传输层被端到端 TLS 加密，代理无法嗅探到明文请求内容，**根本无法解密并持久化缓存 `.deb` 文件**（除非在每台客户端操作系统强制导入自建的自签名 CA 根证书，这在标准化运维场景下不可接受）。
  2. 如果采用**客户端明文 HTTP 接入、代理端发起原生 HTTPS 的路径模式**：
     ```text
     [客户端] --- (HTTP 明文请求) ---> [url-proxy] --- (原生 HTTPS) ---> [官方 HTTPS 镜像源]
     ```
     - 客户端通过普通 HTTP 连接代理，代理拥有完整的明文数据流和响应头解析权限；
     - 代理作为合法的 HTTP 客户端向上游发起原生 HTTPS 连接，**天然规避了 TLS 加密阻断嗅探的问题，无需客户端导入任何证书即可完成 `.deb` 的磁盘落盘与并发流复用**。
- **安全考量**：
  Debian 与 Ubuntu 的 APT 生态原生基于 GPG 签名机制进行包完整性与防篡改校验。即使客户端与内网代理之间采用 HTTP 明文传输，APT 客户端也会对每个包的哈希和签名进行强制校验，传输层无论经过任何中继都不会造成安全降级。
- **最终决策**：代理端双模并存——同时支持用于原生 HTTP 源的标准正向代理，以及用于私有化接入的路径短别名模式。

### 2.2 思考二：动静分离缓存与防误杀配置机制
- **技术风险与痛点**：
  APT 索引（`InRelease`、`Release`、`Packages.xz`）随官方更新频繁变动，一旦落盘会被长期缓存，引发客户端 `Hash Sum mismatch`；但若粗暴按名称模糊匹配（如匹配包含 `packages` 或 `release`），正常软件包（如 `packages-base.deb`、`release-notes.tar.gz`）就会被错误标记为免缓存（误杀），丧失加速价值。
- **架构决策与严密防误杀设计**：
  1. **资产白名单绝对优先（Whitelist Enforcement）**：
     在统一配置中通过 `cache` 动作声明白名单例外规则（如 `cache * suffix .deb`）。只要命中 `cache` 规则，立即赋予绝对最高优先级强制落盘，直接退出免缓存判定。
  2. **严格的路径上下文作用域限定（Context Scoping）**：
     APT 的动态元数据几乎严格分布于 `/dists/` 目录结构之下，而真正的软件包存放在 `/pool/` 结构之下。因此免缓存规则默认绑定在 `/dists/` 作用域内，杜绝扩散至 `/pool/`。
  3. **外置纯配置驱动（Zero Code Hardcoding）**：
     彻底摒弃 Go 代码内部对包扩展名的写死硬编码，将动静判定权完全交由统一配置文件 `proxy.conf` 驱动。

### 2.3 思考三：协议完整性——支持 304 条件协商与 Range 断点续传
- **技术优化与收益**：
  1. **条件请求与 304 Not Modified 透传**：
     - APT 在执行日常 `apt update` 时，会大量发送带有 `If-Modified-Since` 或 `If-None-Match (ETag)` 的请求。
     - 代理将这些条件头如实透传给上游；若上游判定无更新返回 `304 Not Modified`，代理立即将 304 短路透传给客户端，不产生多余的数据拉取，使得日常 update 极速完成（0 负载传输）。
  2. **Range 请求与 206 Partial Content 透传**：
     - 当遇到网络偶发中断或用户手动 Ctrl+C 后再次执行下载时，APT 会携带 `Range: bytes=start-` 发起断点续传。
     - 若本地存在完整缓存，直接通过 `http.ServeFile` 本地高效响应 Range 切片（0 上游网络开销）；
     - 若本地无完整缓存，代理将 `Range` 头透传至上游并原样流式回传 `206 Partial Content`，不落盘脏切片，保证客户端在断网重连后无缝续传。

### 2.4 思考四：统一配置文件系统与单行指令规范
- **痛点与决策演进**：
  原计划为域名转发、免缓存、路径别名分别维护独立的配置文件（如 `forwarding.conf`、`nocache.conf`），导致运维割裂与 CLI 参数膨胀。经重构决策：
  - 收拢建立**单一真理源配置文件 (`proxy.conf`)**；
  - 采用**统一单行指令语法**（每行均以明确的动词前缀开头：`alias`、`forward`、`bypass`、`cache`）；
  - 统一命令行参数 `-conf <path>`，支持 CLI 参数指定、当前工作目录与可执行文件目录多级自动探测。

### 2.5 思考五：源路径表示风格的双模式支持与缓存单真理源
- **架构决策**：
  - **短别名模式（推荐，客户端 sources.list 最干净）**：
    客户端配置形如 `deb http://proxy:8888/ubuntu noble main`。代理通过 `alias` 指令将 `/ubuntu` 动态重写为真实上游 BaseURL `http://archive.ubuntu.com/ubuntu`。
  - **缓存单真理源（Zero Duplicate Cache）**：
    动态重写在请求入口处完成，本地安全落盘缓存路径（`SafeUrlPath`）均基于重写后的真实上游绝对 URL 计算。因此短别名模式与完整原路径模式访问同一资源时，**严格共享同一套本地磁盘缓存**，杜绝重复占用。

### 2.6 思考六：极简的存储与生命周期管理
- **架构决策**：
  - **坚持 KISS（Keep It Simple, Stupid）原则**：不引入复杂的后台 LRU 淘汰引擎，所有缓存文件基于规范的分层安全路径标准落盘，目录结构清晰直观。
  - 存储空间管理交由操作系统的标准工具（如外部 cron 定期按最后访问时间 `find ... -atime +30 -delete`）完成，确保核心代理服务的极简、轻量与极致稳定。

---

## 3. 详细技术实现方案 (Technical Implementation Details)

### 3.1 统一配置文件语法规范 (`proxy.conf`)

每行均为独立的单行指令，使用空格或制表符作为分隔符。以 `#` 开头的行或空行自动被忽略：

| 指令前缀 | 完整语法格式 | 功能说明 | 对应特性 |
| :--- | :--- | :--- | :--- |
| **`alias`** | `alias <短别名> <上游BaseURL>` | 声明路径第一级短别名路由，自动进行 URL 动态重写 | **306** |
| **`forward`** | `forward <源Host> <前置代理BaseURL>` | 声明上游 Host 镜像网关或前置代理重定向 | **103** |
| **`bypass`**<br>*(或 `nocache`)* | `bypass <Scope> <Type> <Pattern>` | 声明动态资源免缓存规则，纯流式透传，严禁落盘 | **305** |
| **`cache`** | `cache <Scope> <Type> <Pattern>` | 声明强制落盘缓存 / 白名单例外规则，绝对最高优先级 | **305** |

#### 作用域 (`<Scope>`) 参数定义
- `*`：全局作用域，对所有域名和路径生效；
- `/prefix/`：路径前缀作用域，当请求路径包含该上下文时生效（例如 `/dists/`）；
- `domain.com`：域名作用域，仅当目标 Host 匹配时生效；
- `domain.com/prefix/`：域名 + 路径复合作用域。

#### 匹配类型 (`<Type>`) 参数定义
- `exact`：严格文件名匹配（忽略大小写，例如 `exact InRelease`）；
- `prefix`：文件名前缀匹配（忽略大小写，例如 `prefix Packages`）；
- `suffix`：文件名后缀匹配（忽略大小写，例如 `suffix .deb`）；
- `regex`：正则表达式匹配（匹配完整 URL 路径或文件名，例如 `regex ^.*/by-hash/.*$`）。

---

### 3.2 动态免缓存与白名单防误杀仲裁引擎 (`url-proxy.go`)

```
                       收到客户端目标请求
                               │
                               ▼
                 遍历 proxy.conf 中的规则列表
                               │
                匹配到任意一条 Action == "cache" 规则?
                              / \
                        是   /   \  否
                            /     \
                           ▼       ▼
                      [强制落盘]   命中任意 Action == "bypass" 规则?
                      (退出免缓存)       / \
                                   是 /   \ 否
                                     /     \
                                    ▼       ▼
                              [纯流式透传]  [默认落盘缓存]
                              (绝不落盘)
```

1. **白名单规则绝对优先**：若请求匹配任意一条声明为 `cache` 的规则，立即判定为强制缓存落盘，退出免缓存判定；
2. **免缓存动态直通 (Zero Disk I/O)**：命中 `bypass` 规则的请求透明向上游中继客户端条件协商头与特征头（`If-Modified-Since`、`If-None-Match`、`Range`、`User-Agent`）；上游返回 304 时直接短路透传 304；返回 200 时通过 `io.Copy(w, resp.Body)` 实时流式中继，**整个过程绝不创建 `.tmp` 临时文件，绝不写入本地磁盘**；
3. **默认行为**：未命中规则的资源正常执行请求合并与落盘缓存。

---

### 3.3 路径短别名路由与动态重写引擎 (`url-proxy.go`)

在 `Handler` 请求入口处进行路径分析：
1. 若 `uris.Scheme` 为空或不在 `prefixes` (`{"ftp", "http", "https"}`) 中，提取路径的第一级片段 `aliasKey`；
2. 若 `targetBase, ok := aliases[aliasKey]` 命中：
   - 剥离 `targetBase` 尾部斜杠与子路径首部斜杠，使用标准单个 `/` 进行规范化拼接；
   - 原样保留客户端查询参数（`r.URL.RawQuery`）；
   - 动态重写为绝对 URI 并重新解析为 `url.URL`；
   - 校验重写后的协议是否合法；
3. 若未命中任何别名，直接返回 `404 Not Found`；
4. 重写后的绝对 URI 无缝流转至后续所有既有管线（免缓存判定、304 协商、Range 续传、本地文件缓存）。

---

### 3.4 条件协商、Range 续传与标准正向代理全链路集成

- **条件请求与 304 透传**：客户端发送 `If-Modified-Since` 或 `If-None-Match` 时，代理向上游透明中继；若上游返回 304，代理直接向客户端返回 304 并关闭连接，零 Body 传输。
- **Range 分片与 206 续传**：
  - 本地存在完整缓存文件时，直接通过 `http.ServeFile` 高效本地响应（0 上游网络开销）；
  - 本地无缓存时，透明向上游发起 Range 请求并纯流式返回 `206 Partial Content`，不落盘脏切片。
- **标准正向代理与 CONNECT 隧道**：
  - 完整解析标准正向代理的绝对请求行（`GET http://...`）；
  - 针对 HTTPS 请求提供 `CONNECT host:443` 双向 TCP 隧道透传。

---

## 4. 实施路线图与功能模块清单 (Modular Implementation Roadmap)

本方案全部 7 个功能特性均已完整实现、全面通过自动化测试，并独立提交：

| 模块编号 | 功能模块名称 | 核心能力与技术亮点 | 交付产物与验证规范 | 关联 Git 提交信息与哈希 |
| :---: | :--- | :--- | :--- | :--- |
| **301** | **标准 HTTP 正向代理协议支持** | 支持正向代理绝对请求行识别与 HTTPS CONNECT 隧道，支持 `Acquire::http::Proxy` 零侵入接入。 | • Feature 301 规格规范<br>• `tests/301_forward_proxy/` 自动化冒烟测试全通 | `ce31ce5` `feat(proxy): support standard HTTP forward proxy protocol (301)` |
| **302** | **条件请求与 304 快速协商透传** | 透传 `If-Modified-Since` 与 `If-None-Match (ETag)`，上游返回 304 时直接短路回传，实现日常 update 极速完成且零多余流量。 | • Feature 302 规格规范<br>• `tests/302_conditional_304/` 304 短路透传验证 | `a9ad53e` `feat(proxy): support conditional requests and 304 fast passthrough (302)` |
| **303** | **Range 请求与 206 断点续传支持** | 透传 `Range` 请求头及 `206 Partial Content`，本地有缓存时直出分片，本地无缓存时纯流式透传不落盘。 | • Feature 303 规格规范<br>• `tests/303_range_requests/` 206 分块切片测试 | `31613ba` `feat(proxy): support Range requests and 206 Partial Content resuming (303)` |
| **304** | **统一代理配置文件系统** | 引入单一真理源 `proxy.conf`，确立单行指令规范（`alias`, `forward`, `bypass`, `cache`），提供 `-conf` 参数及多级自动探测。 | • Feature 304 规格规范<br>• `tests/304_unified_config/` CLI 与探测测试 | `9bd2a94` `feat: implement unified proxy configuration system (Feature 304)` |
| **305** | **可配置免缓存规则与防误杀机制** | 彻底去代码硬编码；实现基于上下文作用域（`/dists/`）的动态元数据纯流式直通（Zero Disk I/O）与白名单绝对优先防误杀仲裁。 | • Feature 305 规格规范<br>• `tests/305_selective_cache/` 动静分离与防误杀测试 | `7449ed3` `feat(cache): implement configurable cache bypass rules and anti-false-positive engine (305)` |
| **306** | **路径短别名路由与动态重写** | 提取第一级路径短别名，动态重写为上游 BaseURL；实现私有镜像站接入体验，并与安全缓存路径联动共享同一磁盘缓存。 | • Feature 306 规格规范<br>• `tests/306_path_alias/` 短别名重写与全管线联动测试 | `bcb63e6` `feat(routing): support path-based short alias routing and dynamic URL rewriting (306)` |
| **307** | **APT 代理全链路端到端集成验证** | 汇总 301~306 全部能力，构建高仿真 APT 仓库服务器与全生命周期自动化集成测试套件，量化审计上游流量。 | • Feature 307 规格规范<br>• `tests/307_apt_integration/` 全链路端到端集成测试 | `314ed48` `test(apt): add end-to-end integration test suite and profile templates for APT (307)` |

---

## 5. 生产级配置文件完整模版 (`proxy.conf`)

```text
# ==============================================================================
# url-proxy 生产级 APT 统一配置文件 (proxy.conf)
# ==============================================================================

# ------------------------------------------------------------------------------
# 1. 路径短别名路由 (Path Short Alias Routing)
# 客户端 sources.list 直接配置: deb http://<proxy>:8888/ubuntu jammy main
# ------------------------------------------------------------------------------
alias ubuntu http://archive.ubuntu.com/ubuntu
alias ubuntu-security http://security.ubuntu.com/ubuntu
alias debian https://deb.debian.org/debian
alias debian-security https://deb.debian.org/debian-security

# ------------------------------------------------------------------------------
# 2. 域名规则转发 (Host Forwarding)
# ------------------------------------------------------------------------------
forward registry-1.docker.io http://mirror.local/docker
forward github.com https://ghproxy.example.com

# ------------------------------------------------------------------------------
# 3. 缓存控制与白名单例外 (Cache Control & Whitelisting)
# 语法: cache/bypass <Scope> <Type> <Pattern>
# ------------------------------------------------------------------------------
# 白名单例外 (配置文件自主声明，强制落盘，绝对最高优先级，彻底杜绝误杀)
cache * suffix .deb
cache * suffix .udeb
cache * suffix .ddeb
cache * suffix .tar.gz
cache * suffix .tar.xz
cache * suffix .tar.zst
cache * suffix .tar.bz2
cache * suffix .dsc

# APT 动态索引元数据免缓存 (纯流式透传，严格禁止落盘，避免 Hash 校验失败)
bypass /dists/ exact InRelease
bypass /dists/ exact Release
bypass /dists/ exact Release.gpg
bypass /dists/ prefix Packages
bypass /dists/ prefix Sources
bypass /dists/ prefix Translation-
bypass /dists/ regex ^.*/by-hash/.*$
```

---

## 6. 客户端配置与落地使用指南 (Client Configuration Guide)

### 6.1 方案 A：短别名模式（强烈推荐，客户端 sources.list 最干净整洁）

1. **Ubuntu 客户端配置 (`/etc/apt/sources.list`)**：
   ```text
   deb http://<proxy-ip>:8888/ubuntu jammy main restricted universe multiverse
   deb http://<proxy-ip>:8888/ubuntu jammy-updates main restricted universe multiverse
   deb http://<proxy-ip>:8888/ubuntu-security jammy-security main restricted universe multiverse
   ```

2. **Debian 客户端配置 (`/etc/apt/sources.list`)**：
   ```text
   deb http://<proxy-ip>:8888/debian bookworm main non-free-firmware
   deb http://<proxy-ip>:8888/debian bookworm-updates main non-free-firmware
   deb http://<proxy-ip>:8888/debian-security bookworm-security main non-free-firmware
   ```

### 6.2 方案 B：标准 HTTP 正向代理模式（零修改 sources.list，配置 APT 代理选项）

在客户端创建 `/etc/apt/apt.conf.d/01proxy`：
```text
Acquire::http::Proxy "http://<proxy-ip>:8888";
```
- 适用于原生使用 `http://` 镜像源的客户端；
- 无需改动任何既有的 `/etc/apt/sources.list` 文件。

### 6.3 方案 C：完整原路径模式（即插即用，零代理端预配置）

客户端直接把原始镜像源 URL 拼接在代理服务地址之后：
```text
deb http://<proxy-ip>:8888/http://archive.ubuntu.com/ubuntu jammy main
deb http://<proxy-ip>:8888/https://deb.debian.org/debian bookworm main
```
- 代理端无需预先配置任何 `alias`；
- 随时随地代理任意第三方第三方 PPA 或临时源。

---

## 7. 总结与后续演进预留 (Summary & Roadmap)

通过 301 到 307 的闭环落地与全链路集成验证，`url-proxy` 已演进成为兼备**极简部署**、**高性能高并发**、**强缓存一致性**与**企业级防误杀**的专业 APT 缓存代理服务。

本体系沉淀的核心技术机制具备极强的横向扩展能力，天然兼容后续的云原生与通用分发场景演进：
- **Docker 容器镜像加速**：
  - 容器镜像清单 (`manifests`)：类似 `InRelease`，属于易变元数据，复用 `bypass` 实时流式直通与条件校验；
  - 镜像层切片 (`blobs/sha256:...`)：类似 `.deb`，基于 SHA256 内容寻址不可变，复用 `cache` 绝对白名单落盘与高并发请求合并。
- **PyPI / NPM / Maven 语言生态包代理**：
  - 包索引页面通过 `bypass` 直通；
  - `.whl`、`.tgz`、`.jar` 安装包通过 `cache` 规则强制落盘缓存。
