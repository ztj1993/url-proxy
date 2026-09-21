# Feature 311: 响应体流式动态重写引擎设计方案

## 1. 功能目标 (Goal)

在将 `url-proxy` 作为应用层包管理器（如 PyPI、npm）官方源代理时，面临一个共性阻碍：上游返回的元数据（如 HTML 索引页面、JSON 版本清单）中，软件包下载超链接硬编码了官方全球 CDN 的绝对 URL（如 `https://files.pythonhosted.org/...` 或 `https://registry.npmjs.org/...`）。这会导致客户端在解析元数据后直接直连公网源，绕过代理服务并导致本地缓存失效。

**Feature 311 建设目标**：
在 `url-proxy` 的中继转发管线中引入通用的**响应体动态流式重写引擎 (`BodyRewriter`)**：
1. **配置驱动与统一前缀谓词语法**：
   - 引入 `type <name> [mime:<MIME>] [suffix:<Ext>]...` 指令声明可复用的内容类型别名与分组；
   - 引入 `rewrite <Scope> [TypeSpec] <Pattern> <Replacement>` 指令，支持 `type:<name>`、`mime:<pattern>`、`suffix:<ext>` 及缺省 3 参数形式；
2. **动态主机宏插值**：支持 `{PROXY_HOST}` 变量，根据客户端请求中的 `Host` 头动态调整替换目标；
3. **精准类型过滤与二进制保护机制**：
   - 结合请求文件扩展名（`isKnownBinaryPath`）与响应 `Content-Type`（`isBinaryMimeType`）实现双重防护；
   - 严格仅对命中规则声明类型的文本响应进行改写，二进制文件（如 `.whl`、`.tgz`、`.deb`）严禁被篡改；
4. **协议头自适应修复**：剥离原上游 `ETag` 与 `Content-MD5`，自适应重算并更新 `Content-Length`；
5. **编码协商与 Gzip 透明解压**：向上游发起带重写作用域的请求时，注入 `Accept-Encoding: identity`；若上游依然返回 gzip 压缩流，自动透明解压、剥离 `Content-Encoding` 并执行重写。

---

## 2. 核心架构与重写管线 (Architecture & Pipeline)

```text
                     收到客户端 HTTP 请求
                              │
                              ▼
            [提取 Host, 匹配别名与 rewrite 规则]
                              │
            是否为重写目标 (MightMatchRequest)?
            (检查 Scope 且排除 .tgz/.whl 等已知二进制后缀)
                             / \
                       是   /   \  否
                           /     \
                          ▼       ▼
       [注入 Accept-Encoding: identity]  [常规上游请求]
                          │               │
                          ▼               ▼
                   [发起原生上游请求] ◄─────┘
                          │
                          ▼
                   [收到上游 HTTP 响应]
                          │
         ShouldRewrite: 检查 Content-Type 与 TypeSpec
         (支持 type:<name>, mime:<pattern>, suffix:<ext>)
                         / \
                   是   /   \  否 (非目标文本或二进制流)
                       /     \
                      ▼       ▼
           [内存/流式文本替换引擎]  [常规直通/落盘缓存管线]
           - Gzip 透明解压 (若存在)
           - 替换 Pattern 为 Replacement ({PROXY_HOST} 插值)
           - 剥离上游 ETag 与 Content-MD5
           - 更新 Content-Length 头部
                      │
                      ▼
               [返回客户端修改后的响应]
```

---

## 3. 配置文件语法规范 (`proxy.conf`)

### 3.1 指令语法

#### 1. 类型定义指令 `type`
```ini
# 语法: type <name> [mime:<MIME>] [suffix:<Ext>]...
# 示例 1: 定义 HTML 页面类型
type html mime:text/html suffix:.html suffix:.htm

# 示例 2: 定义 JSON 元数据类型
type json mime:application/json suffix:.json

# 示例 3: 定义 PyPI 专有类型 (包含 HTML 索引与 PEP 691 JSON 规范)
type pypi mime:text/html mime:application/vnd.pypi.simple* suffix:.html
```

> **内置默认类型**：
> 系统出厂内置了 `html`、`json`、`xml`、`pypi`、`text` 等常见别名，用户无需显式声明即可直接在 `rewrite` 中引用。

#### 2. 响应体重写指令 `rewrite`
```ini
# 语法 1: 引用类型别名 (最推荐，清晰自解释)
rewrite /pypi/simple/ type:pypi https://files.pythonhosted.org/ http://{PROXY_HOST}/pythonhosted/
rewrite /npm/         type:json https://registry.npmjs.org/     http://{PROXY_HOST}/npm/

# 语法 2: 直接内联 MIME 匹配
rewrite /api/         mime:application/json https://api.upstream.com/ http://{PROXY_HOST}/api/

# 语法 3: 直接内联文件后缀匹配
rewrite /docs/        suffix:.html          https://docs.upstream.com/ http://{PROXY_HOST}/docs/

# 语法 4: 缺省 3 参数形式 (默认匹配所有安全文本类型)
rewrite /common/      https://upstream.com/ http://{PROXY_HOST}/common/
```

