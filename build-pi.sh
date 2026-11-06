#!/bin/sh
# Baut sarah-pi4 im Builder-Container (siehe Dockerfile, "mise run build").
# Das Web-UI muss vorher gebaut sein ("mise run ui"), sonst fehlt /ui/.
set -e
go mod vendor
go build -mod=vendor -a -ldflags="-w -s" -o ./sarah-pi4 ./src
