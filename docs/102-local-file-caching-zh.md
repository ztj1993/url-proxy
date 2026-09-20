# 本地文件缓存机制设计方案

## 1. 功能目标 (Goal)
将通过代理下载成功的远程文件持久化缓存在本地磁盘。后续相同资源的下载请求直接由本地缓存提供服务，无需再次向上游发起网络请求，从而显著节省外网带宽并降低延迟。

## 2. 核心机制 (Core Mechanism)
采用 **边流式响应边写缓存 (Concurrent Write & Relay)** 与 **大小完整性校验 (Content-Length Verification)**：

- **边读边写与原子重命名**：首次请求（Cache Miss）时，代理将远端流式数据一边传给客户端，一边写入临时缓存文件（`.tmp` 后缀）。下载完成后通过原子重命名转为正式缓存文件，防止中断导致残缺文件被当作有效缓存。
- **缓存命中判断 (Cache Hit)**：当请求到达时，首先检查本地是否存在对应的缓存文件。若存在且文件大小与上游响应头中的 `Content-Length` 完全一致，则判定为有效命中，直接读取本地文件返回。
- **灵活的缓存目录配置**：通过命令行参数 `-cache` 支持自定义本地缓存目录，若未指定则默认回退至系统的临时目录。

## 3. 技术方案 (Technical Solution)

### 3.1 缓存目录配置
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

### 3.2 缓存命中与短路
```go
name := filepath.Join(*cache, getSafeUrlPath(uri))
stat, _ := os.Stat(name)
// 如果缓存已存在且完整，直接使用
if stat != nil && stat.Size() == length {
    log.Printf("[%d] job cached: %s", reqID, uri)
    job.FilePath = name
    close(job.HeaderDone)
    return
}
```

### 3.3 写入临时文件与重命名落盘
1. 创建父目录：`os.MkdirAll(filepath.Dir(name), 0755)`。
2. 创建带时间戳的临时文件：`tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)`。
3. 数据流写入完成后，触发文件重命名落盘 `os.Rename(tmp, name)`。

## 4. 预期收益 (Benefits)
1. **带宽大幅缩减**：对于频繁被多客户端拉取的静态资产（安装包、依赖包、容器镜像层等），仅首次下载消耗外网带宽。
2. **极速下载体验**：命中本地缓存后，下载速度仅受限于内网或本地磁盘 IO。
3. **数据一致性保证**：基于 `Content-Length` 校验与临时文件机制，杜绝脏读与损坏缓存。
