# Host Forwarding Rules Design Plan (DEPRECATED)

> [!WARNING]
> **【DEPRECATED】**
> - **Deprecation Status**: The standalone `forwarding.conf` configuration file and bare two-field syntax (`<Host> <Prefix>`) described in this document have been **completely removed with zero backward compatibility**.
> - **Evolutionary Replacement**: Host forwarding capabilities have been merged into **Feature 304 Unified Proxy Configuration System (`proxy.conf`)**, standardizing on the single-source-of-truth directive syntax: `forward <source_host> <gateway_base_url>`. For the modern specification, please see Feature 304 Unified Proxy Configuration System (`proxy.conf`).
> - **Testing Status**: The corresponding test suite `tests/103_host_forwarding` has been marked as deprecated and is explicitly SKIPPED in subsequent automated test runs. Please refer to `tests/304_unified_config` for active test coverage.

This document is preserved solely for historical reference.

## 1. Goal
Support domain-specific (Host) rewriting or prefix forwarding rules via configuration. When `url-proxy` requests resources from a matching host, it automatically routes the upstream request through a configured mirror gateway or accelerator, making it seamless to use private mirrors or regional caching proxies.

## 2. Core Mechanism
Adopts **Rule-based Host Rewriting**:

- **Automatic Configuration Discovery**: On startup, `url-proxy` searches for `forwarding.conf` in both the current working directory and the executable directory, requiring no extra CLI flags.
- **Line-based Rule Parsing**: The configuration file is read line-by-line in the format `<TargetHost> <ForwardTargetPrefix>`, delimited by whitespace.
- **Dynamic URI Prefix Prepending**: When a requested URL's host matches a configured entry, the upstream URI is dynamically transformed into `<ForwardTargetPrefix>/<OriginalURI>`.

## 3. Technical Solution

### 3.1 Configuration File Format (`forwarding.conf`)
```text
# Syntax: <Host> <Target-URL-Prefix>
registry-1.docker.io http://mirror.local/docker
github.com https://ghproxy.example.com
```

### 3.2 Automated Rule Loading (`forwardConfig`)
```go
func forwardConfig() map[string]string {
    forwards := make(map[string]string)
    configFiles := []string{
        "forwarding.conf", // Working directory
    }
    if exePath, err := os.Executable(); err == nil {
        exeDir := filepath.Dir(exePath)
        configFiles = append(configFiles, filepath.Join(exeDir, "forwarding.conf"))
    }
    // Scan and parse line by line
    ...
    return forwards
}
```

### 3.3 Runtime Target Rewriting
```go
targetUri := uri
if _, ok := forwards[uris.Host]; ok {
    targetUri = forwards[uris.Host] + "/" + uri
}
log.Printf("[%d] job download: %s", reqID, targetUri)
```

## 4. Benefits
1. **Transparent Redirection**: Clients keep their standard target URLs, while proxies automatically route traffic to faster mirrors or intranet proxies.
2. **Zero-CLI Overhead**: Automatically discovers configuration files placed in the working directory.
3. **Granular Control**: Each target domain can have its own custom mirror gateway.
