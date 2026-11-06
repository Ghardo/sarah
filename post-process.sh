#!/bin/bash
#
# Post-Scan-Skript für sarah (SARAH_POST_SCAN_PROCESS).
#
# sarah ruft dieses Skript nach jedem gespeicherten Scan auf und übergibt:
#   SARAH_SCAN_DIR       Verzeichnis des Scans
#   SARAH_SCAN_FILENAME  Dateiname der PNG
#
# Standardmäßig passiert hier nichts. Was nach dem Scan geschehen soll, hängt
# von der eigenen Umgebung ab. Ein Beispiel (PNG → PDF in den Consume-Ordner
# von paperless-ngx) steht im Wiki auf der Seite „Paperless-Integration“.

exit 0
