# Feature 321: 模块化代码解耦与领域分层重构架构 (Modular Architecture Refactoring)

## 1. 功能目标 (Goal)

随着 `url-proxy` 项目支持的生态协议不断扩展，系统已由最初单纯的单文件缓存代理完整演进至覆盖 8 大官方镜像源生态（APT、Alpine、Go、Maven、PyPI、npm、Docker、Cargo）以及 HTTPS CONNECT 隧道、Range 206 断点分片、条件 304 快速协商、统一配置引擎、免缓存白名单仲裁、短别名路由、响应体流式重写、路由作用域插件以及动态元数据 SWR 软缓存等完整矩阵。全部核心实现集中于根目录单一文件 `url-proxy.go` 中，导致代码膨胀至 1759 行，职责高度耦合，存在包级全局变量并发竞争风险，且工程缺乏原生的 Go 白盒单元测试套件。

**Feature 321 建设目标**：
1. **单一职责与领域分层**：基于单一职责原则（SRP），将单体源文件解耦拆分为 7 个高内聚、低耦合的私有领域子包（`internal/config`、`internal/cache`、`internal/ttl`、`internal/rewriter`、`internal/plugin`、`internal/downloader`、`internal/proxy`）；
2. **面向对象与状态完全隔离**：消除全部包级全局变量与全局互斥锁（如 `jobs`、`jobsMu`、`requestID`、`globalTTLManager`、`routePlugins`），采用结构体管理器封装与依赖注入管理生命周期；
3. **零破坏向下兼容**：根目录保留轻量级主入口 `url-proxy.go`（代码行数由 1759 行缩减至 ~70 行），保持 `go run url-proxy.go` / `go run .` 启动命令、跨平台编译脚本（`build.bat` / `build.sh`）及 Docker 构建 100% 兼容；
4. **原生 Go 单元测试闭环**：为每个领域子包编写原生单元测试（`*_test.go`），实现 `go test ./...` 一键自动化质量回归。

---

## 2. 核心架构与依赖设计 (Architecture & Dependency Design)

### 2.1 目录结构规划

```text
url-proxy/
├── url-proxy.go                # 主程序入口 (仅负责参数解析与组件装配，~70 行)
├── proxy.conf                  # 生产级统一配置模板
├── go.mod                      # 模块名称: uri-proxy
├── build.sh / build.bat        # 跨平台自动化编译脚本
├── Dockerfile                  # 容器镜像构建脚本
├── internal/
│   ├── config/                 # 统一配置领域子包
│   │   ├── config.go           # ProxyConfig, NoCacheRule, TypeDefinition, RewriteRule, Load, Parse
│   │   └── config_test.go      # 配置指令语法与作用域测试
│   ├── cache/                  # 缓存管理领域子包
│   │   ├── path.go             # GetSafeUrlPath (跨平台安全路径与 Query 隔离)
│   │   ├── bypass.go           # ShouldBypassCache (白名单绝对优先仲裁)
│   │   ├── dir.go              # EnsureCacheDir (缓存根目录管理)
│   │   └── cache_test.go       # 路径转义与免缓存规则测试
│   ├── ttl/                    # 元数据软过期与 SWR 领域子包
│   │   ├── manager.go          # TTLManager, TTLItem (并发安全内存元数据管理)
│   │   └── ttl_test.go         # 软过期检测与 SWR 状态刷新测试
│   ├── rewriter/               # 响应体流式重写领域子包
│   │   ├── rewriter.go         # BodyRewriter, RuleMatchesResponse, ShouldRewrite, Apply
│   │   ├── mime.go             # MIME/后缀匹配与二进制安全检测
│   │   └── rewriter_test.go    # PEP 691 JSON 与 Gzip 解压重写测试
│   ├── plugin/                 # 路由作用域协议插件领域子包
│   │   ├── plugin.go           # RoutePlugin 接口与 Registry 注册表
│   │   ├── docker.go           # DockerPlugin (Blobs 快路径)
│   │   └── plugin_test.go      # 插件匹配与快路径响应测试
│   ├── downloader/             # 并发下载与调度领域子包
│   │   ├── job.go              # DownloadJob 模型与临时文件原子重命名/错误清理
│   │   ├── manager.go          # Manager 任务池 (GetOrCreate, Remove)
│   │   ├── watchdog.go         # Idle Watchdog 后台巡检例程
│   │   └── downloader_test.go  # 请求合并、广播流与看门狗超时测试
│   └── proxy/                  # 协议代理与分发领域子包
│       ├── server.go           # Server 主实体与 ServeHTTP 路由调度
│       ├── router.go           # URL 规范化与短别名重写
│       ├── tunnel.go           # HTTPS CONNECT TCP 隧道劫持透传
│       ├── range.go            # Range 206 分片直出与流式透传
│       ├── bypass.go           # 免缓存与 SWR 异步后台同步
│       ├── download.go         # 并发请求合并下载与 Followers 广播
│       └── proxy_test.go       # 协议闭环、Range 直出、304 快速短路测试
├── docs/                       # 架构设计与特性文档
└── tests/                      # 既有端到端 Mock 集成测试套件
```

