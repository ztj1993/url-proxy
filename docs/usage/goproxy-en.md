# Go Modules (GOPROXY) Caching Proxy Usage Guide

`url-proxy` natively provides 100% out-of-the-box support for the Go Modules `GOPROXY` protocol. The Go proxy protocol is a clean, RESTful HTTP specification where the Go toolchain routes all module queries strictly through `$GOPROXY` without external cross-domain URL redirects.

---

## 1. Server Configuration (`proxy.conf`)

Add Go module proxy rules to `proxy.conf` (upstream can be `https://goproxy.cn` or official `https://proxy.golang.org`):

```ini
# --- Upstream GOPROXY alias ---
alias /goproxy https://goproxy.cn

# --- Dynamic metadata: Passthrough without saving to disk ---
# The module version list changes when new versions are tagged
bypass /goproxy/ suffix /@v/list
bypass /goproxy/ suffix /@latest

# --- Package whitelist: 100% persistent cache for module archives and metadata ---
# Module artifacts in Go are strictly immutable per version tag
cache  /goproxy/ suffix .zip
cache  /goproxy/ suffix .mod
cache  /goproxy/ suffix .info
```

Start `url-proxy`:
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. Client Configuration

Configure the Go environment variable on developer machines, CI/CD runners, or container builds:

```bash
# Set GOPROXY to your url-proxy instance (append ,direct as fallback)
go env -w GOPROXY=http://192.168.1.100:8888/goproxy,direct
```

**Dockerfile Example**:
```dockerfile
FROM golang:1.22
WORKDIR /app

# Accelerate go mod download in container builds
ENV GOPROXY=http://192.168.1.100:8888/goproxy,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /server .
```

---

## 3. Verification

Download dependencies in a Go project:

```bash
# Clean module cache and download
go clean -modcache
go mod download
```

**Server Logs**:
- `.../@v/list`: Handled as `[BYPASS]`, ensuring latest version discovery.
- `.../@v/v1.2.3.zip`: Cached on first fetch. Subsequent builds across the internal network hit the local cache with zero external bandwidth.
