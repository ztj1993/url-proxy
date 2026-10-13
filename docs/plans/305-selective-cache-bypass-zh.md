# 可配置免缓存规则与防误杀机制设计方案

## 1. 功能目标 (Goal)

在 APT 代理中继场景中，解决两类核心痛点：
1. **动态元数据免缓存 (Cache Bypass)**：APT 仓库索引文件（如 `InRelease`、`Release`、`Release.gpg`、`Packages.xz` 等）会随上游软件发布频繁更新。若此类文件被代理落盘缓存，会导致客户端获取到过期的索引校验值，引发 `Hash Sum mismatch` 或无法获取最新软件更新。本功能确保所有命中免缓存规则的请求执行**纯流式透明透传，严禁落盘**。
2. **配置驱动的白名单防误杀 (Anti-False-Positive Protection)**：彻底移除代码中对包后缀的硬编码，通过在统一配置文件 `proxy.conf` 中声明 `cache` 动作（如 `cache * suffix .deb`），赋予安装包文件**绝对最高优先级**。即便软件包名称以 `Packages` 开头（如 `Packages-common_1.0_amd64.deb`）或路径位于敏感前缀下，也能 100% 确保安全落盘缓存，绝不误判。
3. **多层级作用域精细化控制 (Scoping Context)**：支持全局作用域（`*`）、路径前缀作用域（`/dists/`）、域名作用域以及复合作用域，有效隔离 `/pool/` 下的软件包与 `/dists/` 下的元数据。

---

## 2. 核心机制与匹配仲裁 (Core Mechanism & Arbitration)

### 2.1 规则优先级仲裁机制
当收到客户端请求（如 `http://archive.ubuntu.com/ubuntu/dists/jammy/Packages.xz`）时：
```
                       收到目标 URI 请求
                              │
                              ▼
                遍历 proxy.conf 中的 CacheRules
                              │
               匹配到任意一条 Action == "cache" 规则?
                             / \
                       是   /   \  否
                           /     \
                          ▼       ▼
                     [强制落盘]   命中任意 Action == "bypass" 规则?
                     (退出免缓存)       / \
                                  是 /   \ 否
                                    /     \
                                   ▼       ▼
                             [纯流式透传]  [默认落盘缓存]
                             (绝不落盘)
```

1. **`cache` 规则绝对优先**：若请求匹配任意一条声明为 `cache` 的白名单例外规则，立即判定为强制缓存落盘，退出免缓存判定。
2. **`bypass` 规则分流**：若未命中任何 `cache` 规则，但命中了 `bypass` 规则，判定为免缓存动态资源，执行纯流式实时透传。
3. **默认行为**：若未命中任何规则，执行 `url-proxy` 的默认行为——并发合并下载并落盘缓存。

### 2.2 纯流式透传流水线 (Zero Disk I/O)
命中 `bypass` 规则的动态元数据请求处理流程：
1. **支持网关转发**：若请求目标 Host 声明了 `forward` 映射，目标 URI 自动重写为镜像网关前缀。
2. **透传协商与范围控制头**：透明向上游中继客户端发起的 `If-Modified-Since`、`If-None-Match`、`Range`、`If-Range`、`Cache-Control` 及 `User-Agent`。
3. **透传状态码与响应体**：
   - 上游返回 `304 Not Modified` 时，直接向客户端返回 304，零 Body 传输；
   - 上游返回 `200 OK` 或 `206 Partial Content` 时，通过 `io.Copy(w, resp.Body)` 实时泵送回客户端；
   - **核心安全保障**：整个过程不创建 `.tmp` 临时文件，不触发任何磁盘写入，杜绝脏切片或旧索引污染。

---

## 3. 架构设计与核心实现 (Technical Solution)

### 3.1 规则匹配引擎 (`url-proxy.go`)
```go
// MatchScope 检查请求 Host 与 Path 是否匹配规则作用域
func (r *NoCacheRule) MatchScope(reqHost, reqPath string) bool {
	if r.Scope == "" || r.Scope == "*" {
		return true
	}
	scopeLower := strings.ToLower(r.Scope)
	reqHostLower := strings.ToLower(reqHost)
	reqPathLower := strings.ToLower(reqPath)

	if strings.HasPrefix(scopeLower, "/") {
		return strings.Contains(reqPathLower, scopeLower)
	}
	hostAndPath := reqHostLower + reqPathLower
	if strings.Contains(scopeLower, "/") {
		return strings.Contains(hostAndPath, scopeLower)
	}
	return reqHostLower == scopeLower || strings.HasPrefix(reqHostLower, scopeLower+":")
}

// MatchPattern 检查 URL 路径及文件名是否符合匹配模式
func (r *NoCacheRule) MatchPattern(urlPath, baseName string) bool {
	baseLower := strings.ToLower(baseName)
	patternLower := strings.ToLower(r.Pattern)

	switch r.Type {
	case "exact":
		return baseLower == patternLower
	case "prefix":
		return strings.HasPrefix(baseLower, patternLower)
	case "suffix":
		return strings.HasSuffix(baseLower, patternLower)
	case "regex":
		if r.Regexp != nil {
			return r.Regexp.MatchString(urlPath) || r.Regexp.MatchString(baseName)
		}
	}
	return false
}

// shouldBypassCache 根据规则判定请求是否应当免除本地落盘缓存
func shouldBypassCache(rawUri string, rules []NoCacheRule) bool {
	if len(rules) == 0 {
		return false
	}
	u, err := url.Parse(rawUri)
	if err != nil {
		return false
	}
	reqHost := u.Host
	reqPath := u.Path
	baseName := path.Base(reqPath)

	var hasBypassMatch bool
	for _, rule := range rules {
		if !rule.MatchScope(reqHost, reqPath) || !rule.MatchPattern(reqPath, baseName) {
			continue
		}
		if rule.Action == "cache" {
			return false // 白名单规则绝对优先
		}
		if rule.Action == "bypass" {
			hasBypassMatch = true
		}
	}
	return hasBypassMatch
}
```

### 3.2 Handler 请求分流集成
在 `Handler` 校验请求合法性后立即执行 `shouldBypassCache` 判定：
- 命中时直接向上游发起流式请求并中继响应头与数据；
- 未命中时继续执行后续 Range 切片与文件合并落盘管线。

---

## 4. 验证与测试标准 (Verification)

1. **动态元数据免缓存验证**：请求 `/dists/jammy/InRelease`，验证返回实时内容且磁盘缓存目录中无任何文件生成（0 字节写入）。
2. **白名单防误杀验证**：请求 `/pool/main/p/packages-base/packages-base_1.0_amd64.deb`（文件名包含 `packages-` 敏感词且存在 `/dists/ prefix Packages` 规则），验证基于 `cache * suffix .deb` 成功落盘缓存。
3. **作用域保护验证**：请求 `/pool/main/r/release-info/Release`（名称为 `Release` 但不在 `/dists/` 作用域下），验证正确执行落盘缓存。
4. **透传协商验证**：对免缓存动态资源携带 `If-Modified-Since` 请求，验证上游返回 304 时代理准确短路回传 304。
