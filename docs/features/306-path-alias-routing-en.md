# Path Short Alias Routing & Dynamic Rewriting Design Plan

## 1. Goal

In the traditional path-prefix proxy mode, APT clients must configure verbose repository URLs that expose the public upstream hostname in `/etc/apt/sources.list`:
```text
deb http://<proxy-ip>:8888/http://archive.ubuntu.com/ubuntu jammy main
```
This presents several key challenges:
1. **Cluttered and Error-Prone**: Nested `http://` protocols often confuse administrators and developers during source list configuration.
2. **Lacks Private Registry Experience**: Cannot provide clean, canonical repository paths (such as `deb http://proxy:8888/ubuntu jammy main`) typical of enterprise private mirrors (e.g. Nexus, Artifactory).
3. **High Upstream Migration Cost**: If the public mirror changes or migrates to internal gateways, every client's `/etc/apt/sources.list` must be updated across all hosts.

**Feature 306 Objectives**:
Introduce the `alias` single-line directive in the unified configuration file `proxy.conf`, allowing the first segment of a request path to act as a short alias dynamically rewritten to a target upstream BaseURL:
- Clients configure concise canonical aliases: `deb http://<proxy-ip>:8888/ubuntu jammy main`.
- The proxy server transparently rewrites the URL to the actual upstream BaseURL and routes the request into existing caching, bypass, Range resuming, and 304 conditional revalidation pipelines.
- Native full-path proxying (`http://proxy:8888/http://...`) and standard forward proxying (`Acquire::http::Proxy`) remain 100% backward-compatible.

---

## 2. Architecture & Rewriting Pipeline

### 2.1 Configuration Syntax (`proxy.conf`)

Path aliases are declared via clean single-line directives:
```text
# Syntax: alias <short_alias> <upstream_base_url>
alias ubuntu http://archive.ubuntu.com/ubuntu
alias ubuntu-security http://security.ubuntu.com/ubuntu
alias debian https://deb.debian.org/debian
alias debian-security https://deb.debian.org/debian-security
```

### 2.2 Rewriting & Dispatching Pipeline

When an incoming client HTTP request is processed:

```
                    Incoming HTTP Request
                             │
                             ▼
              Is URI a standard absolute scheme (http/https/ftp)?
                            / \
                     Yes   /   \  No (Path mode / short alias)
                          /     \
                         ▼       ▼
               [Standard Flow]  Extract 1st segment: aliasKey = parts[0]
                                 │
                                 ▼
                     aliasKey exists in config.Aliases?
                                / \
                          Yes  /   \  No
                              /     \
                             ▼       ▼
              [Normalize & Rewrite to Absolute URI]   [Return 404 Not Found]
              (uri = targetBase + subPath)
                             │
                             ▼
              Proceeds into standard pipeline:
              1. Cache bypass evaluation (shouldBypassCache)
              2. 304 conditional revalidation (If-None-Match / ETag)
              3. Range slicing & resuming (206 Partial Content)
              4. Request coalescing download & SafeUrlPath disk caching
```

### 2.3 Normalized Splicing & Query Preservation
The rewriting engine guarantees rigorous path normalization:
1. Strips the alias segment and extracts `subPath`;
2. Strips trailing `/` from `targetBase` and leading `/` from `subPath`, joining them with a single `/`:
   ```go
   rewrittenUri := strings.TrimRight(targetBase, "/")
   if len(parts) > 1 && parts[1] != "" {
       rewrittenUri += "/" + strings.TrimLeft(parts[1], "/")
   } else if strings.HasSuffix(r.URL.Path, "/") {
       rewrittenUri += "/"
   }
   ```
3. If query parameters are present (`r.URL.RawQuery != ""`), appends `?` and raw query to the rewritten URI;
4. Validates that the rewritten scheme is valid (`http` or `https`); otherwise rejects with `400 Bad Request`.

---

## 3. Technical Benefits & Composition

1. **Seamless Cache Co-location (Zero Duplication)**:
   - Dynamic rewriting occurs at the very beginning of the pipeline. Subsequent disk cache keys (`SafeUrlPath`) are computed from the resolved upstream URL.
   - Whether requested via short alias (`/ubuntu/pool/.../pkg.deb`) or full path (`/http://archive.ubuntu.com/ubuntu/pool/.../pkg.deb`), **both hit the exact same local cache file**, preventing wasted disk space and redundant downloads.
2. **Automatic Inheritance of Bypass & Whitelisting**:
   - The rewritten absolute URI flows directly into `shouldBypassCache`.
   - Directives like `bypass /dists/ exact InRelease` and `cache * suffix .deb` apply seamlessly.
3. **Centralized Operational Management**:
   - When upstream mirrors migrate or swap to internal mirrors, only `proxy.conf` on the proxy server needs adjustment—zero client modifications required.

---

## 4. Client Configuration Examples

### 4.1 Ubuntu Client Setup (`/etc/apt/sources.list`)

```text
deb http://proxy-server:8888/ubuntu jammy main restricted universe multiverse
deb http://proxy-server:8888/ubuntu jammy-updates main restricted universe multiverse
deb http://proxy-server:8888/ubuntu-security jammy-security main restricted universe multiverse
```

### 4.2 Debian Client Setup (`/etc/apt/sources.list`)

```text
deb http://proxy-server:8888/debian bookworm main non-free-firmware
deb http://proxy-server:8888/debian bookworm-updates main non-free-firmware
deb http://proxy-server:8888/debian-security bookworm-security main non-free-firmware
```

---

## 5. Verification Standards

1. **Short Alias Dynamic Bypass**: Querying `/my-ubuntu/dists/jammy/InRelease` returns upstream live data with zero disk cache writes;
2. **Short Alias Package Disk Caching**: Querying `/my-ubuntu/pool/.../pkg.deb` returns 200 OK and successfully caches the `.deb` file on disk;
3. **Short Alias 304 Revalidation**: Querying with `If-None-Match` returns `304 Not Modified` directly from upstream;
4. **Multi-Alias Isolation**: Distinct aliases (`my-ubuntu` vs `my-debian`) route to their respective upstreams without cross-talk;
5. **Unknown Alias Safety**: Querying unconfigured alias routes safely yields `404 Not Found`.
