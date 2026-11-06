# Bookworm (Debian 12) for working repositories
FROM debian:bookworm

# Set up arm64 repositories and toolchain
RUN dpkg --add-architecture arm64 && \
    apt-get update && \
    apt-get install -y \
    curl \
    gcc-aarch64-linux-gnu \
    libc6-dev-arm64-cross \
    libsane-dev:arm64 \
    pkg-config \
    build-essential && \
    rm -rf /var/lib/apt/lists/*

# Install Go manually
ARG GO_VERSION=1.27.3
RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}" \
    GOOS=linux \
    GOARCH=arm64 \
    CGO_ENABLED=1 \
    CC=aarch64-linux-gnu-gcc \
    PKG_CONFIG_PATH="/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig" \
    LD_LIBRARY_PATH="/usr/aarch64-linux-gnu/lib:${LD_LIBRARY_PATH}" \
    GOCACHE=/app/.cache/go-build

WORKDIR /app