# Feature 321: Modular Architecture Refactoring & Domain Layering (Modular Architecture Refactoring)

## 1. Feature Goals (Goal)

As `url-proxy` progressively expanded its supported ecosystem protocols, the system evolved from a simple single-file caching proxy into a comprehensive matrix supporting 8 major official registries (APT, Alpine, Go, Maven, PyPI, npm, Docker, Cargo), HTTPS CONNECT tunneling, Range 206 partial slicing, conditional 304 fast negotiation, unified configuration engine, selective cache bypass with whitelist priority arbitration, path short alias routing, streaming response body rewriting, route-scoped plugins, and dynamic metadata SWR soft caching. Having all core implementation logic concentrated in the single root file `url-proxy.go` caused the file to bloat to 1,759 lines, intertwining responsibilities, introducing concurrency risks around package-level global variables, and lacking native Go white-box unit tests.

**Feature 321 Objectives**:
1. **Separation of Concerns & Domain Layering**: Decouple the monolithic codebase into 7 highly cohesive, loosely-coupled private domain packages (`internal/config`, `internal/cache`, `internal/ttl`, `internal/rewriter`, `internal/plugin`, `internal/downloader`, `internal/proxy`) adhering to the Single Responsibility Principle;
2. **Object-Oriented Design & State Encapsulation**: Completely eliminate package-level mutable global variables and locks (`jobs`, `jobsMu`, `requestID`, `globalTTLManager`, `routePlugins`), managing task pools, SWR metadata, plugin registries, and dispatchers via struct instances and dependency injection;
3. **100% Backward Compatibility**: Preserve the lightweight root entrypoint `url-proxy.go` (slimming lines from 1,759 to ~70), maintaining full compatibility with `go run url-proxy.go` / `go run .`, cross-platform scripts (`build.bat` / `build.sh`), and Docker container builds;
4. **Native Go Unit Testing Closed-Loop**: Deliver dedicated `*_test.go` unit test suites for each domain subpackage, enabling automated quality validation via `go test ./...`.

---

## 2. Core Architecture & Dependency Design

### 2.1 Directory Structure Layout

```text
url-proxy/
??? url-proxy.go                # Main entrypoint (flag parsing, DI & wiring, ~70 lines)
??? proxy.conf                  # Production unified configuration template
??? go.mod                      # Module definition: uri-proxy
??? build.sh / build.bat        # Cross-platform multi-architecture build scripts
??? Dockerfile                  # Container build recipe
??? internal/
?   ??? config/                 # Unified configuration domain
?   ?   ??? config.go           # ProxyConfig, NoCacheRule, TypeDefinition, RewriteRule, Load, Parse
?   ?   ??? config_test.go      # Directives syntax, scoping, and regex precompilation tests
?   ??? cache/                  # Cache path & bypass arbitration domain
?   ?   ??? path.go             # GetSafeUrlPath (safe relative paths & query isolation)
?   ?   ??? bypass.go           # ShouldBypassCache, MatchBypassRule (strict whitelist priority)
?   ?   ??? dir.go              # EnsureCacheDir (cache directory lifecycle)
?   ?   ??? cache_test.go       # Path escape, query isolation & whitelist anti-bypass tests
?   ??? ttl/                    # Metadata soft TTL & SWR caching domain
?   ?   ??? manager.go          # TTLManager, TTLItem (thread-safe in-memory cache)
?   ?   ??? ttl_test.go         # Soft expiration, SWR refresh, and concurrent access tests
?   ??? rewriter/               # Response body dynamic rewriter domain
?   ?   ??? rewriter.go         # BodyRewriter, RuleMatchesResponse, ShouldRewrite, Apply
?   ?   ??? mime.go             # Type aliases, MIME/extension detection, binary safety checks
?   ?   ??? rewriter_test.go    # PEP 691 JSON, HTML tags, and Gzip stream rewrite tests
?   ??? plugin/                 # Route-scoped protocol plugin domain
?   ?   ??? plugin.go           # RoutePlugin interface and Registry definition
?   ?   ??? docker.go           # DockerPlugin (Blobs fast-path & manifest passthrough)
?   ?   ??? plugin_test.go      # Plugin matching, route scope, and fast-path tests
?   ??? downloader/             # Concurrent coalescing & state machine domain
?   ?   ??? job.go              # DownloadJob model, atomic rename, and dirty cleanup
?   ?   ??? manager.go          # Manager task pool (GetOrCreate, Remove, state encapsulation)
?   ?   ??? watchdog.go         # Idle Watchdog background routine
?   ?   ??? downloader_test.go  # Single-Flight coalescing, Cond broadcast, and timeout tests
?   ??? proxy/                  # Protocol proxy & top-level routing domain
?       ??? server.go           # Server entity & ServeHTTP main dispatcher
?       ??? router.go           # URL normalization, path alias rewriting, target resolution
?       ??? tunnel.go           # HTTPS CONNECT TCP tunnel hijacking
?       ??? range.go            # Range 206 local cache direct slicing & streaming
?       ??? bypass.go           # Cache bypass & SWR async background revalidation
?       ??? download.go         # Coalescing download coordination & shared stream feeding
?       ??? proxy_test.go       # Protocol loop, Range direct serve, and 304 fast shortcut tests
??? docs/                       # Architecture designs, feature specs, and user guides
??? tests/                      # Existing mock-server end-to-end integration test suites
```

