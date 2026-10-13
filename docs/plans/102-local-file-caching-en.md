# Local File Caching Mechanism Design Plan

## 1. Goal
Persist downloaded files to local disk storage. Subsequent requests for the same resource are served directly from the local cache without contacting upstream servers, significantly reducing external bandwidth consumption and lowering latency.

## 2. Core Mechanism
Adopts **Concurrent Write & Relay** and **Content-Length Integrity Verification**:

- **Concurrent Streaming & Atomic Rename**: On a cache miss, data from the upstream server is streamed to the client while simultaneously being written to a temporary file (`.tmp`). Once complete, the file is renamed atomically to the final cache path, preventing incomplete downloads from being served as valid cache.
- **Cache Hit Verification**: When a request arrives, the proxy checks if a local cache file exists. If it exists and matches the upstream `Content-Length` exactly, the proxy short-circuits remote downloading and streams directly from disk.
- **Configurable Storage Path**: The `-cache` CLI flag allows specifying any local storage directory, defaulting to the OS temporary directory if omitted.

## 3. Technical Solution

### 3.1 Cache Directory Configuration
```go
cache = flag.String("cache", "", "cache directory")

func cacheConfig() {
    if cache == nil || *cache == "" {
        *cache = os.TempDir()
    }
    if _, err := os.Stat(*cache); err != nil {
        log.Fatal("cache directory does not exist")
    }
}
```

### 3.2 Cache Hit Short-Circuit
```go
name := filepath.Join(*cache, getSafeUrlPath(uri))
stat, _ := os.Stat(name)
// Direct hit if file exists and matches full length
if stat != nil && stat.Size() == length {
    log.Printf("[%d] job cached: %s", reqID, uri)
    job.FilePath = name
    close(job.HeaderDone)
    return
}
```

### 3.3 Temporary File Creation and Persistence
1. Create parent directory: `os.MkdirAll(filepath.Dir(name), 0755)`.
2. Generate timestamped temporary file: `tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)`.
3. Rename temporary file to final path upon complete download: `os.Rename(tmp, name)`.

## 4. Benefits
1. **Bandwidth Savings**: Drastically reduces external bandwidth for frequently downloaded assets (installers, packages, base layers).
2. **Instant Delivery**: Cached files stream at local disk/LAN speeds.
3. **Data Integrity**: Guarded by `Content-Length` checks and atomic renames to prevent serving corrupted files.
