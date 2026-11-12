#!/bin/bash

export SARAH_PORT=7575
export SARAH_SCAN_DIR=/home/pi/paperless/consume
export SARAH_USE_TLS=false
export TZ=UTC

./sarah