### 2.2 Directed Acyclic Graph (DAG)

To eliminate Go circular import compilation errors (`import cycle not allowed`), a strict unidirectional dependency topology is enforced:

```
           main (url-proxy.go)
             ?   ?    ?    ?
             ?   ?    ?    ?
             ?   ?    ?  internal/proxy ????????????????????????
             ?   ?    ?    ?          ?         ?              ?
             ?   ?    ?    ?          ?         ?              ?
             ?  internal/downloader internal/rewriter internal/plugin internal/ttl
             ?                        ?         ?              ?
             ?                        ?         ?              ?
           internal/config ??????????????????? internal/cache ??
             ?
             ?
           Go Standard Library (net/http, sync, os, io)
```

---

## 3. Subpackage Specifications

### 3.1 `internal/config` (Configuration Domain)
- **Responsibility**: Single source of truth for immutable configuration models and directive parsing.
- **Key Structs**:
  - `ProxyConfig`: Stores `alias`, `no_cache`, `type`, `rewrite`, `plugin`, `metadata_ttl`, `metadata_swr` rules.
  - `NoCacheRule`, `TypeDefinition`, `RewriteRule`.
- **Design Decision**: Only depends on standard library, referenced by other internal packages without circular dependencies.

### 3.2 `internal/cache` (Cache Path & Bypass Domain)
- **Responsibility**: Manages cross-platform filesystem paths, cache directory lifecycle, and bypass arbitration.
- **Key Capabilities**:
  - `GetSafeUrlPath(rawUri string) string`: Sanitizes arbitrary URLs into safe relative disk paths, removing Windows reserved characters (`:`) and hashing query strings.
  - `ShouldBypassCache(rawUri string, rules []config.NoCacheRule, clientPaths ...string) bool`: Evaluates cache bypass rules with absolute priority granted to whitelist rules (`Action == "cache"`).
  - `EnsureCacheDir(customDir string) (string, error)`: Validates and initializes the root cache directory hierarchy.

### 3.3 `internal/ttl` (Metadata Soft TTL & SWR Domain)
- **Responsibility**: High-concurrency in-memory metadata soft caching lifecycle (Feature 318).
- **Key Structs**:
  - `TTLItem`: Holds cached metadata bytes, cloned HTTP headers, status code, ETag, Last-Modified, cached timestamp, and expiration deadline.
  - `TTLManager`: Struct-based RWMutex container replacing the package-level `globalTTLManager`.

### 3.4 `internal/rewriter` (Streaming Response Body Rewriter Domain)
- **Responsibility**: Performs dynamic on-the-fly URL rewrites inside index metadata streams (Feature 311).
- **Key Capabilities**:
  - `BodyRewriter`: Encapsulates rules and type definitions.
  - `ShouldRewrite`: Multi-dimensional match against scopes, file suffixes, and MIME types.
  - `isBinaryMimeType` / `isKnownBinaryPath`: Strict binary detection preventing corruption of archives and binary wheels.
  - `Apply`: Transparently handles Gzip decompression, `{PROXY_HOST}` substitution, and re-compression.

### 3.5 `internal/plugin` (Route-Scoped Protocol Plugin Domain)
- **Responsibility**: Extensible contract and performance plugins for specific ecosystems (Feature 317).
- **Key Capabilities**:
  - `RoutePlugin` interface: `Name() string`, `Match(req *http.Request, uri string) bool`, `FastPathServe(w http.ResponseWriter, r *http.Request, uri string, cacheDir string) bool`.
  - `DockerPlugin`: Specialized fast-path handler serving cached Docker Blobs directly without general proxy overhead.
  - `Registry`: Manages route-scoped plugin instances and execution dispatch.

### 3.6 `internal/downloader` (Concurrent Download Coalescing Domain)
- **Responsibility**: Single-Flight download request coalescing, atomic renaming, and idle watchdogs (Features 201, 202).
- **Key Capabilities**:
  - `DownloadJob`: Task state container with `sync.Cond`, active `Readers` atomic counter, and `context.CancelFunc`.
  - `Manager`: Encapsulated task pool replacing package-level `jobs` map and `jobsMu` mutex.
  - `TryRename(reqID uint64)`: Executes atomic rename when active readers reach zero; guarantees deletion of incomplete `.tmp` files on error.
  - `Watchdog`: Background routine canceling stalled tasks and unblocking waiting followers with errors.

