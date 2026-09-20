# 跨平台安全缓存路径机制设计方案

## 1. 功能目标 (Goal)
将各种复杂的原始请求 URL 可靠且安全地映射为本地文件系统路径，解决不同操作系统（尤其是 Windows）文件路径规范冲突、端口冒号非法字符、带参请求（Query Parameter）缓存碰撞冲突、以及末尾斜杠无文件名落盘失败等问题。

## 2. 核心机制 (Core Mechanism)
采用 **分层目录映射 (Hierarchical Path Mapping)** 与 **非法字符过滤/Query 编码 (Safe Sanitation & Query Escaping)**：

1. **协议与主机端口规整化**：
   - 目录第一级为 `<Scheme>-<Host>`。
   - 若主机带有自定义端口（如 `localhost:8080`），将冒号 `:` 统一替换为短横线 `-`（如 `localhost-8080`），杜绝 Windows 下因冒号导致的非法文件名错误（`The filename, directory name, or volume label syntax is incorrect`）。
2. **中间路径层级保留**：
   - 保持原始 URL 内部的目录层级结构，避免扁平化哈希导致单一目录文件数量过多。
3. **文件名与参数编码隔离**：
   - 将末级文件名与 QueryString 拼接为 `filename?query`，整体进行 `url.QueryEscape` 编码。这确保了相同路径但不同参数的请求（如 `/download?v=1` 和 `/download?v=2`）具有各自独立的文件，不会相互覆盖或脏读。
4. **斜杠结尾归一化**：
   - 若 URL 以 `/` 结尾（无显式文件名），则统一归一化文件名为 `index`。

## 3. 技术方案 (Technical Solution)

### 3.1 核心算法实现 (`getSafeUrlPath`)
```go
func getSafeUrlPath(rawUri string) string {
    u, err := url.Parse(rawUri)
    if err != nil {
        return url.QueryEscape(rawUri)
    }

    // 1. 协议-域名-端口 (替换 : 为 - 防止 Windows 路径错误)
    hostStr := strings.ReplaceAll(u.Host, ":", "-")
    schemeHostDir := u.Scheme + "-" + hostStr

    // 2. 请求路径
    pDir := path.Dir(u.Path)
    pBase := path.Base(u.Path)
    if strings.HasSuffix(u.Path, "/") {
        pDir = u.Path
        pBase = "index"
    }

    // 3. 文件和参数拼接后编码当文件
    fileAndQuery := pBase
    if u.RawQuery != "" {
        fileAndQuery += "?" + u.RawQuery
    }
    encodedFileName := url.QueryEscape(fileAndQuery)

    return filepath.Join(schemeHostDir, filepath.FromSlash(pDir), encodedFileName)
}
```

### 3.2 映射样例
| 原始 URL | 本地生成安全相对路径 | 说明 |
| :--- | :--- | :--- |
| `http://localhost:9995/app.zip` | `http-localhost-9995/app.zip` | 端口冒号替换为 `-` |
| `http://example.com/api/data?id=1&t=now` | `http-example.com/api/data%3Fid%3D1%26t%3Dnow` | Query 参数编码落盘 |
| `http://example.com/repo/` | `http-example.com/repo/index` | 斜杠结尾归一为 index |

## 4. 预期收益 (Benefits)
1. **Windows 平台完美兼容**：彻底根除跨平台因盘符、冒号等字符引发的创建文件崩溃。
2. **细粒度多参数支持**：同一 API 路由下不同入参的下载请求均能获得正确隔离的独立缓存。
3. **文件结构清晰直观**：磁盘上的缓存目录与真实网站结构保持镜像对应，运维排查与手动清理极为方便。
