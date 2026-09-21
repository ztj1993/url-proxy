# Node.js npm 官方镜像源代理与缓存使用指南 (npm Caching Guide)

本文档介绍如何配置 `url-proxy` 作为 Node.js 官方 npm 注册中心（`registry.npmjs.org`）的透明缓存代理，以及各类常用 JavaScript/TypeScript 包管理器（`npm`、`yarn`、`pnpm`）的接入配置。

---

## 1. 原理与架构 (Background & Principle)

Node.js 官方包管理器注册中心在架构上，将包的**元数据信息（Packument）**与**压缩安装包（Tarball）**放在统一域名下，但在元数据中声明了绝对下载地址：
- **包元数据入口**：`https://registry.npmjs.org/<pkg>` 或 `https://registry.npmjs.org/@<scope>/<pkg>`
- **Tarball 压缩包声明**：在元数据的 `versions[x].dist.tarball` 字段中显式包含 `https://registry.npmjs.org/<pkg>/-/<pkg>-<version>.tgz`

现代客户端（npm v7+、yarn、pnpm）在解析元数据后，会优先使用 `dist.tarball` 中声明的绝对地址发起拉取。如果仅对客户端配置 registry 别名而未重写元数据响应体，客户端仍会直连官方公网拉取 `.tgz`，**绕过本地代理，导致大量的依赖安装包无法被捕获与持久化缓存**。

`url-proxy` 通过 **Feature 313** 提供了全链路闭环代理方案：
```text
[npm / pnpm / yarn] --- (GET /npm/lodash) ---> [url-proxy] --- (原生 HTTPS) ---> [registry.npmjs.org]
                                                    │
                                          [动态响应体重写引擎]
                                 (将 registry.npmjs.org 替换为代理路径)
                                                    │
[npm / pnpm / yarn] ◄--- (返回重写后的 JSON) ────────┘
        │
        ▼ (后续 .tgz 下载全部回流代理)
[npm / pnpm / yarn] --- (GET /npm/lodash/-/lodash-4.17.21.tgz) ---> [url-proxy]
                                                                         │
                                                             [本地磁盘 100% 强缓存]
                                                             [Single-Flight 并发合并]
```

---

## 2. 服务端配置 (`proxy.conf`)

在 `url-proxy` 的配置文件 `proxy.conf` 中追加以下规则：

```ini
# ==============================================================================
# npm 官方镜像源全链路闭环代理 (Feature 313)
# ==============================================================================

# 1. 声明官方注册中心别名
alias npm https://registry.npmjs.org

# 2. 声明 JSON 元数据中的官方 Tarball URL 重写规则 (闭环重定向至代理节点)
rewrite /npm/ type:npm https://registry.npmjs.org/ http://{PROXY_HOST}/npm/

# 3. 声明 .tgz 压缩包白名单强缓存 (持久化落盘并合并高并发请求)
cache /npm/ suffix .tgz
cache * suffix .tgz
```

---

## 3. 客户端接入配置 (Client Configuration)

假设 `url-proxy` 运行在局域网地址 `192.168.1.100:8888`。

### 3.1 npm 客户端配置

#### 方式 A：全局配置（推荐）
```bash
npm config set registry http://192.168.1.100:8888/npm/
```

#### 方式 B：项目级配置（在工程根目录下创建 `.npmrc`）
```ini
registry=http://192.168.1.100:8888/npm/
```

#### 方式 C：单次临时指定
```bash
npm install --registry http://192.168.1.100:8888/npm/ chalk
```

---

### 3.2 yarn 客户端配置

#### Yarn Classic (v1.x)
```bash
# 全局设置
yarn config set registry http://192.168.1.100:8888/npm/

# 或在项目根目录 .npmrc 中声明:
# registry=http://192.168.1.100:8888/npm/
```

#### Yarn Berry (v2 / v3 / v4)
在项目根目录 `.yarnrc.yml` 中配置：
```yaml
npmRegistryServer: "http://192.168.1.100:8888/npm/"
unsafeHttpWhitelist:
  - "192.168.1.100"
```

---

### 3.3 pnpm 客户端配置

#### 方式 A：全局命令行配置
```bash
pnpm config set registry http://192.168.1.100:8888/npm/
```

#### 方式 B：项目级 `.npmrc` 配置
在包含 `pnpm-workspace.yaml` 或 `package.json` 的项目根目录下创建 `.npmrc`：
```ini
registry=http://192.168.1.100:8888/npm/
```

---

### 3.4 组织作用域（Scoped Package）混合配置

若公司内部有自建的私有 npm 仓库（如 `@mycorp/*`），可保持私有包指向内部私服，公共官方包全部流经 `url-proxy`：

在 `.npmrc` 中配置：
```ini
# 默认公共依赖走代理并缓存
registry=http://192.168.1.100:8888/npm/

# 组织私有 Scope 走公司内部私有制品库
@mycorp:registry=https://npm.corp.internal/
```

---

## 4. 验证与缓存确认 (Verification)

### 4.1 验证 JSON 元数据重写
```bash
curl -s http://192.168.1.100:8888/npm/lodash | grep tarball
```
**正常输出片段**：
```json
"tarball": "http://192.168.1.100:8888/npm/lodash/-/lodash-4.17.21.tgz"
```
确认 `https://registry.npmjs.org/` 已全部被动态转换为代理自身地址。

### 4.2 验证点号包名正常重写
```bash
curl -s http://192.168.1.100:8888/npm/socket.io | grep tarball
```
**正常输出片段**：
```json
"tarball": "http://192.168.1.100:8888/npm/socket.io/-/socket.io-4.7.2.tgz"
```

### 4.3 验证实际安装与 0 外网流量
```bash
# 清空客户端全局缓存后重新安装
npm cache clean --force
npm install --registry http://192.168.1.100:8888/npm/ chalk
```
- **首拉取**：代理控制台显示 `job leader`，拉取并写入磁盘缓存；
- **后续并发与二次拉取**：客户端请求直接从本地文件返回，不产生额外公网回源流量。
