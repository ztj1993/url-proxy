# Rust Cargo 稀疏索引与 Crates 官方源闭环代理指南 (Cargo Caching Guide)

本文档介绍如何配置 `url-proxy` 作为 Rust 官方 Crates.io 稀疏索引（Sparse Index Protocol, RFC 2785）与源码包（`https://static.crates.io`）的高性能本地/局域网缓存代理，并提供客户端快速接入配置。

---

## 1. 原理与架构 (Background & Principle)

自 Rust 1.68 起，Cargo 全面推荐使用**稀疏索引协议（RFC 2785）**代替传统的 Git 全量索引仓库：
1. **轻量按需索引查询**：Cargo 仅通过 HTTP GET 按需拉取项目依赖包的元数据（NDJSON 格式，如 `GET /crates-index/ra/nd/rand`），体积仅数 KB，支持 304 条件协商实时感知最新版本；
2. **源码包下载端点声明 (`config.json`)**：
   - 官方索引根路径包含 `config.json`，其中显式声明了源码包的下载模板：`"dl": "https://static.crates.io/crates"`；
   - 若直接透传该配置，Cargo 客户端在解析索引后仍会直接跨域连接外网 `static.crates.io` 下载源码包，导致代理无法捕获流量；
3. **闭环代理机制 (Closed-Loop Proxy)**：
   - `url-proxy` 通过 **Feature 311 动态响应体重写引擎**，在向客户端返回 `config.json` 时，自动将 `"dl"` 替换为本地代理地址：`"dl": "http://<proxy-host>:<port>/crates-static/crates"`；
   - Cargo 随后拉取的所有 `.crate` 源码包（不可变 tar.gz 压缩包）将 100% 回流至代理节点的 `/crates-static/` 别名路由；
   - 代理实施 **Single-Flight 并发请求合并** 与 **本地磁盘永久强缓存**，后续团队多机器或 CI 构建下载同一依赖时直接命中本地磁盘，实现 **0 外部带宽消耗** 与 **内网满速拉取**。

```text
[cargo CLI] --- (GET /crates-index/config.json) ---> [url-proxy] ---> [index.crates.io]
     ▲                                                    │                   │
     │                                           [动态响应体重写引擎]         │
     │                                 (将 dl 替换为代理 /crates-static/crates)
     │                                                    │                   │
     └───────── (返回重写后的 config.json) ───────────────┴───────────────────┘
     │
     ▼ (查询特定包版本索引: 动态直通与 304 快速协商)
[cargo CLI] --- (GET /crates-index/ra/nd/rand) ----> [url-proxy] ---> [index.crates.io]
     │
     ▼ (拉取 .crate 依赖包: 自动回流代理并强缓存)
[cargo CLI] --- (GET /crates-static/crates/rand/rand-0.8.5.crate) ---> [url-proxy]
                                                                           │
                                                               [本地磁盘 100% 强缓存]
                                                               [Single-Flight 并发合并]
                                                               [Range 206 断点分片直出]
```

---

## 2. 服务端配置 (`proxy.conf`)

在 `url-proxy` 的配置文件 `proxy.conf` 中追加以下规则：

```ini
# ==============================================================================
# 7. Rust Cargo 稀疏索引与 Crates 官方源闭环代理 (Feature 315)
# 客户端配置: 在 ~/.cargo/config.toml 中配置 registry = "sparse+http://<proxy>:8888/crates-index/"
# ==============================================================================
alias crates-index https://index.crates.io
alias crates-static https://static.crates.io

# config.json 动态响应体重写: 将 crates 下载端点闭环重定向至本地代理
rewrite /crates-index/config.json type:json https://static.crates.io/crates http://{PROXY_HOST}/crates-static/crates

# 稀疏索引元数据动态免缓存 (纯流式透传，严格支持 304 条件协商)
bypass /crates-index/ *

# Crates 源码包 100% 永久强缓存
cache /crates-static/ suffix .crate
cache * suffix .crate
```

---

## 3. 客户端接入配置 (Client Configuration)

假设 `url-proxy` 运行在局域网地址 `192.168.1.100:8888`。

### 3.1 全局用户配置 (`~/.cargo/config.toml`)

编辑当前用户的 Cargo 配置文件 `~/.cargo/config.toml`（Windows 下为 `%USERPROFILE%\.cargo\config.toml`）：

```toml
[source.crates-io]
replace-with = "url-proxy-sparse"

[source.url-proxy-sparse]
registry = "sparse+http://192.168.1.100:8888/crates-index/"
```

> [!TIP]
> 必须使用 `sparse+http://` 前缀显式指示 Cargo 使用 HTTP 稀疏协议而非传统 Git 索引。

### 3.2 项目级独立配置 (`.cargo/config.toml`)

若仅希望特定项目通过代理拉取依赖，可在该 Rust 项目根目录下创建 `.cargo/config.toml`，写入相同内容即可生效。

### 3.3 CI/CD 与容器环境变量配置

在 Dockerfile 或 CI 流水线环境中，无需修改配置文件，直接注入环境变量：

```bash
export CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse
export CARGO_SOURCE_CRATES_IO_REPLACE_WITH=url-proxy-sparse
export CARGO_SOURCE_URL_PROXY_SPARSE_REGISTRY="sparse+http://192.168.1.100:8888/crates-index/"
```

---

## 4. 验证与排错 (Verification)

### 4.1 手动验证 `config.json` 重写
执行以下命令检查代理返回的 `config.json`：
```bash
curl -s http://192.168.1.100:8888/crates-index/config.json
```
**预期输出**：
```json
{
  "dl": "http://192.168.1.100:8888/crates-static/crates",
  "api": "https://crates.io"
}
```
`"dl"` 字段已成功重定向为代理节点的 `/crates-static/crates` 地址。

### 4.2 真实项目构建验证
在一个 Rust 测试项目中拉取依赖并编译：
```bash
cargo new hello-cargo && cd hello-cargo
cargo add rand tokio serde
cargo build
```
观察 `url-proxy` 服务端日志：
- `crates-index/ra/nd/rand`: 显示 `dynamic body rewrite target` 或 `cache bypass`；
- `crates-static/crates/rand/...`: 显示 `job leader` 并完成本地磁盘原子重命名持久化。

### 4.3 离线与缓存加速验证
清理客户端本地编译与缓存：
```bash
rm -rf target/
rm -rf ~/.cargo/registry/cache/*
cargo build
```
**验证结果**：所有 `.crate` 压缩包以千兆内网线速瞬间下载完成，代理日志显示 `range hit local cache` 或本地缓存直出，**0 外部上游带宽占用**。
