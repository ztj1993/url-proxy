# Release Notes Generation Specification

## 1. Objective & Scope
This specification standardizes the process and format for generating release notes and changelogs when creating Git tags or project releases for `url-proxy`. It ensures consistency, traceability, and developer convenience across releases.

---

## 2. Core Principles

1. **Incremental Comparison Principle**:
   - Release notes must be strictly derived from the Git commit differences between the target tag and the preceding tag (e.g., `git log <previous-tag>..<current-tag>`).
   - Content must accurately reflect verified code and documentation changes, highlighting new features and bug fixes without speculative or unsubstantiated claims.
2. **Copy-Friendly Principle**:
   - The final release notes output must be fully wrapped in a Markdown code block (using ````markdown ... ````) to prevent client UI renderers from converting it into rich text, allowing users to copy the raw Markdown in a single click.
3. **Path and Formatting Conventions**:
   - **No absolute paths or protocol URLs**: Do not use drive letters, host paths, WSL mount paths, or `file://` protocols.
   - **Relative paths only**: All references to project documentation, test suites, or configurations must use relative paths from the repository root (e.g., `tests/304_unified_config/`).
   - **Inline code formatting without links**: Mention code files, configs, and symbols in inline code backticks (e.g., `url-proxy.go`, `proxy.conf`) without hyperlink wrappers.

---

## 3. Standard Structure

The release notes comprise three required sections:

1. **One-sentence Summary**:
   - Located at the very top line.
   - Summarizes the overall positioning, core milestone evolution, and key capabilities of the release.
2. **`### 新增功能` / `### New Features`**:
   - Enumerates new capabilities, protocol support, and test coverage introduced compared to the previous tag.
   - Structured by feature modules, ideally referencing design feature identifiers (e.g., Feature 301).
3. **`### 修复缺陷` / `### Bug Fixes`**:
   - Enumerates bug fixes, concurrency conflict resolutions, edge-case handlings, and compatibility improvements.
   - Clearly describes the problem scenario and resolution.

---

## 4. Standard Template

````markdown
`url-proxy` <version> <one-sentence summary of the release including milestone positioning>.

### 新增功能

- **<Feature Name> (<Feature ID>)**:
  - <Key capability 1>
  - <Key capability 2>

### 修复缺陷

- **<Bug fix description>**: <Scenario, root cause, and resolved behavior>
````

---

## 5. Reference Example (0.1.0)

````markdown
`url-proxy` 0.1.0 全面升级为支持标准 HTTP 正向代理协议与统一配置引擎的高性能代理服务，新增 Range 断点续传、304 快速协商、精细化免缓存规则以及短路径别名路由，深度适配 APT 等包管理软件源加速场景。

### 新增功能

- **标准 HTTP 正向代理与 HTTPS CONNECT 隧道 (Feature 301)**：
  - 支持请求行绝对 URI 解析，允许客户端直接通过标准代理环境变量（如 `http_proxy`）或原生代理配置透明接入。
  - 支持基于连接劫持的 HTTPS CONNECT 双向透明 TCP 隧道流式中继。
  - 100% 保持与原有路径前缀代理模式（`/http://...`）的双模兼容。
- **条件请求与 304 快速协商透传 (Feature 302)**：
  - 向上游请求透传客户端的 `If-Modified-Since` 与 `If-None-Match` 验证头部。
  - 拦截上游 304 Not Modified 响应并直接短路返回，零响应体传输，完全绕过磁盘 I/O 写入开销。
- **HTTP Range 分片请求与 206 断点续传 (Feature 303)**：
  - 本地缓存命中时直接通过 `http.ServeFile` 毫秒级分发指定 Byte Range，实现零上游网络流量消耗。
  - 缓存未命中时透明透传 Range 与校验头（如 `If-Range`）流式回传客户端，并默认在响应头中宣告 `Accept-Ranges: bytes`。
- **统一代理配置系统 `proxy.conf` (Feature 304)**：
  - 建立集中式规则配置系统，支持单行显式动词指令（`forward`、`alias`、`bypass`、`cache`）。
  - 新增 `-conf` 命令行参数，支持从指定路径、当前工作目录及可执行文件所在目录多级自动发现配置文件。
- **细粒度免缓存旁路与防误判仲裁引擎 (Feature 305)**：
  - 支持配置动态元数据（如索引清单、签名文件）直通不写缓存，保障软件源元数据实时性。
  - 支持白名单与黑名单优先级仲裁，精准支持后缀匹配、完整路径精确匹配及路径前缀匹配。
- **路径短别名路由与动态 URL 重写 (Feature 306)**：
  - 支持 `alias <别名路径> <目标BaseURL>` 指令，支持通过优雅的短路径映射并重写至上游完整镜像地址。
- **APT 软件源生态集成与自动化测试套件 (Feature 307)**：
  - 针对 Debian/Ubuntu APT 包管理器场景提供短别名与标准正向代理双接入模式。
  - 配套交付完整的端到端自动化测试验证套件与 Mock 软件源（`tests/307_apt_integration/`）。

### 修复缺陷

- **修复 Range 分片请求与并发合并队列冲突导致的流污染**：将 Range 分片请求剥离出并发下载合并共享队列进行独立直通处理，避免多客户端不同偏移量读取相互干扰。
- **修复条件请求返回 304 时误损坏既有缓存文件的问题**：修复上游返回 304 协商响应时错误触发写盘覆写的问题，确保已缓存文件的完整性得到持久保留。
- **消除旧版域名转发隐式语法导致的规则误判与安全歧义**：废弃无指令动词的历史配置文件 `forwarding.conf`（Feature 103），将转发逻辑平滑收敛至确定性语法解析引擎，消除路径前缀匹配中的歧义风险。
````