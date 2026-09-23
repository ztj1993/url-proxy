# url-proxy 常用包管理器与系统源代理使用指南 (Usage Guides)

`url-proxy` 能够作为多种包管理器与操作系统软件源的高性能本地/局域网缓存代理网关。通过结合**请求合并（Single-Flight）**、**安全落盘路径（SafeUrlPath）**、**动静分离规则引擎（bypass/cache）**与**协议高阶特性（304 快速协商 / Range 206 断点分片）**，实现零重复外网流量浪费与秒级构建提速。

---

## 1. 软件源支持状态一览 (Support Matrix)

| 软件源 / 生态 | 协议规范 | 动态索引 (免缓存/304透传) | 静态包 (100%永久缓存) | 当前支持状态 | 使用文档 |
| :--- | :--- | :--- | :--- | :---: | :--- |
| **APT (Debian / Ubuntu)** | APT 镜像规范 | `InRelease`, `Release`, `Packages*` | `.deb`, `.udeb` | **100% 原生支持** | [APT 使用指南](apt-zh.md) |
| **Alpine Linux** | APK 仓库规范 | `APKINDEX.tar.gz` | `.apk` | **100% 原生支持** | [Alpine APK 使用指南](alpine-zh.md) |
| **Go Modules** | GOPROXY 协议 | `/@v/list` (版本列表) | `.zip`, `.mod`, `.info` | **100% 原生支持** | [Go GOPROXY 使用指南](goproxy-zh.md) |
| **Maven / Gradle** | Maven 2 仓库布局 | `maven-metadata.xml` | `.jar`, `.aar`, `.pom` | **100% 原生支持** | [Maven 使用指南](maven-zh.md) |
| **PyPI (Python)** | PEP 503 / 691 | `/simple/<pkg>/` | `.whl`, `.tar.gz` | **100% 完整支持**<br>*(Feature 312 闭环代理)* | [PyPI 使用指南](pypi-zh.md) |
| **npm (Node.js)** | npm Registry 规范 | `/<pkg>` (包元数据 JSON) | `/-/*.tgz` | **100% 完整支持**<br>*(Feature 313 闭环代理)* | [npm 使用指南](npm-zh.md) |
| **Docker / OCI** | Registry v2 API | `/v2/.../manifests/` | `/v2/.../blobs/` | **100% 完整支持**<br>*(Feature 314 分层缓存)* | [Docker 使用指南](docker-zh.md) |
| **Rust Cargo** | Sparse Index (RFC 2785) | `/crates-index/...` | `crates/*/*.crate` | **100% 完整支持**<br>*(Feature 315 闭环代理)* | [Cargo 使用指南](cargo-zh.md) |

---

## 2. 统一配置快速入门 (`proxy.conf`)

`url-proxy` 支持单文件集中声明所有上游源的别名与动静分离规则：

```ini
# ==========================================
# 1. APT (Ubuntu / Debian) 代理规则
# ==========================================
alias /ubuntu http://archive.ubuntu.com/ubuntu
bypass /ubuntu/dists/ prefix Packages
bypass /ubuntu/dists/ exact InRelease
bypass /ubuntu/dists/ exact Release
bypass /ubuntu/dists/ suffix .gpg
cache  /ubuntu/ suffix .deb

# ==========================================
# 2. Alpine Linux (apk) 代理规则
# ==========================================
alias /alpine https://dl-cdn.alpinelinux.org/alpine
bypass /alpine/ exact APKINDEX.tar.gz
cache  /alpine/ suffix .apk

# ==========================================
# 3. Go Modules (GOPROXY) 代理规则
# ==========================================
alias /goproxy https://goproxy.cn
bypass /goproxy/ suffix /@v/list
cache  /goproxy/ suffix .zip
cache  /goproxy/ suffix .mod
cache  /goproxy/ suffix .info

# ==========================================
# 4. Maven 仓库 (Central) 代理规则
# ==========================================
alias /maven https://repo1.maven.org/maven2
bypass /maven/ exact maven-metadata.xml
bypass /maven/ suffix maven-metadata.xml.sha1
cache  /maven/ suffix .jar
cache  /maven/ suffix .pom
cache  /maven/ suffix .aar

# ==========================================
# 5. PyPI (Python) 官方闭环代理规则 (Feature 312)
# ==========================================
alias pypi https://pypi.org
alias pythonhosted https://files.pythonhosted.org
rewrite /pypi/simple/ type:pypi https://files.pythonhosted.org/ http://{PROXY_HOST}/pythonhosted/
bypass /pypi/simple/ prefix *
cache /pythonhosted/ suffix .whl
cache /pythonhosted/ suffix .tar.gz
cache /pythonhosted/ suffix .zip

# ==========================================
# 6. npm (Node.js) 官方闭环代理规则 (Feature 313)
# ==========================================
alias npm https://registry.npmjs.org
rewrite /npm/ type:npm https://registry.npmjs.org/ http://{PROXY_HOST}/npm/
cache /npm/ suffix .tgz

# ==========================================
# 7. Docker / OCI 镜像 Registry v2 规则 (Feature 314)
# ==========================================
alias v2 https://registry-1.docker.io/v2
alias docker https://registry-1.docker.io
bypass /v2/ exact v2
bypass /v2/ contains /manifests/
cache /v2/ contains /blobs/

# ==========================================
# 8. Rust Cargo 稀疏索引与 Crates 规则 (Feature 315)
# ==========================================
alias crates-index https://index.crates.io
alias crates-static https://static.crates.io
rewrite /crates-index/config.json type:json https://static.crates.io/crates http://{PROXY_HOST}/crates-static/crates
bypass /crates-index/ *
cache /crates-static/ suffix .crate
```

---

## 3. 启动代理服务

```bash
# 启动代理服务，指定配置文件与本地缓存存储目录
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

详细客户端配置步骤请点击上方对应生态的使用指南。
