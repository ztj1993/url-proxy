FROM docker.m.daocloud.io/library/golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY url-proxy.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /bin/url-proxy .

FROM docker.m.daocloud.io/library/alpine:latest

RUN apk add --no-cache ca-certificates tzdata curl \
  && mkdir -p /cache && chmod 777 /cache

COPY --from=builder /bin/url-proxy /usr/local/bin/url-proxy

EXPOSE 8888
WORKDIR /workspace

CMD ["url-proxy", "-addr", ":8888", "-conf", "/etc/url-proxy/proxy.conf", "-cache", "/cache"]
