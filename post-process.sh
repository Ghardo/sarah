#!/bin/bash
#
# Post-scan script for sarah (SARAH_POST_SCAN_PROCESS).
#
# sarah calls this script after every saved scan and passes:
#   SARAH_SCAN_DIR       directory of the scan
#   SARAH_SCAN_FILENAME  file name of the PNG
#
# By default nothing happens here. What should happen after a scan depends on
# your own setup. An example (PNG → PDF into the consume folder of
# paperless-ngx) is on the wiki page "Paperless-Integration".

exit 0
