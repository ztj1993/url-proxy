# Basic Proxy & Caching Foundation Plan (Plan 100)

## 1. Background & Goals

During the initial phase of the project, the primary objective was to build a lightweight, efficient, and non-intrusive general-purpose proxy and caching foundation to reduce outbound network bandwidth waste and download latency across multi-client environments.

Plan 100 focused on establishing the core data flow pipelines and persistent file caching capabilities:
1. **Transparent Proxy Forwarding**: Provide transparent streaming relay for HTTP, HTTPS, and FTP resources, faithfully forwarding HTTP status codes and response headers;
2. **Local File Caching**: Establish persistent on-disk storage with write-through on first fetch and direct local file serving on cache hits;
3. **Host Forwarding Rules**: Support rule-based host routing and gateway redirection;
4. **Cross-Platform Safe Cache Paths**: Mitigate filesystem character restrictions and path length limitations across operating systems (Windows, Linux, etc.) while preventing directory traversal attacks.

---

## 2. Sub-plans & Deliverables

Plan 100 comprises 4 core sub-plans (features):

| Feature | Name | Objectives & Responsibilities | Test Suite |
| :---: | :--- | :--- | :--- |
| **Feature 101** | **Basic Proxy Forwarding** | Target URI validation, streaming pipeline relay, and header preservation | `tests/101_basic_proxy` |
| **Feature 102** | **Local File Caching** | Write-through local persistence on cold requests and local hit retrieval | `tests/102_local_caching` |
| **Feature 103** | **Host Forwarding Rules** | Domain-based upstream gateway rewriting (later superseded by Feature 304) | `tests/103_host_forwarding` |
| **Feature 104** | **Safe Cache Path Generation** | URL-to-path sanitization and hashing, eliminating traversal risks across OS filesystems | `tests/104_safe_cache_path` |

---

## 3. Architecture & Core Mechanisms

### 3.1 Transparent Routing and Streaming Relay
When a client requests a resource through the proxy, the proxy parses the target protocol and destination address, streaming upstream chunks directly to the downstream client to maintain a minimal memory footprint without buffering entire payloads in memory.

### 3.2 Safe Cache Path Specifications
To resolve filesystem storage failures caused by reserved characters (colons, question marks, wildcards) or excessively long URLs, a whitelist-based escaping and hash-digesting path mapping mechanism was established:
- Reserved characters are sanitized or normalized;
- Oversized path components are compressed using cryptographic hash digests;
- Relative directory traversal elements (`..`) are strictly rejected.

### 3.3 Evolution & Replacement
The standalone host forwarding rules implemented in Feature 103 were later standardized and incorporated into the single source of truth configuration system introduced in Feature 304 under Plan 300.
