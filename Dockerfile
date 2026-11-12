# Build stage
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

WORKDIR /srv

COPY go.mod ./
COPY url-proxy.go ./
COPY internal ./internal

# Cross-compilation support via Buildx target args
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /bin/url-proxy .

# Runtime stage
FROM alpine:3.20

# Install CA certificates (required for HTTPS upstream proxying) and tzdata
RUN apk add --no-cache ca-certificates tzdata \
  && mkdir -p /cache /srv && chmod 777 /cache /srv

LABEL org.opencontainers.image.source="https://github.com/ztj1993/url-proxy" \
      org.opencontainers.image.description="A lightweight URL proxy and caching service" \
      maintainer="Ztj <ztj1993@gmail.com>"

COPY --from=builder /bin/url-proxy /usr/local/bin/url-proxy

# Copy default unified configuration
COPY proxy.conf /srv/proxy.conf

WORKDIR /srv
EXPOSE 8888

# Entrypoint wrapper: automatically prepends "url-proxy" if first argument starts with "-"
RUN printf '#!/bin/sh\nset -e\nif [ "${1#-}" != "$1" ]; then\n  set -- url-proxy "$@"\nfi\nexec "$@"\n' > /entrypoint.sh \
 && chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["url-proxy", "-addr", ":8888", "-conf", "/srv/proxy.conf", "-cache", "/srv/cache"]
