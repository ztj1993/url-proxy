# url-proxy Package Manager & OS Repository Usage Guides

`url-proxy` serves as a high-performance local/LAN caching proxy gateway for package managers and operating system repositories. By combining **Request Coalescing (Single-Flight)**, **Safe Storage Paths (SafeUrlPath)**, **Dynamic/Static Separation Rules (bypass/cache)**, and **Advanced HTTP Features (304 Not Modified / Range 206 Partial Content)**, it achieves zero redundant external bandwidth consumption and accelerates CI/CD pipelines and deployment workflows.

---

## 1. Ecosystem Support Matrix

| Ecosystem | Protocol / Layout | Dynamic Metadata (Bypass/304 Passthrough) | Static Packages (100% Cached) | Current Status | Usage Guide |
| :--- | :--- | :--- | :--- | :---: | :--- |
| **APT (Debian / Ubuntu)** | APT Repository Spec | `InRelease`, `Release`, `Packages*` | `.deb`, `.udeb` | **100% Supported** | [APT Usage Guide](apt-en.md) |
| **Alpine Linux** | APK Repository Spec | `APKINDEX.tar.gz` | `.apk` | **100% Supported** | [Alpine APK Usage Guide](alpine-en.md) |
| **Go Modules** | GOPROXY Protocol | `/@v/list` (version list) | `.zip`, `.mod`, `.info` | **100% Supported** | [Go GOPROXY Usage Guide](goproxy-en.md) |
| **Maven / Gradle** | Maven 2 Layout | `maven-metadata.xml` | `.jar`, `.aar`, `.pom` | **100% Supported** | [Maven Usage Guide](maven-en.md) |
| **PyPI (Python)** | PEP 503 / 691 | `/simple/<pkg>/` | `.whl`, `.tar.gz` | **100% Supported**<br>*(Feature 312 Closed-Loop)* | [PyPI Usage Guide](pypi-en.md) |
| **npm (Node.js)** | npm Registry Spec | `/<pkg>` (metadata JSON) | `/-/*.tgz` | **100% Supported**<br>*(Feature 313 Closed-Loop)* | [npm Usage Guide](npm-en.md) |
| **Docker / OCI** | Registry v2 API | `/v2/.../manifests/` | `/v2/.../blobs/` | **100% Supported**<br>*(Feature 314 Layer Caching)* | [Docker Usage Guide](docker-en.md) |
| **Rust Cargo** | Sparse Index (RFC 2785) | `/crates-index/...` | `crates/*/*.crate` | **100% Supported**<br>*(Feature 315 Closed-Loop)* | [Cargo Usage Guide](cargo-en.md) |

---

## 2. Quick Unified Configuration (`proxy.conf`)

`url-proxy` uses a single declarative file for all aliases and bypass rules:

```ini
# ==========================================
# 1. APT (Ubuntu / Debian) Rules
# ==========================================
alias /ubuntu http://archive.ubuntu.com/ubuntu
bypass /ubuntu/dists/ prefix Packages
bypass /ubuntu/dists/ exact InRelease
bypass /ubuntu/dists/ exact Release
bypass /ubuntu/dists/ suffix .gpg
cache  /ubuntu/ suffix .deb

# ==========================================
# 2. Alpine Linux (apk) Rules
# ==========================================
alias /alpine https://dl-cdn.alpinelinux.org/alpine
bypass /alpine/ exact APKINDEX.tar.gz
cache  /alpine/ suffix .apk

# ==========================================
# 3. Go Modules (GOPROXY) Rules
# ==========================================
alias /goproxy https://goproxy.cn
bypass /goproxy/ suffix /@v/list
cache  /goproxy/ suffix .zip
cache  /goproxy/ suffix .mod
cache  /goproxy/ suffix .info

# ==========================================
# 4. Maven Central Repository Rules
# ==========================================
alias /maven https://repo1.maven.org/maven2
bypass /maven/ exact maven-metadata.xml
bypass /maven/ suffix maven-metadata.xml.sha1
cache  /maven/ suffix .jar
cache  /maven/ suffix .pom
cache  /maven/ suffix .aar

# ==========================================
# 5. PyPI (Python) Official Closed-Loop Rules (Feature 312)
# ==========================================
alias pypi https://pypi.org
alias pythonhosted https://files.pythonhosted.org
rewrite /pypi/simple/ type:pypi https://files.pythonhosted.org/ http://{PROXY_HOST}/pythonhosted/
bypass /pypi/simple/ prefix *
cache /pythonhosted/ suffix .whl
cache /pythonhosted/ suffix .tar.gz
cache /pythonhosted/ suffix .zip

# ==========================================
# 6. npm (Node.js) Official Closed-Loop Rules (Feature 313)
# ==========================================
alias npm https://registry.npmjs.org
rewrite /npm/ type:npm https://registry.npmjs.org/ http://{PROXY_HOST}/npm/
cache /npm/ suffix .tgz

# ==========================================
# 7. Docker / OCI Registry v2 Rules (Feature 314)
# ==========================================
alias v2 https://registry-1.docker.io/v2
alias docker https://registry-1.docker.io
bypass /v2/ exact v2
bypass /v2/ contains /manifests/
cache /v2/ contains /blobs/

# ==========================================
# 8. Rust Cargo Sparse Index & Crates Rules (Feature 315)
# ==========================================
alias crates-index https://index.crates.io
alias crates-static https://static.crates.io
rewrite /crates-index/config.json type:json https://static.crates.io/crates http://{PROXY_HOST}/crates-static/crates
bypass /crates-index/ *
cache /crates-static/ suffix .crate
```

---

## 3. Starting the Proxy Service

```bash
# Start url-proxy with unified config and local cache storage directory
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

For client configuration details, click on the respective ecosystem guide above.
