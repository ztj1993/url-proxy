# Docker / OCI 镜像 Registry v2 协议中继与分层缓存使用指南 (Docker Caching Guide)

本文档介绍如何配置 `url-proxy` 作为 Docker / OCI 官方镜像仓库（`registry-1.docker.io`）的高性能分层缓存代理，以及各类容器运行时（Docker Daemon、Containerd / nerdctl、Podman）的接入配置。

---

## 1. 原理与架构 (Background & Principle)

Docker 客户端在拉取镜像（`docker pull`）时遵循 OCI / Registry v2 API 规范：
1. **401 Bearer Token 握手挑战**：拉取前向服务端发送 `GET /v2/` 探测，官方 Registry 返回 `401 Unauthorized` 并在 `Www-Authenticate` 响应头中指示认证授权服务器（如 `realm="https://auth.docker.io/token"`）；
2. **易变镜像清单（Manifests）**：客户端获取 Token 后请求 `GET /v2/<name>/manifests/<tag>`，清单描述了镜像的元数据和所有 Layer 分层 SHA256；清单具有时效性，通过 304 条件协商与动态透传保证版本最新；
3. **不可变分层数据（Layer Blobs）**：镜像分层数据路径格式为 `/v2/<name>/blobs/sha256:<digest>`，内容具有强加密哈希不可变性，单层体积达数十 MB 至数 GB。

`url-proxy` 通过 **Feature 314** 提供原生的 Registry v2 协议中继与分层缓存加速：
```text
[Docker Daemon] --- (GET /v2/) ---> [url-proxy] --- (GET /v2/) ---> [registry-1.docker.io]
       ▲                                  │                                    │
       │                                  │ (透传 401 + Www-Authenticate)      │
       └────── (返回 401 挑战信息) ───────┴────────────────────────────────────┘
       │
       ▼ (客户端向官方 auth.docker.io 换取拉取 Token)
[Docker Daemon] --- (GET /v2/.../manifests/... + Token) ---> [url-proxy] ---> [Registry]
                                                                  │ (动态透传 / 304 协商)
                                                                  ▼
[Docker Daemon] --- (HEAD /v2/.../blobs/sha256:...) --------> [url-proxy]
                                                                  │
                                                        [本地已缓存? 直接 200 OK 直出]
                                                        [未缓存? 透传上游探测]
                                                                  │
[Docker Daemon] --- (GET /v2/.../blobs/sha256:...) ---------> [url-proxy]
                                                                  │
                                                        [未命中: 自动跟随 307 重定向至 CDN]
                                                        [单飞合并 Single-Flight + 原子落盘]
                                                        [已命中: 高速直出 / Range 206 续传]
```

---

## 2. 服务端配置 (`proxy.conf`)

在 `url-proxy` 的配置文件 `proxy.conf` 中追加以下规则即可生效：

```ini
# ==============================================================================
# 6. Docker / OCI 镜像 Registry v2 协议中继与分层缓存 (Feature 314)
# Daemon Mirror: "registry-mirrors": ["http://<proxy>:8888"]
# 路径短别名模式: docker pull <proxy>:8888/docker/library/ubuntu:22.04
# ==============================================================================
alias v2 https://registry-1.docker.io/v2
alias docker https://registry-1.docker.io

# Docker Registry 握手探测与 Manifest 元数据动态免缓存 (纯流式透传，严格防陈旧)
bypass /v2/ exact v2
bypass /v2/ exact index
bypass /v2/ contains /manifests/
bypass /docker/v2/ exact v2
bypass /docker/v2/ exact index
bypass /docker/v2/ contains /manifests/

# Docker 镜像分层 Layer Blob 强制本地永久落盘缓存 (SHA256 内容寻址不可变对象)
cache /v2/ contains /blobs/
cache /docker/v2/ contains /blobs/
```

---

## 3. 客户端接入配置 (Client Configuration)

假设 `url-proxy` 运行在局域网地址 `192.168.1.100:8888`。

### 3.1 方案 A：Docker Daemon 全局镜像加速 (推荐)

在客户端宿主机的 `/etc/docker/daemon.json` 中配置镜像加速器与不安全仓库允许列表：

```json
{
  "registry-mirrors": ["http://192.168.1.100:8888"],
  "insecure-registries": ["192.168.1.100:8888"]
}
```

重新加载并重启 Docker 服务：
```bash
sudo systemctl daemon-reload
sudo systemctl restart docker
```

拉取官方镜像，与原生官方拉取完全一致，无需更改镜像名称：
```bash
docker pull ubuntu:22.04
docker pull alpine:latest
docker pull redis:alpine
```

### 3.2 方案 B：显式路径短别名拉取

若不方便修改 Docker Daemon 全局配置文件，可以在拉取时直接在镜像名前加上代理主机前缀：

在 `/etc/docker/daemon.json` 中声明不安全仓库：
```json
{
  "insecure-registries": ["192.168.1.100:8888"]
}
```

重启 Docker 服务后执行：
```bash
# 格式: docker pull <proxy-host>:<port>/docker/<namespace>/<image>:<tag>
docker pull 192.168.1.100:8888/docker/library/ubuntu:22.04
docker pull 192.168.1.100:8888/docker/library/alpine:latest
```

### 3.3 Containerd / nerdctl / K8s CRI 配置

编辑 `/etc/containerd/config.toml`，在 `plugins."io.containerd.grpc.v1.cri".registry.mirrors` 中配置：

```toml
[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://192.168.1.100:8888"]
```

重启 containerd：
```bash
sudo systemctl restart containerd
```

使用 `nerdctl` 或 `crictl` 拉取：
```bash
nerdctl pull ubuntu:22.04
```

### 3.4 Podman 配置

在 `/etc/containers/registries.conf` 中添加：

```toml
[[registry]]
prefix = "docker.io"
location = "docker.io"

[[registry.mirror]]
location = "192.168.1.100:8888"
insecure = true
```

---

## 4. 验证与诊断 (Verification)

### 4.1 手动探测与握手验证
```bash
# 验证 /v2/ 探测透传 401 与 Www-Authenticate
curl -i http://192.168.1.100:8888/v2/

# 预期输出:
# HTTP/1.1 401 Unauthorized
# Www-Authenticate: Bearer realm="https://auth.docker.io/token",service="registry.docker.io"
# Docker-Distribution-Api-Version: registry/2.0
```

### 4.2 观察缓存目录与分层命中
在代理服务器上查看缓存目录（如 `./cache/https-registry-1.docker.io/v2/`）：
```bash
ls -lh ./cache/https-registry-1.docker.io/v2/library/ubuntu/blobs/
```
可以看到类似 `sha256%3A7b1a8c9e...` 的分层数据文件，文件权限和大小与官方分层一致。

### 4.3 性能与加速效果
当集群多台机器同时拉取同一镜像或 CI 构建重复运行时：
- **首次拉取**：由一个 Leader 连接 Docker Hub 并原子写入本地磁盘，并发的其他节点作为 Follower 实时共享流式数据（Single-Flight 并发合并）；
- **二次拉取**：Docker 客户端发送 `HEAD` 探测或 `GET` 请求时，`url-proxy` 直接从本地 NVMe/SSD 高速直出，实现 **0 外部带宽消耗**与 **内网线速满速下载**。
