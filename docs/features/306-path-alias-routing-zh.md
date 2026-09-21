# 路径短别名路由与动态重写设计方案

## 1. 功能目标 (Goal)

在传统的路径前缀代理模式下，APT 客户端必须在 `/etc/apt/sources.list` 中配置冗长且暴露公网源站地址的 URL：
```text
deb http://<proxy-ip>:8888/http://archive.ubuntu.com/ubuntu jammy main
```
这种方式存在以下痛点：
1. **配置臃肿易错**：包含两层 `http://` 结构，运维人员与开发人员配置源时容易拼接错误；
2. **缺乏私有镜像站体验**：无法像私有或企业级镜像源（如 Nexus、Artifactory、清华源）那样使用清爽短路径（如 `deb http://proxy:8888/ubuntu jammy main`）；
3. **上游源变更成本高**：若公网源发生变动（例如从官方源迁移至国内镜像或内网网关），所有客户端机器必须全量更新 `sources.list`。

**Feature 306 建设目标**：
在统一配置文件 `proxy.conf` 中引入 `alias` 单行指令，支持按第一级请求路径进行短别名映射与 URL 动态重写：
- 客户端只需配置简洁别名：`deb http://<proxy-ip>:8888/ubuntu jammy main`；
- 代理服务端透明重写至真实上游 BaseURL 并继续执行后续的缓存、免缓存、Range 续传与 304 条件协商管线；
- 原生完整路径模式（`http://proxy:8888/http://...`）与标准正向代理模式（`Acquire::http::Proxy`）依然 100% 保持兼容。

---

## 2. 核心架构与重写流水线 (Architecture & Pipeline)

### 2.1 配置文件语法规范 (`proxy.conf`)

通过统一单行指令声明路径别名：
```text
# 语法: alias <短别名> <上游 BaseURL>
alias ubuntu http://archive.ubuntu.com/ubuntu
alias ubuntu-security http://security.ubuntu.com/ubuntu
alias debian https://deb.debian.org/debian
alias debian-security https://deb.debian.org/debian-security
```

### 2.2 动态重写与请求分流引擎

当收到客户端发起的 HTTP 请求时，执行以下决策链路：

```
                    收到客户端 HTTP 请求
                             │
                             ▼
              URI 是否为标准绝对协议 (http/https/ftp)?
                            / \
                      是   /   \  否 (非绝对协议或本地路径模式)
                          /     \
                         ▼       ▼
                 [原生代理管线]  提取第一级路径: aliasKey = parts[0]
                                 │
                                 ▼
                     aliasKey 存在于 config.Aliases?
                                / \
                          是   /   \  否
                              /     \
                             ▼       ▼
              [规范化拼接重写为绝对 URI]   [返回 404 Not Found]
              (uri = targetBase + subPath)
                             │
                             ▼
              进入后续标准处理管线:
              1. 免缓存判定 (shouldBypassCache)
              2. 304 条件协商 (If-None-Match / ETag)
              3. Range 切片与断点续传 (206 Partial Content)
              4. 任务合并并发下载与磁盘安全落盘 (SafeUrlPath)
```

### 2.3 规范化斜杠拼接与参数保留算法
重写引擎严格遵循路径规范化逻辑：
1. 剥离第一级短别名，获取后续子路径 `subPath`；
2. 剥离 `targetBase` 尾部多余的 `/`，剥离 `subPath` 前部多余的 `/`，使用标准单个 `/` 进行拼接：
   ```go
   rewrittenUri := strings.TrimRight(targetBase, "/")
   if len(parts) > 1 && parts[1] != "" {
       rewrittenUri += "/" + strings.TrimLeft(parts[1], "/")
   } else if strings.HasSuffix(r.URL.Path, "/") {
       rewrittenUri += "/"
   }
   ```
3. 若请求附带查询参数（`r.URL.RawQuery != ""`），原样追加于重写 URI 末尾；
4. 校验重写后的协议是否合法（必须为 `http` 或 `https`），若非法则直接阻断并返回 `400 Bad Request`。

---

## 3. 技术优势与深度组合 (Technical Benefits)

1. **缓存无缝共享与零重复占用**：
   - 动态重写在请求入口处最先完成，后续的磁盘安全缓存路径（`SafeUrlPath`）均基于重写后的真实上游 URL 计算。
   - 这意味着无论客户端通过短别名访问（`/ubuntu/pool/.../pkg.deb`），还是通过原路径访问（`/http://archive.ubuntu.com/ubuntu/pool/.../pkg.deb`），在本地磁盘上**严格命中同一份缓存文件**，不会造成磁盘空间浪费或重复下载。
2. **免缓存与防误杀策略天然继承**：
   - 重写后的绝对 URI 无缝流经 Feature 305 的 `shouldBypassCache` 判定；
   - 规则文件中的 `bypass /dists/ exact InRelease` 以及 `cache * suffix .deb` 依然精准生效。
3. **运维集中管控**：
   - 上游官方源发生迁移、切换内网镜像或加速节点时，仅需修改代理端的 `proxy.conf`，全网所有客户端源配置无需任何变动。

---

## 4. 客户端配置落地指南 (Client Configuration)

### 4.1 Ubuntu 客户端配置样例 (`/etc/apt/sources.list`)

```text
# 基于 url-proxy 短别名路由模式
deb http://proxy-server:8888/ubuntu jammy main restricted universe multiverse
deb http://proxy-server:8888/ubuntu jammy-updates main restricted universe multiverse
deb http://proxy-server:8888/ubuntu-security jammy-security main restricted universe multiverse
```

### 4.2 Debian 客户端配置样例 (`/etc/apt/sources.list`)

```text
# 基于 url-proxy 短别名路由模式
deb http://proxy-server:8888/debian bookworm main non-free-firmware
deb http://proxy-server:8888/debian bookworm-updates main non-free-firmware
deb http://proxy-server:8888/debian-security bookworm-security main non-free-firmware
```

---

## 5. 验证与测试标准 (Verification)

1. **短别名重写与免缓存动态直通**：通过短别名请求 `/my-ubuntu/dists/jammy/InRelease`，验证响应来自上游且磁盘 0 字节落盘；
2. **短别名重写与软件包落盘缓存**：通过短别名请求 `/my-ubuntu/pool/.../pkg.deb`，验证返回 200 OK 且本地磁盘成功生成对应缓存文件；
3. **短别名 304 条件协商**：携带 `If-None-Match` 发起别名请求，验证上游 304 被直接短路透传；
4. **多别名隔离性**：不同别名（如 `my-ubuntu` 与 `my-debian`）准确路由至不同上游源；
5. **未知别名错误防御**：请求未在配置中声明的别名路径，服务端安全返回 `404 Not Found`。