### 3.2 字段定义
- **`<Scope>`**：生效的 URL 路径前缀（如 `/pypi/simple/` 或 `/npm/`）；
- **`<TypeSpec>`**：内容类型规格，支持四种形式：
  - `type:<name>`：引用 `type` 语句预定义或内置的类型别名；
  - `mime:<pattern>`：直接匹配 HTTP `Content-Type`（支持通配符 `*`）；
  - `suffix:<ext>`：直接匹配 URL 文件名后缀；
  - `*` 或省略：缺省匹配所有通用文本类型。
- **`<MatchPattern>`**：待在响应体文本中匹配的目标字符串；
- **`<Replacement>`**：替换成的目标字符串模板，支持宏变量 `{PROXY_HOST}`（运行时自动获取请求头 `r.Host`，如 `192.168.1.100:8888`）。

---

## 4. 模块设计与实现细节 (`url-proxy.go`)

### 4.1 数据结构

```go
// TypeDefinition 内容类型定义
type TypeDefinition struct {
    Name     string   // 类型别名 (如 "html", "json", "pypi")
    Mimes    []string // 匹配的 Content-Type 列表
    Suffixes []string // 匹配的文件名/URL后缀列表
}

// RewriteRule 响应体动态重写规则
type RewriteRule struct {
    Scope       string // 作用域路径前缀
    TypeSpec    string // 内容类型规格 ("type:json", "mime:...", "suffix:...", "*")
    Pattern     string // 待匹配的目标字符串
    Replacement string // 替换目标字符串模板
}

// BodyRewriter 响应体动态重写器
type BodyRewriter struct {
    Rules []RewriteRule
    Types map[string]TypeDefinition
}
```

### 4.2 核心方法

1. **`MightMatchRequest(reqPath, uri string) bool`**：
   在发起网络请求前，快速根据路径上下文与文件扩展名排除 `.tgz`、`.whl`、`.deb` 等二进制包，避免不必要的流式直通拦截。
2. **`RuleMatchesResponse(r RewriteRule, reqPath, uri, contentType string) bool`**：
   根据上游返回的真实 `Content-Type` 和 URL 路径后缀，精准验证是否满足规则的类型约束，具备强二进制防护。
3. **`Apply(clientHost, reqPath, uri, contentType string, body []byte) []byte`**：
   将匹配规则中的 `{PROXY_HOST}` 动态展开为请求的主机名与端口，执行内存/流式字符串替换。

### 4.3 协议头自适应处理
在响应回写客户端前：
```go
// 1. 剥离可能导致客户端校验失败的原上游摘要头
respHeader.Del("ETag")
respHeader.Del("Content-MD5")

// 2. 修正 Content-Length
rewrittenBody := rewriter.Apply(clientHost, r.URL.Path, uri, resp.Header.Get("Content-Type"), rawBody)
respHeader.Set("Content-Length", strconv.Itoa(len(rewrittenBody)))
w.WriteHeader(resp.StatusCode)
w.Write(rewrittenBody)
```

---

## 5. 验证场景与测试标准 (Verification)

1. **类型谓词指令解析验证**：正确解析 `type <name> ...` 与带有 `type:json`、`type:pypi`、`mime:...`、`suffix:...` 及 3 参数缺省格式的 `rewrite` 规则；
2. **基本字符串动态替换**：文本响应中出现的 `https://files.pythonhosted.org/` 精准替换为 `http://127.0.0.1:8888/pythonhosted/`；
3. **`{PROXY_HOST}` 动态自适应**：客户端携带不同 `Host`（如 `10.0.0.1:8888` 与 `proxy.local:8888`）时，替换生成的 URL 动态匹配对应的 Host；
4. **二进制流安全绕过与正常落盘缓存**：下载 `.whl`、`.tgz`、`.deb` 等二进制安装包时，即使路径在 `/npm/` 或 `/pypi/` 作用域下，也能安全绕过重写引擎，正常进入单飞合并与磁盘持久化缓存；
5. **ETag 剥离与 Content-Length 一致性**：重写后客户端收到的响应无 ETag，Content-Length 严格等于重写后字节数；
6. **Gzip 响应透明解压重写**：当上游返回 Gzip 压缩文本时，自动解压重写，剥离 `Content-Encoding`。
