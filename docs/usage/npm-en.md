# Node.js npm Official Registry Proxy & Caching Guide

This document describes how to configure `url-proxy` as a caching reverse proxy for the official Node.js npm registry (`registry.npmjs.org`), along with client configuration guides for `npm`, `yarn`, and `pnpm`.

---

## 1. Background & Architecture

The official npm registry architecture serves package **metadata (packuments)** and **tarball archives** under the same domain, but declares absolute URLs inside the metadata:
- **Metadata Ingress**: `https://registry.npmjs.org/<pkg>` or `https://registry.npmjs.org/@<scope>/<pkg>`
- **Tarball Distribution**: Declared inside `versions[x].dist.tarball` as `https://registry.npmjs.org/<pkg>/-/<pkg>-<version>.tgz`

Modern clients (npm v7+, yarn, pnpm) prioritize downloading from the absolute address declared in `dist.tarball`. Without response body rewriting, clients bypass the proxy when downloading package archives, **preventing heavy `.tgz` packages from being cached locally**.

`url-proxy` solves this with **Feature 313**:
```text
[npm / pnpm / yarn] --- (GET /npm/lodash) ---> [url-proxy] --- (Native HTTPS) ---> [registry.npmjs.org]
                                                    │
                                     [Dynamic Body Rewriting Engine]
                                (Replaces registry.npmjs.org with proxy URL)
                                                    │
[npm / pnpm / yarn] ◄--- (Rewritten JSON metadata) ─┘
        │
        ▼ (.tgz downloads route through proxy)
[npm / pnpm / yarn] --- (GET /npm/lodash/-/lodash-4.17.21.tgz) ---> [url-proxy]
                                                                         │
                                                             [100% Disk Caching]
                                                          [Single-Flight Coalescing]
```

---

## 2. Server Configuration (`proxy.conf`)

Add the following directives to your `proxy.conf`:

```ini
# ==============================================================================
# npm Official Registry Closed-Loop Proxy (Feature 313)
# ==============================================================================

# 1. Declare upstream alias
alias npm https://registry.npmjs.org

# 2. Rewrite official Tarball URLs in metadata JSON to route through proxy
rewrite /npm/ type:npm https://registry.npmjs.org/ http://{PROXY_HOST}/npm/

# 3. Permanent disk cache for .tgz archives
cache /npm/ suffix .tgz
cache * suffix .tgz
```

---

## 3. Client Configuration

Assuming `url-proxy` runs on `192.168.1.100:8888`.

### 3.1 npm Configuration

#### Option A: Global Configuration (Recommended)
```bash
npm config set registry http://192.168.1.100:8888/npm/
```

#### Option B: Per-Project `.npmrc`
Create `.npmrc` in your project root:
```ini
registry=http://192.168.1.100:8888/npm/
```

#### Option C: Ad-Hoc CLI Usage
```bash
npm install --registry http://192.168.1.100:8888/npm/ chalk
```

---

### 3.2 yarn Configuration

#### Yarn Classic (v1.x)
```bash
# Global configuration
yarn config set registry http://192.168.1.100:8888/npm/

# Or via project .npmrc:
# registry=http://192.168.1.100:8888/npm/
```

#### Yarn Berry (v2 / v3 / v4)
In your project `.yarnrc.yml`:
```yaml
npmRegistryServer: "http://192.168.1.100:8888/npm/"
unsafeHttpWhitelist:
  - "192.168.1.100"
```

---

### 3.3 pnpm Configuration

#### Option A: Global CLI
```bash
pnpm config set registry http://192.168.1.100:8888/npm/
```

#### Option B: Project `.npmrc`
In your workspace root or package directory:
```ini
registry=http://192.168.1.100:8888/npm/
```

---

### 3.4 Scoped Private Packages Hybrid Configuration

If your organization uses internal private packages (e.g. `@mycorp/*`), route private packages to your internal server and public packages to `url-proxy`:

In `.npmrc`:
```ini
# Public packages cached via url-proxy
registry=http://192.168.1.100:8888/npm/

# Private scoped packages
@mycorp:registry=https://npm.corp.internal/
```

---

## 4. Verification

### 4.1 JSON Metadata Rewriting
```bash
curl -s http://192.168.1.100:8888/npm/lodash | grep tarball
```
**Expected snippet**:
```json
"tarball": "http://192.168.1.100:8888/npm/lodash/-/lodash-4.17.21.tgz"
```

### 4.2 Dotted Package Names Rewriting
```bash
curl -s http://192.168.1.100:8888/npm/socket.io | grep tarball
```
**Expected snippet**:
```json
"tarball": "http://192.168.1.100:8888/npm/socket.io/-/socket.io-4.7.2.tgz"
```

### 4.3 End-to-End Installation & Cache Hits
```bash
npm cache clean --force
npm install --registry http://192.168.1.100:8888/npm/ chalk
```
- First install fetches tarball and atomically saves to cache directory.
- Subsequent runs or concurrent machines download directly from local disk with zero upstream bandwidth.
