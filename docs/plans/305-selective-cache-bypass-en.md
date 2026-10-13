# Configurable Cache Bypass Rules and Anti-False-Positive Engine Design Specification

## 1. Goal

In APT caching proxy relay scenarios, address two critical challenges:
1. **Dynamic Metadata Cache Bypass**: Repository metadata index files (`InRelease`, `Release`, `Release.gpg`, `Packages.xz`, etc.) update frequently as upstream packages are released. If these files are cached permanently on disk by the proxy, clients receive stale checksums, causing `Hash Sum mismatch` errors or blocking software updates. This feature ensures all requests matching bypass rules are **streamed transparently with zero disk persistence**.
2. **Configuration-Driven Anti-False-Positive Protection**: Completely eliminate hardcoded file extensions from Go code. By declaring `cache` directives in `proxy.conf` (e.g. `cache * suffix .deb`), software package files receive **absolute highest priority**. Even if a package name begins with `Packages` (e.g., `Packages-common_1.0_amd64.deb`) or resides under sensitive path prefixes, it is guaranteed 100% safe caching to disk.
3. **Multi-Tier Scoping Context**: Support global scope (`*`), path prefix scope (`/dists/`), host scope, and composite scopes to strictly isolate packages under `/pool/` from metadata under `/dists/`.

---

## 2. Core Mechanism & Arbitration

### 2.1 Rule Priority Arbitration
When a client request arrives (e.g., `http://archive.ubuntu.com/ubuntu/dists/jammy/Packages.xz`):
```
                       Incoming Request URI
                                │
                                ▼
                   Iterate through CacheRules
                                │
                 Matches any Action == "cache" rule?
                               / \
                         Yes  /   \  No
                             /     \
                            ▼       ▼
                       [FORCE CACHE]  Matches any Action == "bypass" rule?
                       (Exit Bypass)        / \
                                      Yes  /   \  No
                                          /     \
                                         ▼       ▼
                                  [STREAM RELAY]  [NORMAL DISK CACHE]
                                  (Never to disk)
```

1. **`cache` Rules Absolute Precedence**: If a request matches any rule declared as `cache` (whitelist exceptions), it is immediately designated for disk caching, short-circuiting out of bypass checks.
2. **`bypass` Rule Routing**: If no `cache` rule matches, but a `bypass` rule matches, the resource is flagged as dynamic metadata and streamed transparently.
3. **Default Behavior**: If neither matches, standard `url-proxy` caching takes place (coalesced download and disk caching).

### 2.2 Transparent Streaming Pipeline (Zero Disk I/O)
Workflow for dynamic metadata requests matching `bypass` rules:
1. **Forwarding Gateway Support**: If the target host matches a `forward` directive, the request URI is automatically rewritten with the mirror gateway prefix.
2. **Relay Negotiation & Control Headers**: Transparently forward client headers: `If-Modified-Since`, `If-None-Match`, `Range`, `If-Range`, `Cache-Control`, and `User-Agent`.
3. **Relay Status Code & Body**:
   - When upstream returns `304 Not Modified`, return 304 directly to the client with zero-byte payload transfer;
   - When upstream returns `200 OK` or `206 Partial Content`, stream data directly to client via `io.Copy(w, resp.Body)`;
   - **Core Safety Guarantee**: Never create `.tmp` temporary files and perform zero disk writes, preventing stale cache poisoning.

---

## 3. Architecture & Implementation (Technical Solution)

### 3.1 Rule Matching Engine (`url-proxy.go`)
```go
// MatchScope checks if request Host and Path match the rule's scope
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

// MatchPattern checks if request path and filename match pattern criteria
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

// shouldBypassCache determines if a request should bypass disk caching
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
			return false // Whitelist rule takes absolute priority
		}
		if rule.Action == "bypass" {
			hasBypassMatch = true
		}
	}
	return hasBypassMatch
}
```

### 3.2 Handler Pipeline Integration
In `Handler`, immediately evaluate `shouldBypassCache` following URI validation:
- On match: directly initiate an upstream streaming request, relaying response headers and body without touching disk;
- On miss: proceed to the standard Range checking and request coalescing caching pipeline.

---

## 4. Verification & Testing Standards

1. **Dynamic Metadata Cache Bypass**: Query `/dists/jammy/InRelease`; verify live content is received and 0 bytes are created in the disk cache directory.
2. **Anti-False-Positive Whitelist Protection**: Query `/pool/main/p/packages-base/packages-base_1.0_amd64.deb` (filename contains `packages-` keyword under `/dists/ prefix Packages` rule); verify it is successfully cached to disk due to `cache * suffix .deb`.
3. **Scope Protection**: Query `/pool/main/r/release-info/Release` (named `Release` but located outside `/dists/`); verify it is safely cached to disk.
4. **Negotiation Passthrough**: Query a bypass resource with `If-Modified-Since`; verify 304 is cleanly short-circuited back to the client.
