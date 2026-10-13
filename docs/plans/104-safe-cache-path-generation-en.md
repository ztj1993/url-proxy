# Safe Cache Path Generation Design Plan

## 1. Goal
Reliably map diverse and complex target URLs into valid, safe local filesystem paths. This resolves cross-platform filesystem issues (especially on Windows), strips illegal characters like colons from host ports, isolates requests with different query parameters, and normalizes slash-terminated URLs.

## 2. Core Mechanism
Adopts **Hierarchical Path Mapping** with **Sanitization & Query Escaping**:

1. **Protocol and Host/Port Normalization**:
   - The top-level folder is formatted as `<Scheme>-<Host>`.
   - Colons `:` in host ports (e.g. `localhost:8080`) are replaced with hyphens `-` (`localhost-8080`), eliminating Windows invalid path syntax errors.
2. **Path Hierarchy Preservation**:
   - Preserves intermediate directory structures from the original URL, avoiding flat directories containing excessive file counts.
3. **Query Parameter Isolation via Encoding**:
   - Concatenates the basename and raw query as `filename?query` and passes it through `url.QueryEscape`. This ensures requests sharing the same path but differing in query parameters (e.g. `/download?v=1` vs `/download?v=2`) are cached into distinct files without collisions or stale data.
4. **Slash Normalization**:
   - URLs ending in `/` are given a default file name of `index`.

## 3. Technical Solution

### 3.1 Algorithm Implementation (`getSafeUrlPath`)
```go
func getSafeUrlPath(rawUri string) string {
    u, err := url.Parse(rawUri)
    if err != nil {
        return url.QueryEscape(rawUri)
    }

    // 1. Scheme-Host-Port (Replace : with - for Windows compatibility)
    hostStr := strings.ReplaceAll(u.Host, ":", "-")
    schemeHostDir := u.Scheme + "-" + hostStr

    // 2. Request path
    pDir := path.Dir(u.Path)
    pBase := path.Base(u.Path)
    if strings.HasSuffix(u.Path, "/") {
        pDir = u.Path
        pBase = "index"
    }

    // 3. Query string concatenation and escape
    fileAndQuery := pBase
    if u.RawQuery != "" {
        fileAndQuery += "?" + u.RawQuery
    }
    encodedFileName := url.QueryEscape(fileAndQuery)

    return filepath.Join(schemeHostDir, filepath.FromSlash(pDir), encodedFileName)
}
```

### 3.2 Mapping Examples
| Original URL | Generated Local Path | Note |
| :--- | :--- | :--- |
| `http://localhost:9995/app.zip` | `http-localhost-9995/app.zip` | Colon in port replaced by `-` |
| `http://example.com/api/data?id=1&t=now` | `http-example.com/api/data%3Fid%3D1%26t%3Dnow` | Query escaped to separate cache |
| `http://example.com/repo/` | `http-example.com/repo/index` | Trailing slash normalized to index |

## 4. Benefits
1. **Full Windows Compatibility**: Completely prevents filesystem errors caused by colons or invalid naming rules.
2. **Query-Aware Isolation**: Different URL query parameters result in separate, non-conflicting cache files.
3. **Structured Storage**: Local directory layout mirrors the remote web hierarchy, making inspection and cleanup straightforward.