### 2.2 有向无环依赖图 (Acyclic Dependency Graph)

为严格规避 Go 语言编译时的循环依赖错误（`import cycle not allowed`），系统确立了严格的单向分层调用：

```
           main (url-proxy.go)
             │   │    │    │
             │   │    │    ▼
             │   │    │  internal/proxy ────────┬──────────────┐
             │   │    │    │          │         │              │
             │   ▼    ▼    ▼          ▼         ▼              ▼
             │  internal/downloader internal/rewriter internal/plugin internal/ttl
             │                        │         │              │
             ▼                        ▼         ▼              │
           internal/config ◄────────────────── internal/cache ─┘
             │
             ▼
           Go 标准库 (net/http, sync, os, io)
```

---

## 3. 各领域子包核心实现规范 (Component Implementations)

### 3.1 `internal/config` (配置解析领域)
- **职责**：作为统一配置的“单一真理源”，负责不可变规则定义与语法解析。
- **核心数据结构**：
  - `ProxyConfig`：解析存储 `alias`、`no_cache`、`type`、`rewrite`、`plugin`、`metadata_ttl`、`metadata_swr` 指令。
  - `NoCacheRule`、`TypeDefinition`、`RewriteRule`。
- **设计决策**：仅依赖 Go 标准库，被其他所有领域包单向引用，杜绝反向依赖。

### 3.2 `internal/cache` (缓存管理领域)
- **职责**：负责本地文件路径的跨平台安全转义、缓存目录生命周期维护以及免缓存规则链仲裁。
- **核心能力**：
  - `GetSafeUrlPath(rawUri string) string`：将请求格式化为合法相对路径，消除 Windows 冒号等非法字符，Query 哈希隔离。
  - `ShouldBypassCache(rawUri string, rules []config.NoCacheRule, clientPaths ...string) bool`：按规则链进行上下文作用域与正则匹配。
  - **白名单绝对优先机制**：命中 `Action == "cache"` 规则立即判定强制落盘，杜绝安装包被误杀免缓存。

### 3.3 `internal/ttl` (元数据软过期与 SWR 领域)
- **职责**：高并发内存元数据软缓存生命周期管理（Feature 318）。
- **核心数据结构**：
  - `TTLItem`：缓存元数据字节切片、克隆响应头、HTTP 状态码、ETag、Last-Modified 及过期时间戳。
  - `TTLManager`：面向对象读写锁管理器，彻底取代原 `globalTTLManager` 全局变量。

### 3.4 `internal/rewriter` (响应体流式重写领域)
- **职责**：针对 PyPI、npm 等官方源索引元数据执行 URL 动态原地重写（Feature 311）。
- **核心能力**：
  - `BodyRewriter`：封装重写规则集与类型映射字典。
  - `ShouldRewrite`：按作用域、文件后缀和 MIME 过滤。
  - `isBinaryMimeType` / `isKnownBinaryPath`：严格防污染拦截二进制内容。
  - `Apply`：透明支持 Gzip 流解压、`{PROXY_HOST}` 宏替换及重新压缩。

### 3.5 `internal/plugin` (路由作用域协议插件领域)
- **职责**：协议扩展点契约与领域优化插件管理（Feature 317）。
- **核心能力**：
  - `RoutePlugin` 接口：`Name() string`、`Match(req *http.Request, uri string) bool`、`FastPathServe(w http.ResponseWriter, r *http.Request, uri string, cacheDir string) bool`。
  - `DockerPlugin`：针对 Docker Registry v2 Blobs 执行高效本地缓存快路径直出。
  - `Registry`：实例化插件容器，支持路由作用域绑定与执行分发。

### 3.6 `internal/downloader` (并发下载领域)
- **职责**：实现高并发下载请求合并（Request Coalescing）、Leader/Follower 广播流水线与空闲超时看门狗。
- **核心类型**：
  - `DownloadJob`：并发任务状态载体，内含 `sync.Cond` 条件变量、Readers 活跃引用计数与超时取消 `context.CancelFunc`。
  - `Manager`：面向对象的任务池管理器，封装了 `jobs` 字典与 `jobsMu` 互斥锁，彻底消除包级全局变量。
- **核心机制**：
  - `TryRename(reqID uint64)`：当活跃 Readers 降为 0 时安全重命名；若任务出错，严禁重命名并自动删除残缺 `.tmp` 临时文件。
  - `StartWatchdog()`：后台巡检协程，自动终止停滞超时的挂起任务并唤醒 Followers 报错退出。

