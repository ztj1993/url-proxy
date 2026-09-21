# Alpine Linux (apk) Caching Proxy Usage Guide

`url-proxy` natively provides 100% out-of-the-box support for the Alpine Linux `apk` package manager. Like APT, Alpine relies on dynamic index metadata (`APKINDEX.tar.gz`) and immutable static binary packages (`.apk`).

---

## 1. Server Configuration (`proxy.conf`)

Add Alpine rules to `proxy.conf`:

```ini
# --- Upstream official Alpine CDN alias ---
alias /alpine https://dl-cdn.alpinelinux.org/alpine

# --- Dynamic metadata: Passthrough without saving to disk (Zero Disk I/O) ---
bypass /alpine/ exact APKINDEX.tar.gz

# --- Package whitelist: 100% persistent cache for .apk packages ---
cache  /alpine/ suffix .apk
```

Start `url-proxy`:
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. Client Configuration

Configure the repository on Alpine hosts or within Docker container builds.

### Method A: Edit `/etc/apk/repositories` (Recommended for Dockerfile builds)

Replace the official CDN domain with the proxy address and `/alpine` alias:

```text
http://192.168.1.100:8888/alpine/v3.19/main
http://192.168.1.100:8888/alpine/v3.19/community
```

**Dockerfile Best Practice**:
```dockerfile
FROM alpine:3.19
# Point repositories to internal proxy for near-instant package installation
RUN sed -i 's|https://dl-cdn.alpinelinux.org/alpine|http://192.168.1.100:8888/alpine|g' /etc/apk/repositories \
    && apk add --no-cache curl git ca-certificates
```

### Method B: Environment Variable Proxy

If using direct HTTP upstreams, set standard environment variables:
```bash
export http_proxy="http://192.168.1.100:8888"
apk update && apk add curl
```

---

## 3. Verification

Run index update and package installation on Alpine:

```bash
# 1. Update index (APKINDEX.tar.gz streamed directly from upstream)
apk update

# 2. Install package (first download cached; subsequent pulls hit local cache)
apk add curl
```

**Benefits**:
- Repeated Docker builds or CI runs pulling identical `.apk` packages experience zero external WAN latency and bandwidth consumption.
