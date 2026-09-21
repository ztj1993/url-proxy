# APT (Debian / Ubuntu) 缓存代理使用指南

`url-proxy` 原生完整支持 Debian、Ubuntu 等基于 APT 包管理系统的代理与缓存。

---

## 1. 服务端配置 (`proxy.conf`)

在 `url-proxy` 的配置文件 `proxy.conf` 中添加 APT 规则：

```ini
# --- 声明上游别名路由 (支持 HTTP 或 HTTPS 上游) ---
alias /ubuntu http://archive.ubuntu.com/ubuntu
alias /debian http://deb.debian.org/debian
alias /ubuntu-security http://security.ubuntu.com/ubuntu

# --- 动静分离规则：动态元数据实时透传 (Zero Disk I/O)，严禁落盘 ---
bypass /ubuntu/dists/ prefix Packages
bypass /ubuntu/dists/ exact InRelease
bypass /ubuntu/dists/ exact Release
bypass /ubuntu/dists/ suffix .gpg
bypass /ubuntu/dists/ suffix .diff/Index

bypass /debian/dists/ prefix Packages
bypass /debian/dists/ exact InRelease
bypass /debian/dists/ exact Release
bypass /debian/dists/ suffix .gpg

# --- 白名单规则：软件包文件 100% 强制持久化缓存 ---
cache /ubuntu/ suffix .deb
cache /ubuntu/ suffix .udeb
cache /debian/ suffix .deb
cache /debian/ suffix .udeb
```

启动 `url-proxy` 服务（假设代理监听 `192.168.1.100:8888`）：
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. 客户端配置

APT 客户端有两种接入模式，任选其一即可：

### 模式 A：路径短别名模式（推荐，客户端 sources.list 最干净）

直接修改客户端机器的 `/etc/apt/sources.list`（或 `/etc/apt/sources.list.d/ubuntu.sources`），将原镜像域名替换为代理服务的 IP 和短别名：

```sources.list
# Ubuntu 22.04 (Jammy) 示例
deb http://192.168.1.100:8888/ubuntu jammy main restricted universe multiverse
deb http://192.168.1.100:8888/ubuntu jammy-updates main restricted universe multiverse
deb http://192.168.1.100:8888/ubuntu-security jammy-security main restricted universe multiverse
```

> [!TIP]
> **免安装根证书优势**：上游即使配置为官方 HTTPS 镜像，客户端以普通明文 HTTP 连接内网代理，代理端向上游发起原生 HTTPS 连接。由于 APT 原生基于 GPG 签名机制校验完整性，因此在内网使用 HTTP 接入不会导致安全性降级，同时免除了在客户端安装自签名 CA 根证书的繁琐工作。

### 模式 B：标准正向代理协议模式

保持原有 `/etc/apt/sources.list` 内容不变（适用于上游源本身为 `http://`），在客户端创建 `/etc/apt/apt.conf.d/01proxy`：

```apt.conf
Acquire::http::Proxy "http://192.168.1.100:8888";
```

---

## 3. 运行与验证

在客户端执行更新与安装测试：

```bash
# 1. 更新索引：通过 If-None-Match 透传，未变动时直接 304 短路，不落地脏缓存
sudo apt-get update

# 2. 安装软件包：首次下载落盘，后续相同包秒级命中本地缓存
sudo apt-get install -y --download-only curl
```

**服务端日志观察**：
- 请求 `/ubuntu/dists/.../InRelease`：日志标记为 `[BYPASS]`，直接流式透传；
- 请求 `/ubuntu/pool/.../curl_*.deb`：首次下载显示 `[CACHE-MISS]` 并写入本地存储；并发或第二次请求显示 `[CACHE-HIT]`，0 上游流量消耗。
