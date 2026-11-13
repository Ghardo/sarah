#!/bin/sh
# Collects everything needed to report a scanner problem with sarah.
# Read-only: it changes nothing, starts no scan unless -s is given, and does not stop sarah.
#
# Usage (on the computer the scanner is connected to):
#   sh sarah-diagnostics.sh [-u URL] [-d DEVICE] [-b SECONDS] [-s] [-o FILE]
#
#   -u URL      sarah's address (default: http://localhost:$SARAH_PORT, port 7575)
#   -d DEVICE   only this SANE device (default: all devices from /list)
#   -b SECONDS  button test: listen this long for button presses, press every button meanwhile
#   -s          scan test: one preview scan (whole bed, low resolution, no file is saved)
#   -o FILE     write the report to FILE (default: sarah-diagnostics-<date>.txt)
#
# Review the report before you post it: it contains host name, user names, paths
# and the serial number of the scanner if the backend reports one.

set -u

URL="http://localhost:${SARAH_PORT:-7575}"
DEVICE=""
BUTTON_SECONDS=0
SCAN_TEST=0
OUT="sarah-diagnostics-$(date +%Y%m%d-%H%M%S).txt"

while getopts "u:d:b:so:h" opt; do
  case "$opt" in
    u) URL="${OPTARG%/}" ;;
    d) DEVICE="$OPTARG" ;;
    b) BUTTON_SECONDS="$OPTARG" ;;
    s) SCAN_TEST=1 ;;
    o) OUT="$OPTARG" ;;
    *) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  esac
done

have() { command -v "$1" >/dev/null 2>&1; }

if ! have curl; then
  echo "curl is required (sudo apt install curl)" >&2
  exit 1
fi

section() { printf '\n===== %s =====\n' "$1"; }

# run "label" command...: prints the command and its output, never aborts
run() {
  label="$1"; shift
  printf '\n$ %s\n' "$label"
  "$@" 2>&1 || printf '(exit code %s)\n' "$?"
}

api() {
  curl -s -m 60 -w '\n(HTTP %{http_code}, %{time_total}s)\n' "$URL$1" 2>&1 || echo "(not reachable)"
}

urlencode() {
  printf '%s' "$1" | sed -e 's/%/%25/g' -e 's/:/%3A/g' -e 's/ /%20/g' -e 's/\//%2F/g'
}

# Short option table from GET /config (python3 if available, otherwise the raw JSON)
options_table() {
  if have python3; then
    python3 -c '
import json, sys
types = {0: "bool", 1: "int", 2: "fixed", 3: "string", 4: "button"}
data = sys.stdin.read()
try:
    opts = json.loads(data)
except ValueError:
    print(data); sys.exit()
if not isinstance(opts, list):
    print(json.dumps(opts)); sys.exit()
print("%-12s %-22s %-7s %-6s %-8s %s" % ("group", "name", "type", "active", "settable", "values"))
for o in opts:
    c = o.get("ConstrSet") or o.get("ConstrRange") or ""
    if isinstance(c, list) and len(c) > 12:
        c = c[:12] + ["..."]
    print("%-12s %-22s %-7s %-6s %-8s %s" % (
        (o.get("Group") or "-")[:12], o.get("Name", "")[:22], types.get(o.get("Type"), o.get("Type")),
        "yes" if o.get("IsActive") else "no", "yes" if o.get("IsSettable") else "no", c))
'
  else
    cat
  fi
}

devices_from_list() {
  # Device names from the JSON of /list without jq: "Name":"..."
  grep -o '"Name":"[^"]*"' | sed 's/^"Name":"//; s/"$//'
}

# Messages for the person at the terminal (stdout goes into the report)
exec 3>&1

