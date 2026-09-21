# Go Modules (GOPROXY) 缓存代理使用指南

`url-proxy` 原生 100% 完美支持 Go Modules 的 `GOPROXY` 协议规范。Go 模块代理是标准且优雅的纯 HTTP RESTful 协议，客户端不会向外部跳转绝对链接，所有资源均严格通过 `$GOPROXY` 统一拉取。

---

## 1. 服务端配置 (`proxy.conf`)

在 `proxy.conf` 中追加 Go 代理规则（上游推荐使用 `https://goproxy.cn` 或官方 `https://proxy.golang.org`）：

```ini
# --- 声明 GOPROXY 上游镜像别名 ---
alias /goproxy https://goproxy.cn

# --- 动静分离规则：动态版本发现元数据实时透传 ---
# 模块版本列表文件随时可能由于上游发版而更新
bypass /goproxy/ suffix /@v/list
bypass /goproxy/ suffix /@latest

# --- 强制缓存规则：模块源码包与版本元信息 100% 永久安全落盘 ---
# Go 模块的版本内容一旦发布具有强不可变性 (Immutable)
cache  /goproxy/ suffix .zip
cache  /goproxy/ suffix .mod
cache  /goproxy/ suffix .info
```

启动 `url-proxy`：
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. 客户端配置

在开发机、CI/CD Runner 或 Docker 镜像构建环境中配置 Go 环境变量：

```bash
# 将 GOPROXY 指向 url-proxy 本地代理 (推荐在结尾追加 ,direct 作为兜底)
go env -w GOPROXY=http://192.168.1.100:8888/goproxy,direct
```

**在 Dockerfile 中的应用示例**：
```dockerfile
FROM golang:1.22
WORKDIR /app

# 注入代理配置，大幅度加速依赖拉取 (go mod download)
ENV GOPROXY=http://192.168.1.100:8888/goproxy,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /server .
```

---

## 3. 运行与验证

在 Go 项目中执行依赖下载：

```bash
# 强制清除本地模块缓存后重新拉取
go clean -modcache
go mod download
```

**服务端日志与加速表现**：
- 查询 `.../@v/list`：标记为 `[BYPASS]`，直接回源拉取最新版本列表；
- 下载 `.../@v/v1.2.3.zip`、`.../@v/v1.2.3.mod`：首次请求写入磁盘缓存，后续相同版本的依赖下载直接由 `url-proxy` 毫秒级本地响应，不再产生外网请求。
