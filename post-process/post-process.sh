#!/bin/bash
#
# Example post-process for sarah (see "Post-processes" in the README).
#
# sarah runs this file after every saved scan, without arguments, with
# SARAH_POST_PROCESS_DIR as working directory. Among others it passes:
#   SARAH_SCAN_DIR       directory of the scan
#   SARAH_SCAN_FILENAME  file name of the scan (PNG or JPEG)
#   SARAH_SCAN_PATH      full path of the scan
#   SARAH_SCAN_DOC       document counter {doc}, SARAH_SCAN_INDEX page counter {index}
# The web UI lists all variables on the "Post-process" tab.
#
# By default nothing happens here. What should happen after a scan depends on
# your own setup. An example (PNG → PDF into the consume folder of
# paperless-ngx) is on the wiki page "Paperless-Integration".

exit 0
