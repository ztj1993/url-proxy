# Python PyPI Official Registry Proxy & Caching Guide

This guide describes how to configure `url-proxy` as an accelerating caching gateway for the official Python PyPI repository, and how to configure client tools (pip, uv, poetry, pdm).

---

## 1. Background & Architecture

The official Python Package Index (PyPI) decouples indexing from package distribution:
- **Simple Index Root**: `https://pypi.org/simple/<pkg>/`
- **File Distribution CDN**: `https://files.pythonhosted.org/packages/...`

Traditional proxies fail to cache PyPI because the Simple index (PEP 503 HTML or PEP 691 JSON) embeds absolute URLs pointing directly to `files.pythonhosted.org`. When `pip` parses the index, it connects directly to the external CDN, bypassing the proxy and downloading hundreds of megabytes of wheel archives over the WAN.

With **Feature 312**, `url-proxy` solves this with a closed-loop rewriting architecture:
```text
[pip client] --- (GET /pypi/simple/requests/) ---> [url-proxy] --- (Native HTTPS) ---> [pypi.org]
                                                         │
                                             [Body Rewriting Engine]
                                      (Replaces files.pythonhosted.org)
                                                         │
[pip client] ◄--- (Rewritten index returned) ───────────┘
     │
     ▼ (Subsequent downloads route through proxy)
[pip client] --- (GET /pythonhosted/packages/...) ---> [url-proxy]
                                                             │
                                                  [100% Persistent Disk Cache]
                                                  [Single-Flight Coalescing]
```

---

## 2. Server Configuration (`proxy.conf`)

Add the following rules to your `proxy.conf`:

```ini
# ==============================================================================
# PyPI Official Registry Proxy & Caching (Feature 312)
# ==============================================================================

# 1. Dual upstream aliases
alias pypi https://pypi.org
alias pythonhosted https://files.pythonhosted.org

# 2. Dynamic response body rewriting for PEP 503 HTML and PEP 691 JSON
rewrite /pypi/simple/ type:pypi https://files.pythonhosted.org/ http://{PROXY_HOST}/pythonhosted/

# 3. Dynamic index streaming bypass (ensures immediate release visibility)
bypass /pypi/simple/ prefix *

# 4. Mandatory disk caching for binary wheels and source archives
cache /pythonhosted/ suffix .whl
cache /pythonhosted/ suffix .tar.gz
cache /pythonhosted/ suffix .zip
```

---

## 3. Client Configuration

Assume `url-proxy` runs on `192.168.1.100:8888`.

### 3.1 Method 1: pip CLI Flag (Temporary)
```bash
pip install requests \
  --index-url http://192.168.1.100:8888/pypi/simple/ \
  --trusted-host 192.168.1.100
```

### 3.2 Method 2: Global Configuration File (`pip.conf` / `pip.ini`)
Edit or create your configuration file:
- **Linux / macOS**: `~/.pip/pip.conf` or `~/.config/pip/pip.conf`
- **Windows**: `%APPDATA%\pip\pip.ini`

```ini
[global]
index-url = http://192.168.1.100:8888/pypi/simple/
trusted-host = 192.168.1.100
```

### 3.3 Method 3: Environment Variables (CI/CD & Containers)
```bash
export PIP_INDEX_URL="http://192.168.1.100:8888/pypi/simple/"
export PIP_TRUSTED_HOST="192.168.1.100"
```

Dockerfile example:
```dockerfile
FROM python:3.11-slim
ENV PIP_INDEX_URL="http://192.168.1.100:8888/pypi/simple/"
ENV PIP_TRUSTED_HOST="192.168.1.100"

WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
```

### 3.4 Method 4: `requirements.txt` Directives
```text
--index-url http://192.168.1.100:8888/pypi/simple/
--trusted-host 192.168.1.100

requests>=2.31.0
urllib3>=2.0.0
numpy>=1.24.0
```

### 3.5 Method 5: Modern Tooling

#### 1. uv
```bash
uv pip install requests --index-url http://192.168.1.100:8888/pypi/simple/ --allow-insecure-host 192.168.1.100
```

#### 2. Poetry
```toml
[[tool.poetry.source]]
name = "local-proxy"
url = "http://192.168.1.100:8888/pypi/simple/"
priority = "default"
```

#### 3. PDM
```bash
pdm config pypi.url http://192.168.1.100:8888/pypi/simple/
```

---

## 4. Verification

### 1. Verify Index Rewriting
```bash
curl -s http://192.168.1.100:8888/pypi/simple/urllib3/ | grep "http://"
```
*Expected*: All package links point to `http://192.168.1.100:8888/pythonhosted/packages/...`.

### 2. Verify Disk Caching & Zero WAN Latency
1. Run `pip install requests`;
2. Check the proxy cache directory for `.whl` files;
3. Clear the client pip cache and re-run:
   ```bash
   pip cache purge
   pip install requests
   ```
4. Confirm in proxy logs that the response is served locally with 0 upstream network traffic.
