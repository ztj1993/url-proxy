FROM golang:1.18 as builder

LABEL maintainer="Ztj <ztj1993@gmail.com>"

COPY ./url-proxy.go /srv/url-proxy.go
COPY ./go.mod /srv/go.mod

RUN cd /srv \
  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o uproxy_linux_amd64 \
  && chmod +x ./uproxy_linux_amd64


FROM alpine:3.11.6

LABEL maintainer="Ztj <ztj1993@gmail.com>"

COPY --from=builder /srv/uproxy_linux_amd64 /bin/uproxy_linux_amd64

CMD ["uproxy_linux_amd64"]
