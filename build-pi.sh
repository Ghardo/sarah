#!/bin/sh
# Builds sarah-pi4 in the builder container (see Dockerfile, "mise run build").
# The web UI must be built first ("mise run ui"), otherwise /ui/ is missing.
set -e
go mod vendor
go build -mod=vendor -a -ldflags="-w -s" -o ./sarah-pi4 ./src