{
  echo "sarah diagnostics, $(date '+%Y-%m-%d %H:%M:%S %Z')"
  echo "URL: $URL"

  section "System"
  run "uname -a" uname -a
  [ -r /etc/os-release ] && run "grep PRETTY_NAME /etc/os-release" grep PRETTY_NAME /etc/os-release
  [ -r /proc/device-tree/model ] && run "cat /proc/device-tree/model" sh -c 'tr -d "\0" < /proc/device-tree/model; echo'
  run "ldd --version (glibc)" sh -c 'ldd --version 2>&1 | head -1'

  section "sarah"
  run "GET /version" api /version
  if have systemctl; then
    run "systemctl show sarah" systemctl show sarah -p ActiveState -p SubState -p ExecStart -p User -p Group -p NRestarts --no-pager
    run "systemctl show sarah -p Environment" systemctl show sarah -p Environment --no-pager
  fi

  section "SANE and USB"
  run "scanimage --version" scanimage --version
  run "scanimage -L (a scanner that sarah holds open may be missing or listed under a shorter name)" scanimage -L
  if have lsusb; then
    run "lsusb" lsusb
    # Permissions of the USB device nodes, except root hubs
    printf '\n$ ls -l /dev/bus/usb/<bus>/<device> (without hubs)\n'
    lsusb 2>/dev/null | grep -vi 'hub' | while read -r _ bus _ dev _; do
      ls -l "/dev/bus/usb/$bus/${dev%:}" 2>&1
    done
  fi
  service_user=$(systemctl show sarah -p User --value 2>/dev/null)
  run "id ${service_user:-$(id -un)} (group scanner or lp needed for USB access)" id "${service_user:-$(id -un)}"

  section "Devices (GET /list)"
  list=$(curl -s -m 60 "$URL/list" 2>/dev/null)
  echo "$list"
  if [ -n "$DEVICE" ]; then
    devs="$DEVICE"
  else
    devs=$(printf '%s' "$list" | devices_from_list)
  fi

  for d in $devs; do
    section "Options of $d (GET /config)"
    curl -s -m 60 "$URL/config?device=$(urlencode "$d")" 2>&1 | options_table
  done

  section "Watchers (GET /watch/list)"
  api /watch/list

  if [ "$SCAN_TEST" = 1 ]; then
    for d in $devs; do
      section "Scan test on $d (POST /preview, no file is saved)"
      curl -s -m 180 -o /dev/null -w 'HTTP %{http_code}, %{size_download} bytes, %{time_total}s\n' \
        -H 'Content-Type: application/json' -d "{\"Device\":\"$d\",\"Options\":{}}" "$URL/preview" 2>&1
    done
  fi

  if [ "$BUTTON_SECONDS" -gt 0 ] 2>/dev/null; then
    section "Button test ($BUTTON_SECONDS s, GET /events)"
    echo ">>> Press every button on the scanner now, one after another, with a short pause in between ($BUTTON_SECONDS s)." >&3
    events=$(curl -s -N -m "$BUTTON_SECONDS" "$URL/events" 2>/dev/null | awk '
      /^event:/ { sub(/^event: ?/, ""); name = $0; next }
      /^data:/  { sub(/^data: ?/, ""); if (name == "button-pressed" || name ~ /^scan-/) print name " " $0 }')
    if [ -n "$events" ]; then echo "$events"; else echo "(no button press received)"; fi
    echo ">>> Button test finished." >&3
  fi

  section "sarah log (last 150 entries without HTTP requests, repeated messages collapsed)"
  if have journalctl; then
    journalctl -u sarah -n 2000 --no-pager 2>&1 | grep -v '\[GIN' | awk '
      { msg = $0; sub(/^.*[0-9][0-9]:[0-9][0-9]:[0-9][0-9] /, "", msg) }
      msg == last { n++; next }
      { if (n > 0) print "    (repeated " n " more times)"; print; last = msg; n = 0 }
      END { if (n > 0) print "    (repeated " n " more times)" }' | tail -150
  else
    echo "(journalctl not available)"
  fi
} > "$OUT" 2>&1

echo "Report written to $OUT"
echo "Please review it (host name, user names, scanner serial) and attach it to the issue."
