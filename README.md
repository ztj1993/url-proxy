# url-proxy

This is a simple URL proxy forwarding program, mainly used for file download.

这是一个简单的网址代理转发程序，主要用于文件的下载。

## Binary

### Run
```
go run .\url-proxy.go --help
go run .\url-proxy.go -addr :8888
```

### Build
```
docker run --rm -v "$PWD":/srv -w /srv golang:1.18 ./build.sh dev
```

## Docker

### build
```
docker build -t ztj1993/url-proxy:latest .
```

## Run
```
docker run -d --restart=always -p 8888:8888 ztj1993/url-proxy:latest
```
