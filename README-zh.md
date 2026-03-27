# url-proxy

[English Documentation](README.md)

这是一个简单但高效的网址代理转发与缓存程序，专为大文件下载加速和并发请求优化而设计。

## 核心特性 (Features)

- **并发请求合并 (Request Coalescing)**: 当多个客户端同时请求下载同一个文件时，系统只会向上游服务器发起一次真实的网络请求，所有客户端将共享这一个下载流，极大节省服务器带宽。
- **断网/挂起自动清理 (Idle Timeout Watchdog)**: 内置后台巡检机制，自动检测并切断长时间无数据传输（假死）的上游连接，防止系统资源（内存/句柄）泄漏，保证系统长期稳定运行。
- **安全的本地缓存**: 自动将下载的文件缓存到本地磁盘，避免重复下载。支持复杂 URL 参数的合法化转义，兼容 Windows 等各种文件系统。
- **自定义域名转发 (Domain Forwarding)**: 支持根据请求的域名，将流量智能转发给指定的上游服务器或其他的 `url-proxy` 节点。

## 二进制运行 (Binary)

### 运行
```bash
go run .\url-proxy.go --help
go run .\url-proxy.go -addr :8888
```

### 命令行参数说明
- `-addr`: HTTP 服务器监听地址，默认 `:8888`
- `-cache`: 缓存文件存储目录，默认使用系统临时目录
- `-idle-time`: 空闲超时时间，当上游服务器超过此时间未返回新数据时，将强制断开连接，默认 `10m` (10分钟)

### 构建
```bash
docker run --rm -v "$PWD":/srv -w /srv golang:1.18 ./build.sh dev
```

## Docker 运行

### 构建镜像
```bash
docker build -t ztj1993/url-proxy:latest .
```

### 启动容器
```bash
docker run -d --name=uproxy --restart=always -p 8888:8888 ztj1993/url-proxy:latest
```

## 转发配置 (Forward)

支持根据域名将请求转发到其它的 url-proxy 程序或上游服务器。

在当前工作目录或可执行文件所在目录下创建 `forwarding.conf` 文件，程序启动时会自动检测并加载该文件。

`forwarding.conf` 示例格式：
```text
github.com http://192.168.100.1:8888
google.com http://192.168.99.1:8888
```

运行程序即可自动加载：
```bash
go run .\url-proxy.go
```