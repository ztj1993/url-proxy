# url-proxy

[中文文档](README-zh.md)

This is a simple but highly efficient URL proxy forwarding and caching program, specifically designed to accelerate large file downloads and optimize concurrent requests.

## Features

- **Request Coalescing**: When multiple clients request to download the same file simultaneously, the system makes only one real network request to the upstream server. All clients share this single download stream, significantly saving server bandwidth.
- **Idle Timeout Watchdog**: A built-in background inspection mechanism automatically detects and terminates upstream connections that have been inactive (hung) for a long time, preventing system resource (memory/file descriptor) leaks and ensuring long-term stability.
- **Safe Local Caching**: Automatically caches downloaded files to the local disk to prevent repeated downloads. It supports escaping complex URL parameters, making it compatible with various file systems like Windows.
- **Domain Forwarding**: Supports intelligently routing traffic to specific upstream servers or other `url-proxy` nodes based on the requested domain name.

## Binary

### Run
```bash
go run .\url-proxy.go --help
go run .\url-proxy.go -addr :8888
```

### Command-line Arguments
- `-addr`: HTTP server listening address, default is `:8888`
- `-cache`: Directory to store cached files, defaults to the system's temporary directory
- `-idle-time`: Idle timeout threshold. If the upstream server does not return new data within this duration, the connection is forcefully disconnected. Default is `10m` (10 minutes)

### Build
```bash
docker run --rm -v "$PWD":/srv -w /srv golang:1.18 ./build.sh dev
```

## Docker

### build
```bash
docker build -t ztj1993/url-proxy:latest .
```

### Run
```bash
docker run -d --name=uproxy --restart=always -p 8888:8888 ztj1993/url-proxy:latest
```

## Forward

Support forwarding to other url-proxy programs.

Create a `forwarding.conf` file in the current working directory or the directory where the executable is located.
The program will automatically detect and load it.

`forwarding.conf` example:
```text
github.com http://192.168.100.1:8888
google.com http://192.168.99.1:8888
```

run:
```bash
go run .\url-proxy.go
```