### 3.7 `internal/proxy` (Protocol Proxy & Orchestration Domain)
- **Responsibility**: Standard `http.Handler` orchestrating all domain packages.
- **Key Capabilities**:
  - `Server`: Top-level handler holding references to all modular domain components.
  - `ServeHTTP`: Coordinates request processing:
    1. HTTPS CONNECT tunnel hijacking (`handleConnect`);
    2. URL resolution and short alias rewriting (`rewriteTargetURI`);
    3. Plugin fast-path detection (`FastPathServe`);
    4. HTTP Range 206 local slicing or upstream streaming (`handleRange`);
    5. Cache bypass and asynchronous SWR revalidation (`handleBypass`);
    6. Static asset coalescing downloads (`handleDownload`).

### 3.8 Root Entrypoint `url-proxy.go`
The root file is reduced from 1,759 lines to ~70 lines of clean wiring logic:
```go
func main() {
    flag.Parse()
    cfg := config.Load(*conf)
    cDir, err := cache.EnsureCacheDir(*cacheDir)
    if err != nil { log.Fatalf(...) }

    ttlMgr := ttl.NewManager()
    downMgr := downloader.NewManager(cDir, *idleTime)
    downMgr.StartWatchdog()
    defer downMgr.StopWatchdog()

    bodyRewriter := rewriter.NewBodyRewriter(cfg.Rewrites, cfg.Types)
    pluginRegistry := plugin.NewDefaultRegistry()

    proxyServer := proxy.NewServer(cfg, cDir, downMgr, ttlMgr, bodyRewriter, pluginRegistry)
    server := &http.Server{Addr: *addr, Handler: proxyServer}
    log.Fatal(server.ListenAndServe())
}
```

---

## 4. Testing & Verification

Each `internal` subpackage is equipped with native Go white-box unit tests:

### 4.1 Test Coverage Details
1. **`internal/config/config_test.go`**: Validates directive syntax parsing, comments/whitespace handling, malformed directives skip, and regex precompilation.
2. **`internal/cache/cache_test.go`**: Validates Windows path safety (colon sanitization), query string isolation, and strict whitelist priority.
3. **`internal/ttl/ttl_test.go`**: Validates soft expiration detection, SWR status refresh, concurrent access, and evictions.
4. **`internal/rewriter/rewriter_test.go`**: Validates PEP 691 JSON replacement, HTML tag rewriting, Gzip stream re-compression, and binary stream safety guards.
5. **`internal/plugin/plugin_test.go`**: Validates Docker Blobs matching, route scoping, and fast-path responses.
6. **`internal/downloader/downloader_test.go`**: Validates Leader/Follower role allocation, reader count synchronization, dirty cleanup, and watchdog cancellations.
7. **`internal/proxy/proxy_test.go`**: Validates short alias routing, Range 206 slicing, cache bypass streaming, and 304 Not Modified shortcuts.

### 4.2 Verification Execution
Run standard unit tests under WSL:
```bash
go test -v ./...
```
Output results demonstrate all packages passing:
- `uri-proxy/internal/config`: PASS
- `uri-proxy/internal/cache`: PASS
- `uri-proxy/internal/ttl`: PASS
- `uri-proxy/internal/rewriter`: PASS
- `uri-proxy/internal/plugin`: PASS
- `uri-proxy/internal/downloader`: PASS
- `uri-proxy/internal/proxy`: PASS

### 4.3 Build Compatibility
- `go build .`: Compiles cleanly into a standalone binary.
- `build.bat` & `build.sh`: Completely compatible with cross-compilation matrix.
- `Dockerfile`: Unaffected container build recipe.

---

## 5. Benefits & Summary

| Dimension | Pre-Refactoring | Post-Refactoring | Optimization Benefit |
| :--- | :--- | :--- | :--- |
| **Entrypoint Lines** | 1,759 lines | ~70 lines | Lines reduced by **96%**, ultra-clean entrypoint |
| **Responsibility Coupling** | All 18 features mixed in one file | 7 decoupled domain subpackages | Single Responsibility Principle, easy to extend |
| **State Concurrency Safety** | 5 package-level globals and mutexes | Object-oriented struct managers & DI | Clean isolation, controllable lifecycles |
| **Native Unit Testing** | Relies on external mock servers | Native Go white-box unit tests in each package | Enables fast `go test ./...` in CI workflows |
| **Workflow Compatibility** | Single-file build | Preserved root `url-proxy.go` entrypoint | Zero disruption to builds, scripts, and Docker |
