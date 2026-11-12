#!/bin/sh
# Builds dist/sarah-linux-<target> in the builder container (see Dockerfile, "mise run build").
# The target comes from the image (TARGET) or the first argument: arm64, armv7, amd64.
# Run "mise run vendor" first. The web UI must be built first ("mise run ui"),
# otherwise /ui/ is missing.
set -e
TARGET="${1:-$TARGET}"
case "$TARGET" in
  arm64) export GOARCH=arm64 CC=aarch64-linux-gnu-gcc ;;
  armv7) export GOARCH=arm GOARM=7 CC=arm-linux-gnueabihf-gcc ;;
  amd64) export GOARCH=amd64 CC=gcc ;;
  *) echo "unknown target '$TARGET' (arm64, armv7, amd64)" >&2; exit 1 ;;
esac
mkdir -p dist
go build -mod=vendor -a -ldflags="-w -s" -o "./dist/sarah-linux-$TARGET" ./src
echo "built dist/sarah-linux-$TARGET"
