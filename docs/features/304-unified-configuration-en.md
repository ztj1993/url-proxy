# Unified Proxy Configuration System Design Specification

## 1. Goal

Build a unified, standardized, and centralized proxy configuration system (`proxy.conf`) to resolve configuration fragmentation and CLI flag proliferation:
1. **Single Source of Truth (`proxy.conf`)**: Centralize path short alias routing (`alias`), host forwarding (`forward`), dynamic cache bypass (`bypass`/`nocache`), and cache exceptions (`cache`) in a single master configuration file.
2. **Configuration-Driven, Zero Code Hardcoding**: All routing and caching behaviors are defined purely via configuration; the Go codebase contains zero hardcoded package formats or path prefixes.
3. **Single-Line Directive Syntax**: Adopt a clean, consistent single-line directive format that maximizes readability and eases automated DevOps audits and script generation.
4. **Unified CLI Flag & Multi-Tier Discovery**: Provide a standard `-conf <path>` CLI flag with automatic multi-tier discovery (custom CLI path -> current working directory -> executable directory).

---

## 2. Syntax Specification & Directives (`proxy.conf`)

The configuration file supports blank lines and comments starting with `#`. Rules are formatted as single-line directives separated by spaces or tabs:

### 2.1 Directive Reference

| Directive | Full Syntax Format | Description | Target Feature |
| :--- | :--- | :--- | :--- |
| **`alias`** | `alias <short_alias> <upstream_base_url>` | Path short alias routing and dynamic rewriting | Feature 306 |
| **`forward`** | `forward <source_host> <gateway_base_url>` | Upstream host redirection and mirror gateway forwarding | Feature 103 |
| **`bypass`**<br>*(or `nocache`)* | `bypass <Scope> <Type> <Pattern>` | Dynamic resource cache bypass with transparent streaming | Feature 305 |
| **`cache`** | `cache <Scope> <Type> <Pattern>` | Whitelist exceptions forced to disk cache (takes precedence over bypass) | Feature 305 |

### 2.2 Scopes and Match Types
- **Scope**:
  - `*`: Global scope; matches all domains and paths.
  - `/prefix/`: Path prefix scope; matches when request path contains the specified prefix.
  - `domain.com`: Host scope; matches specific upstream host.
  - `domain.com/prefix/`: Composite scope matching both host and path.
- **Type**:
  - `exact`: Exact filename match (case-insensitive).
  - `prefix`: Filename prefix match.
  - `suffix`: Filename suffix / extension match.
  - `regex`: Regular expression match against full URL path or filename.

---

## 3. Architecture & Implementation (Technical Solution)

### 3.1 Core Data Structures (`ProxyConfig`)
```go
// NoCacheRule defines rules for cache bypass and whitelist exceptions
type NoCacheRule struct {
	Scope   string         // Scope: "*", "/dists/", "archive.ubuntu.com", etc.
	Action  string         // Action: "bypass" or "cache"
	Type    string         // Type: "exact", "prefix", "suffix", "regex"
	Pattern string         // Pattern string
	Regexp  *regexp.Regexp // Pre-compiled regex when Type is "regex"
}

// ProxyConfig holds all configuration parsed from proxy.conf
type ProxyConfig struct {
	Aliases      map[string]string // Short alias mapping: alias -> upstreamBaseUrl
	Forwards     map[string]string // Host forwarding: host -> gatewayBaseUrl
	NoCacheRules []NoCacheRule     // Ordered cache bypass and whitelist exception rules
	SourceFile   string            // Resolved configuration file path
}
```

### 3.2 Multi-Tier Discovery Flow
```
                     Start url-proxy -conf <path>
                                  │
         Explicit CLI argument provided & exists? ── Yes ──> Parse specified config
                                  │ No
                 proxy.conf exists in CWD? ───────── Yes ──> Parse CWD/proxy.conf
                                  │ No
               proxy.conf exists in ExeDir? ──────── Yes ──> Parse ExeDir/proxy.conf
                                  │ No
                                  ▼
                    No config found; use empty default configuration
```

### 3.3 Parser Engine (`parseProxyConfig`)
The parser reads the configuration stream line by line:
1. Strips leading and trailing whitespace and ignores comments.
2. Identifies the primary directive token (case-insensitive).
3. Dispatches tokens to `alias`, `forward`, `bypass`/`nocache`, and `cache` handlers.
4. Logs a warning and safely skips any unrecognized directive.
5. Regular expressions are pre-compiled and validated during the parsing phase.

---

## 4. Verification & Testing Standards
1. **Full Directive Parsing**: Verify parsing accuracy for `alias`, `forward`, `bypass`, and `cache`.
2. **CLI Override**: Verify that `-conf <custom.conf>` takes precedence over default discovery paths.
3. **Auto-Discovery**: Verify automatic discovery and loading of `proxy.conf` in the working directory.