### 3.7 `internal/proxy` (协议代理领域)
- **职责**：实现标准 `http.Handler` 接口，协调完成协议层路由识别、隧道透传与分流下载。
- **核心能力**：
  - `handleConnect`：劫持 TCP 连接实现 HTTPS CONNECT 双向流中继。
  - `rewriteTargetURI`：支持标准绝对 URI、相对路径以及短别名（`alias`）重写。
  - `handleBypass`：动态资源流式直通（Zero Disk I/O）与 SWR 异步后台同步。
  - `handleRange`：HTTP Range 206 分片（本地命中 direct slice 直出，未命中透传上游）。
  - `handleDownload`：协调 Leader 边拉取边写盘、`sync.Cond` 广播与 Followers 共享读取。

### 3.8 主入口 `url-proxy.go`
重构后的主入口仅保留装配逻辑（由 1759 行缩减至 ~70 行）：
```go
func main() {
    flag.Parse()
    cfg := config.Load(*conf)
    cDir, err := cache.EnsureCacheDir(*cacheDir)
    if err != nil { log.Fatalf(...) }

    ttlMgr := ttl.NewManager()
    downMgr := downloader.NewManager(cDir, *idleTime)
    downMgr.StartWatchdog()
    defer downMgr.StopWatchdog()

    bodyRewriter := rewriter.NewBodyRewriter(cfg.Rewrites, cfg.Types)
    pluginRegistry := plugin.NewDefaultRegistry()

    proxyServer := proxy.NewServer(cfg, cDir, downMgr, ttlMgr, bodyRewriter, pluginRegistry)
    server := &http.Server{Addr: *addr, Handler: proxyServer}
    log.Fatal(server.ListenAndServe())
}
```

---

## 4. 单元测试与质量验证 (Testing & Verification)

每个 `internal` 子包均具备独立的原生 Go 单元测试套件：

### 4.1 测试覆盖明细
1. **`internal/config/config_test.go`**：验证单行指令语法解析、注释与空白行容错、非法指令安全跳过以及正则预编译。
2. **`internal/cache/cache_test.go`**：验证 Windows 路径安全性（冒号过滤）、Query 参数隔离以及白名单绝对优先仲裁。
3. **`internal/ttl/ttl_test.go`**：验证软过期判定、SWR 状态延长、并发读写与过期淘汰。
4. **`internal/rewriter/rewriter_test.go`**：验证 PEP 691 JSON 替换、HTML 标签重写、Gzip 解压重写与二进制防污染。
5. **`internal/plugin/plugin_test.go`**：验证 Docker Blobs 模式匹配、路由作用域隔离与快路径响应。
6. **`internal/downloader/downloader_test.go`**：验证 Leader/Follower 角色分配、Readers 互斥重命名、错误清理以及看门狗强制超时取消。
7. **`internal/proxy/proxy_test.go`**：验证短别名重写、本地缓存 Range 206 切片直出、免缓存直接流式回传以及 304 快速短路。

### 4.2 验证结果
在 WSL 环境下执行标准自动化测试：
```bash
go test -v ./...
```
输出结果显示各个子包测试全部一次性通过：
- `uri-proxy/internal/config`: PASS
- `uri-proxy/internal/cache`: PASS
- `uri-proxy/internal/ttl`: PASS
- `uri-proxy/internal/rewriter`: PASS
- `uri-proxy/internal/plugin`: PASS
- `uri-proxy/internal/downloader`: PASS
- `uri-proxy/internal/proxy`: PASS

### 4.3 编译与构建兼容性
- `go build .`：顺利编译为单二进制文件。
- `build.bat` 与 `build.sh`：直接兼容多系统多架构交叉编译。
- `Dockerfile`：容器构建流程无需任何修改。

---

## 5. 收益与总结 (Benefits & Summary)

| 指标维度 | 重构前 (Pre-Refactoring) | 重构后 (Post-Refactoring) | 优化效益 |
| :--- | :--- | :--- | :--- |
| **入口文件代码行数** | 1759 行 | ~70 行 | 行数缩减 **96%**，主入口极度轻量清晰 |
| **职责耦合度** | 18 项特性逻辑全部混杂于单个源文件 | 7 个独立领域子包各司其职 | 符合单一职责原则，易于扩展与排障 |
| **状态并发安全** | 5 处包级全局变量与全局互斥锁 | 面向对象结构体实例封装与依赖注入 | 彻底消除全局变量污染，生命周期可控 |
| **单元测试支持** | 仅依赖外部手工/Mock 端到端黑盒测试 | 各子包自带原生白盒 Go 单元测试 | 支持 `go test ./...` 快速持续集成 |
| **生态构建兼容性** | 单一主文件构建 | 保持根目录 `url-proxy.go` 入口 | 现有构建脚本、Docker 与运行命令零破坏 |
