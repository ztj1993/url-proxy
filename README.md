# url-proxy

This is a simple URL proxy forwarding program, mainly used for file download.

这是一个简单的网址代理转发程序，主要用于文件的下载。

## Run 运行
```
go run .\url-proxy.go --help
go run .\url-proxy.go -addr :8888
```

## Build 构建
```
docker run --rm -v "$PWD":/srv -w /srv golang:1.18 ./build.sh dev
```
