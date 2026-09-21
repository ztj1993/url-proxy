# APT (Debian / Ubuntu) Caching Proxy Usage Guide

`url-proxy` natively provides full support for caching and proxying Debian and Ubuntu APT repositories.

---

## 1. Server Configuration (`proxy.conf`)

Add APT rules to `proxy.conf`:

```ini
# --- Upstream alias definitions (supports HTTP or HTTPS upstreams) ---
alias /ubuntu http://archive.ubuntu.com/ubuntu
alias /debian http://deb.debian.org/debian
alias /ubuntu-security http://security.ubuntu.com/ubuntu

# --- Dynamic metadata: Streamed passthrough (Zero Disk I/O) ---
bypass /ubuntu/dists/ prefix Packages
bypass /ubuntu/dists/ exact InRelease
bypass /ubuntu/dists/ exact Release
bypass /ubuntu/dists/ suffix .gpg
bypass /ubuntu/dists/ suffix .diff/Index

bypass /debian/dists/ prefix Packages
bypass /debian/dists/ exact InRelease
bypass /debian/dists/ exact Release
bypass /debian/dists/ suffix .gpg

# --- Package whitelist: 100% persistent caching ---
cache /ubuntu/ suffix .deb
cache /ubuntu/ suffix .udeb
cache /debian/ suffix .deb
cache /debian/ suffix .udeb
```

Start `url-proxy` (assuming server IP `192.168.1.100:8888`):
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. Client Configuration

Choose one of two client configuration modes:

### Mode A: Path Short Alias Mode (Recommended)

Directly edit `/etc/apt/sources.list` (or `/etc/apt/sources.list.d/ubuntu.sources`) on client machines, replacing the original mirror domain with the proxy IP and alias:

```sources.list
# Ubuntu 22.04 (Jammy) Example
deb http://192.168.1.100:8888/ubuntu jammy main restricted universe multiverse
deb http://192.168.1.100:8888/ubuntu jammy-updates main restricted universe multiverse
deb http://192.168.1.100:8888/ubuntu-security jammy-security main restricted universe multiverse
```

> [!TIP]
> **No CA Root Certificate Required**: Even when upstream uses HTTPS, clients connect to `url-proxy` via standard HTTP on the internal network, while `url-proxy` initiates native HTTPS to the upstream mirror. Because APT natively verifies package integrity via GPG signatures, internal HTTP transport poses no security risk and eliminates the need to install custom CA certificates on clients.

### Mode B: Standard HTTP Forward Proxy Mode

Keep your existing `/etc/apt/sources.list` intact (applicable when upstream mirror itself is HTTP) and create `/etc/apt/apt.conf.d/01proxy`:

```apt.conf
Acquire::http::Proxy "http://192.168.1.100:8888";
```

---

## 3. Verification

Run update and package downloads on the client:

```bash
# 1. Update package index (uses If-None-Match passthrough; fast 304 Not Modified when unchanged)
sudo apt-get update

# 2. Download package: First download caches to disk; subsequent requests hit local cache
sudo apt-get install -y --download-only curl
```

**Server Log Verification**:
- `/ubuntu/dists/.../InRelease`: Logged as `[BYPASS]`, streamed directly without saving to disk.
- `/ubuntu/pool/.../curl_*.deb`: First download logged as `[CACHE-MISS]` and written to disk; concurrent or subsequent downloads logged as `[CACHE-HIT]`.
