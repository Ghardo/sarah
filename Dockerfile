# Builder image for cross compiling sarah (cgo against libsane).
# One image per target architecture: TARGET = arm64 | armv7 | amd64
#   docker build --build-arg TARGET=arm64 -t sarah-builder-arm64 .
#
# Bookworm (Debian 12): working repositories, and the glibc of the binary
# (2.36) matches Raspberry Pi OS Bookworm. Targets need glibc >= 2.36.
FROM debian:bookworm

ARG TARGET=arm64

# Cross toolchain and libsane-dev for the target architecture
RUN set -e; \
    case "$TARGET" in \
      arm64) DEB_ARCH=arm64; CC_PKGS="gcc-aarch64-linux-gnu libc6-dev-arm64-cross" ;; \
      armv7) DEB_ARCH=armhf; CC_PKGS="gcc-arm-linux-gnueabihf libc6-dev-armhf-cross" ;; \
      amd64) DEB_ARCH=amd64; CC_PKGS="gcc libc6-dev" ;; \
      *) echo "unknown TARGET '$TARGET' (arm64, armv7, amd64)" >&2; exit 1 ;; \
    esac; \
    dpkg --add-architecture "$DEB_ARCH"; \
    apt-get update; \
    apt-get install -y curl pkg-config build-essential $CC_PKGS "libsane-dev:$DEB_ARCH"; \
    rm -rf /var/lib/apt/lists/*

# Install Go manually
ARG GO_VERSION=1.27.3
RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

# GOARCH, GOARM and CC are set per target in build.sh
ENV PATH="/usr/local/go/bin:${PATH}" \
    TARGET=${TARGET} \
    GOOS=linux \
    CGO_ENABLED=1 \
    GOCACHE=/app/.cache/go-build

WORKDIR /app
