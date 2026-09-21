# Python PyPI 官方镜像源代理与缓存使用指南 (PyPI Caching Guide)

本文档介绍如何配置 `url-proxy` 作为 Python 官方 PyPI 镜像源的透明缓存代理，以及各类 Python 客户端（pip、uv、poetry、pdm）的接入配置。

---

## 1. 原理与架构 (Background & Principle)

Python 官方软件仓库（PyPI）在架构上采用**索引与文件分发解耦**：
- **Simple 索引入口**：`https://pypi.org/simple/<pkg>/`
- **文件分发 CDN**：`https://files.pythonhosted.org/packages/...`

当普通反向代理尝试代理 PyPI 时，返回的 Simple 索引（PEP 503 HTML 或 PEP 691 JSON）中包含 `https://files.pythonhosted.org/` 的公网绝对超链接。客户端解析后会直接直连公网 CDN，**绕过本地代理，导致几百 MB 的 Wheel 安装包无法被捕获与缓存**。

`url-proxy` 通过 **Feature 312** 提供了全链路闭环代理方案：
```text
[pip 客户端] --- (GET /pypi/simple/requests/) ---> [url-proxy] --- (原生 HTTPS) ---> [pypi.org]
                                                         │
                                               [动态响应体重写引擎]
                                      (将 files.pythonhosted.org 替换为代理路径)
                                                         │
[pip 客户端] ◄--- (返回重写后的索引页面) ───────────────────┘
     │
     ▼ (后续包下载全部回流代理)
[pip 客户端] --- (GET /pythonhosted/packages/...) ---> [url-proxy]
                                                             │
                                                  [本地磁盘 100% 强缓存]
                                                  [Single-Flight 并发合并]
```

---

## 2. 服务端配置 (`proxy.conf`)

在 `url-proxy` 的配置文件 `proxy.conf` 中追加以下规则：

```ini
# ==============================================================================
# PyPI 官方镜像源全链路闭环代理 (Feature 312)
# ==============================================================================

# 1. 声明双上游别名路由
alias pypi https://pypi.org
alias pythonhosted https://files.pythonhosted.org

# 2. 声明 Simple 索引绝对链接动态重写 (支持 HTML 与 PEP 691 JSON)
rewrite /pypi/simple/ type:pypi https://files.pythonhosted.org/ http://{PROXY_HOST}/pythonhosted/

# 3. 动静分离规则：动态索引纯流式直通 (防止版本更新延迟)
bypass /pypi/simple/ prefix *

# 4. 白名单强缓存规则：Wheel 与源码分发包 100% 持久化落盘
cache /pythonhosted/ suffix .whl
cache /pythonhosted/ suffix .tar.gz
cache /pythonhosted/ suffix .zip
```

---

## 3. 客户端接入配置 (Client Configuration)

假设 `url-proxy` 运行在局域网地址 `192.168.1.100:8888`。

### 3.1 方式一：pip 单次命令行使用（临时指定）
```bash
pip install requests \
  --index-url http://192.168.1.100:8888/pypi/simple/ \
  --trusted-host 192.168.1.100
```

### 3.2 方式二：全局配置文件持久化（推荐）
编辑或创建 pip 配置文件：
- **Linux / macOS**: `~/.pip/pip.conf` 或 `~/.config/pip/pip.conf`
- **Windows**: `%APPDATA%\pip\pip.ini` (例如 `C:\Users\<用户名>\AppData\Roaming\pip\pip.ini`)

写入以下内容：
```ini
[global]
index-url = http://192.168.1.100:8888/pypi/simple/
trusted-host = 192.168.1.100
```

### 3.3 方式三：环境变量配置（适合容器构建与 CI/CD）
在 Dockerfile 或 CI 流水线中注入环境变量：
```bash
export PIP_INDEX_URL="http://192.168.1.100:8888/pypi/simple/"
export PIP_TRUSTED_HOST="192.168.1.100"
```

Dockerfile 示例：
```dockerfile
FROM python:3.11-slim
ENV PIP_INDEX_URL="http://192.168.1.100:8888/pypi/simple/"
ENV PIP_TRUSTED_HOST="192.168.1.100"

WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
```

### 3.4 方式四：项目 `requirements.txt` 配置
在项目的 `requirements.txt` 首行声明镜像源参数：
```text
--index-url http://192.168.1.100:8888/pypi/simple/
--trusted-host 192.168.1.100

requests>=2.31.0
urllib3>=2.0.0
numpy>=1.24.0
```

### 3.5 方式五：现代包管理器接入

#### 1. uv (极速 Python 包管理器)
```bash
# 命令行使用
uv pip install requests --index-url http://192.168.1.100:8888/pypi/simple/ --allow-insecure-host 192.168.1.100

# 环境变量
export UV_INDEX_URL="http://192.168.1.100:8888/pypi/simple/"
export UV_INSECURE_HOST="192.168.1.100"
```

#### 2. Poetry
在项目根目录的 `pyproject.toml` 中配置源：
```toml
[[tool.poetry.source]]
name = "local-proxy"
url = "http://192.168.1.100:8888/pypi/simple/"
priority = "default"
```
并在命令行中信任 HTTP 凭据或设置 `poetry config certificates.local-proxy.cert false`。

#### 3. PDM
```bash
pdm config pypi.url http://192.168.1.100:8888/pypi/simple/
```

---

## 4. 验证与缓存命中排查

### 1. 验证索引重写
通过 `curl` 检查 Simple 索引返回的超链接：
```bash
curl -s http://192.168.1.100:8888/pypi/simple/urllib3/ | grep "http://"
```
*预期效果*：返回的 HTML 超链接前缀均为 `http://192.168.1.100:8888/pythonhosted/packages/...`，无任何外部公网直连域名。

### 2. 验证本地磁盘落盘与秒级复用
1. 执行 `pip install requests` 安装依赖；
2. 查看 `url-proxy` 缓存目录（如 `/var/cache/url-proxy/`），确认已生成对应 `.whl` 文件；
3. 清理客户端本地缓存后再次安装：
   ```bash
   pip cache purge
   pip install requests
   ```
4. 观察代理服务端日志，确认返回状态为本地磁盘读取，**上游回源流量为 0**，安装秒级完成。
