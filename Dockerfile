FROM golang:1.22 AS gobuild
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.version=$(cat VERSION)" -o /out/flux-agent ./cmd/flux-agent

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Install system dependencies
RUN apt-get update && apt-get install -y \
    redis-server \
    redis-sentinel \
    redis-tools \
    supervisor \
    curl \
    jq \
    gnupg \
    ca-certificates \
    net-tools \
    procps \
    openssl \
    xxd \
    gnutls-bin \
    && rm -rf /var/lib/apt/lists/*

# Create necessary directories
RUN mkdir -p /etc/redis /app /var/log/supervisor /var/lib/redis/data /etc/ssl/cluster/ca /etc/ssl/cluster/redis /etc/ssl/cluster/sentinel /etc/ssl/cluster/proxy

# Ensure redis user owns data directory
RUN chown -R redis:redis /var/lib/redis /etc/redis
RUN chmod 700 /var/lib/redis/data

# Copy configuration templates and scripts
COPY redis.conf.tpl /app/redis.conf.tpl
COPY sentinel.conf.tpl /app/sentinel.conf.tpl
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY generate-certs.sh /app/generate-certs.sh
COPY VERSION /app/VERSION

# Copy Go binary
COPY --from=gobuild /out/flux-agent /app/flux-agent

# Make scripts executable
RUN chmod +x /app/generate-certs.sh /app/flux-agent

WORKDIR /app

# Expose ports
# 6379 = redis direct, 26379 = sentinel, 6380 = primary-routing proxy
EXPOSE 6379 26379 6380

HEALTHCHECK --interval=30s --timeout=10s --start-period=90s --retries=3 \
    CMD /app/flux-agent health || exit 1

# Run init then start supervisord
CMD ["/bin/bash", "-c", "/app/flux-agent version && /app/flux-agent init && supervisord -n"]
