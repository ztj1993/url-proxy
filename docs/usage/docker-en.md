# Docker / OCI Registry v2 Relay & Layer Caching Guide

This document describes how to configure `url-proxy` as a high-performance caching proxy for the official Docker Hub / OCI Registry (`registry-1.docker.io`), as well as client configurations for container runtimes (Docker Daemon, Containerd / nerdctl, and Podman).

---

## 1. Principle & Architecture

When pulling container images (`docker pull`), the Docker client adheres to the OCI / Registry v2 API specification:
1. **401 Bearer Token Handshake Challenge**: Prior to pulling, the client sends a `GET /v2/` probe. The official registry responds with `401 Unauthorized` and indicates the authorization server in the `Www-Authenticate` header (e.g. `realm="https://auth.docker.io/token"`);
2. **Dynamic Manifests**: After obtaining a Bearer Token, the client requests `GET /v2/<name>/manifests/<tag>`, describing image metadata and the SHA256 hashes of all layer blobs. Manifests change when tags update and are kept fresh via HTTP 304 conditional revalidation and streaming bypass;
3. **Immutable Layer Blobs**: Image layers are accessed via `/v2/<name>/blobs/sha256:<digest>`. These content-addressable blobs are cryptographically immutable and vary from tens of megabytes to several gigabytes in size.

`url-proxy` natively relays the Registry v2 protocol and accelerates image layer caching through **Feature 314**:
```text
[Docker Daemon] --- (GET /v2/) ---> [url-proxy] --- (GET /v2/) ---> [registry-1.docker.io]
       ▲                                  │                                    │
       │                                  │ (Relay 401 + Www-Authenticate)     │
       └────── (Return 401 challenge) ────┴────────────────────────────────────┘
       │
       ▼ (Client exchanges token with official auth.docker.io)
[Docker Daemon] --- (GET /v2/.../manifests/... + Token) ---> [url-proxy] ---> [Registry]
                                                                  │ (Dynamic bypass / 304)
                                                                  ▼
[Docker Daemon] --- (HEAD /v2/.../blobs/sha256:...) --------> [url-proxy]
                                                                  │
                                                      [Cached locally? Direct 200 OK]
                                                      [Not cached? Relay upstream probe]
                                                                  │
[Docker Daemon] --- (GET /v2/.../blobs/sha256:...) ---------> [url-proxy]
                                                                  │
                                                      [Miss: Follow 307 redirect to CDN]
                                                      [Single-Flight coalescing + atomic cache]
                                                      [Hit: Direct fast-path / Range 206]
```

---

## 2. Server Configuration (`proxy.conf`)

Add the following directives to `proxy.conf`:

```ini
# ==============================================================================
# 6. Docker / OCI Registry v2 Relay & Layer Caching (Feature 314)
# Daemon Mirror: "registry-mirrors": ["http://<proxy>:8888"]
# Path Prefix: docker pull <proxy>:8888/docker/library/ubuntu:22.04
# ==============================================================================
alias v2 https://registry-1.docker.io/v2
alias docker https://registry-1.docker.io

# Handshake probing & Manifest metadata dynamic bypass (streaming passthrough)
bypass /v2/ exact v2
bypass /v2/ exact index
bypass /v2/ contains /manifests/
bypass /docker/v2/ exact v2
bypass /docker/v2/ exact index
bypass /docker/v2/ contains /manifests/

# Immutable Layer Blobs 100% permanent local caching
cache /v2/ contains /blobs/
cache /docker/v2/ contains /blobs/
```

---

## 3. Client Configuration

Assume `url-proxy` is running on your LAN at `192.168.1.100:8888`.

### 3.1 Method A: Docker Daemon Global Registry Mirror (Recommended)

In `/etc/docker/daemon.json` on client hosts, configure the registry mirror and insecure registry whitelist:

```json
{
  "registry-mirrors": ["http://192.168.1.100:8888"],
  "insecure-registries": ["192.168.1.100:8888"]
}
```

Reload and restart Docker:
```bash
sudo systemctl daemon-reload
sudo systemctl restart docker
```

Pull official images as usual without modifying image names:
```bash
docker pull ubuntu:22.04
docker pull alpine:latest
docker pull redis:alpine
```

### 3.2 Method B: Explicit Path Prefix Pulling

If you prefer not to modify global daemon mirrors:

Add the proxy to `insecure-registries` in `/etc/docker/daemon.json`:
```json
{
  "insecure-registries": ["192.168.1.100:8888"]
}
```

Restart Docker and pull with the host prefix:
```bash
docker pull 192.168.1.100:8888/docker/library/ubuntu:22.04
docker pull 192.168.1.100:8888/docker/library/alpine:latest
```

### 3.3 Containerd / nerdctl / Kubernetes CRI Configuration

Edit `/etc/containerd/config.toml` under `plugins."io.containerd.grpc.v1.cri".registry.mirrors`:

```toml
[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://192.168.1.100:8888"]
```

Restart containerd:
```bash
sudo systemctl restart containerd
```

Pull with `nerdctl` or `crictl`:
```bash
nerdctl pull ubuntu:22.04
```

### 3.4 Podman Configuration

In `/etc/containers/registries.conf`:

```toml
[[registry]]
prefix = "docker.io"
location = "docker.io"

[[registry.mirror]]
location = "192.168.1.100:8888"
insecure = true
```

---

## 4. Verification & Diagnostics

### 4.1 Manual Probing
```bash
# Verify /v2/ probe returns 401 and Www-Authenticate
curl -i http://192.168.1.100:8888/v2/

# Expected output:
# HTTP/1.1 401 Unauthorized
# Www-Authenticate: Bearer realm="https://auth.docker.io/token",service="registry.docker.io"
# Docker-Distribution-Api-Version: registry/2.0
```

### 4.2 Cache Verification
Inspect the proxy's cache directory (e.g. `./cache/https-registry-1.docker.io/v2/`):
```bash
ls -lh ./cache/https-registry-1.docker.io/v2/library/ubuntu/blobs/
```
You will find safe cached files named like `sha256%3A7b1a8c9e...`.

### 4.3 Performance & Acceleration
When multiple nodes pull images simultaneously or CI jobs rebuild repeatedly:
- **First pull**: A Leader connects to upstream and writes to local disk atomically, while concurrent Follower nodes stream chunks in real-time (Single-Flight request coalescing).
- **Subsequent pulls**: Docker client `HEAD` and `GET` requests are served directly from local SSD/NVMe at wire speed with **0 upstream bandwidth consumption**.
