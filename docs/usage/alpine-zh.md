# Alpine Linux (apk) 缓存代理使用指南

`url-proxy` 原生 100% 完美支持 Alpine Linux 的 `apk` 软件包管理器。与 APT 架构完全同构，Alpine 的核心元数据为 `APKINDEX.tar.gz`，软件包实体为 `.apk` 文件。

---

## 1. 服务端配置 (`proxy.conf`)

在 `url-proxy` 的配置文件 `proxy.conf` 中追加 Alpine 规则：

```ini
# --- 声明上游 Alpine 官方镜像别名 ---
alias /alpine https://dl-cdn.alpinelinux.org/alpine

# --- 动静分离规则：动态索引包实时透传 (Zero Disk I/O) ---
bypass /alpine/ exact APKINDEX.tar.gz

# --- 强制缓存规则：.apk 二进制软件包 100% 永久安全落盘 ---
cache  /alpine/ suffix .apk
```

启动 `url-proxy`：
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. 客户端配置

在 Alpine 宿主机或 Docker 镜像构建中配置仓库源。

### 方式 A：修改 `/etc/apk/repositories`（推荐，构建 Dockerfile 零证书依赖）

将官方域名替换为代理地址与 `/alpine` 短别名：

```text
http://192.168.1.100:8888/alpine/v3.19/main
http://192.168.1.100:8888/alpine/v3.19/community
```

**在 Dockerfile 中的最佳实践**：
```dockerfile
FROM alpine:3.19
# 将源指向内网代理，构建速度飞升
RUN sed -i 's|https://dl-cdn.alpinelinux.org/alpine|http://192.168.1.100:8888/alpine|g' /etc/apk/repositories \
    && apk add --no-cache curl git ca-certificates
```

### 方式 B：设置 HTTP 环境变量代理

若上游直接为 HTTP 源，可在终端或 CI 流水线注入环境变量：
```bash
export http_proxy="http://192.168.1.100:8888"
apk update && apk add curl
```

---

## 3. 运行与验证

在 Alpine 系统中执行：

```bash
# 1. 更新仓库索引 (流式透传 APKINDEX.tar.gz，不落地)
apk update

# 2. 安装软件包 (首次缓存，后续直接命中本地磁盘缓存)
apk add curl
```

**加速效益**：
- 在 CI/CD 中多次构建 `alpine` 基础镜像时，相同版本的 `.apk` 包将实现 100% 局域网内网瞬时传输，构建耗时从数十秒降至秒级。
