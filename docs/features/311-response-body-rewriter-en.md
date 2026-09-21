# Feature 311: Streaming Response Body Rewriter Engine Specification

## 1. Goal

When configuring `url-proxy` to relay application-level package managers (such as PyPI or npm) from their official registries, a persistent structural hurdle emerges: upstream metadata (HTML simple indexes, JSON package listings) hardcodes package artifact download URLs to external CDN domains (e.g. `https://files.pythonhosted.org/...` or `https://registry.npmjs.org/...`). Clients parsing this metadata directly connect to external hosts, bypassing the proxy and rendering caching mechanisms useless.

**Feature 311 Goal**:
Introduce a generic **Response Body Rewriting Engine (`BodyRewriter`)** into the `url-proxy` forwarding pipeline:
1. **Config-Driven Directives & Unified Prefix Syntax**:
   - `type <name> [mime:<MIME>] [suffix:<Ext>]...` directive to declare reusable content-type aliases and groupings;
   - `rewrite <Scope> [TypeSpec] <Pattern> <Replacement>` directive supporting `type:<name>`, `mime:<pattern>`, `suffix:<ext>`, and legacy 3-argument syntax;
2. **Host Macro Interpolation**: Support `{PROXY_HOST}` to adapt replacement targets dynamically based on the client's `Host` header;
3. **MIME-Type & Extension Precision Filtering**: Restrict payload modifications strictly to declared textual content types, with binary protection (`isKnownBinaryPath`, `isBinaryMimeType`) to ensure `.whl`, `.tgz`, `.deb` files are cached properly and never altered;
4. **Header Adaptation**: Strip conflicting upstream `ETag` and `Content-MD5` headers, recalculating `Content-Length`;
5. **Encoding Negotiation & Transparent Gzip Handling**: Transmit `Accept-Encoding: identity` for rewriting scopes, and transparently decompress, rewrite, and strip `Content-Encoding` if an upstream returns gzip compressed content.

---

## 2. Architecture & Pipeline

```text
                     Client HTTP Request Arrives
                                 │
                                 ▼
               [Extract Host, Match Aliases & Rewrites]
                                 │
                 Potential Rewrite Target (MightMatchRequest)?
                 (Checks Scope & Excludes .tgz/.whl/etc.)
                                / \
                          Yes  /   \  No
                              /     \
                             ▼       ▼
          [Inject Accept-Encoding: identity]  [Standard Request]
                             │                 │
                             ▼                 ▼
                    [Forward Upstream Request] ◄┘
                             │
                             ▼
                    [Upstream HTTP Response]
                             │
            ShouldRewrite: Content-Type & TypeSpec Match?
            (Supports type:<name>, mime:<pattern>, suffix:<ext>)
                            / \
                      Yes  /   \  No (Binary files or non-target MIME)
                          /     \
                         ▼       ▼
             [Body Rewriting Engine]  [Standard Bypass / Cache Pipeline]
             - Transparent Gzip decompression if required
             - Replace Pattern with Replacement ({PROXY_HOST} expanded)
             - Strip ETag and Content-MD5, update Content-Length
                         │
                         ▼
              [Client Receives Rewritten Body]
```

---

## 3. Configuration Syntax (`proxy.conf`)

### 3.1 Directives

#### 1. Type Declaration Directive `type`
```ini
# Syntax: type <name> [mime:<MIME>] [suffix:<Ext>]...
type html mime:text/html suffix:.html suffix:.htm
type json mime:application/json suffix:.json
type pypi mime:text/html mime:application/vnd.pypi.simple* suffix:.html
```

> **Built-in Types**:
> Built-in default types include `html`, `json`, `xml`, `pypi`, and `text`. Users can reference them directly in `rewrite` without explicit definition.

#### 2. Body Rewriting Directive `rewrite`
```ini
# Syntax 1: Reference type alias (Recommended)
rewrite /pypi/simple/ type:pypi https://files.pythonhosted.org/ http://{PROXY_HOST}/pythonhosted/
rewrite /npm/         type:json https://registry.npmjs.org/     http://{PROXY_HOST}/npm/

# Syntax 2: Direct inline MIME pattern
rewrite /api/         mime:application/json https://api.upstream.com/ http://{PROXY_HOST}/api/

# Syntax 3: Direct inline file suffix
rewrite /docs/        suffix:.html          https://docs.upstream.com/ http://{PROXY_HOST}/docs/

# Syntax 4: Default 3-argument form (fallback to textual MIME types)
rewrite /common/      https://upstream.com/ http://{PROXY_HOST}/common/
```

### 3.2 Parameter Definitions
- **`<Scope>`**: Applicable URL path prefix (e.g. `/pypi/simple/`, `/npm/`);
- **`<TypeSpec>`**: Content type specification:
  - `type:<name>`: References a declared or built-in type alias;
  - `mime:<pattern>`: Direct matching on HTTP `Content-Type` header (wildcard `*` supported);
  - `suffix:<ext>`: Direct matching on URL path extension;
  - `*` or omitted: Defaults to all general textual types.
- **`<MatchPattern>`**: String sequence to find in the textual body;
- **`<Replacement>`**: Replacement template, supporting `{PROXY_HOST}` macro variable (runtime client `r.Host`).

---

## 4. Verification & Testing

1. **Directive Parsing**: Verified parsing of `type` directives alongside `type:json`, `type:pypi`, `mime:...`, `suffix:...`, and legacy 3-arg syntax;
2. **Basic String Replacement**: Text occurrences of `https://files.pythonhosted.org/` accurately transform to `http://127.0.0.1:8888/pythonhosted/`;
3. **`{PROXY_HOST}` Dynamic Adaptation**: Different client hosts (`10.0.0.1:8888` vs `proxy.local:8888`) dynamically generate corresponding host URLs;
4. **Binary Stream Bypass & Normal Disk Caching**: Binary files (`.whl`, `.tgz`, `.deb`) safely bypass rewriting even under `/npm/` or `/pypi/`, entering standard SingleFlight disk cache;
5. **ETag & Content-Length Consistency**: Upstream ETags are removed and Content-Length precisely matches the rewritten bytes;
6. **Transparent Gzip Handling**: Gzip-compressed responses are decompressed, rewritten, and served cleanly without compression headers.
