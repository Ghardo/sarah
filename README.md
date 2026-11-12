# sarah

**sarah** – **SA**ne **R**est **A**pi (the *h* is just there to make it readable)

REST API for SANE scanners with scanner button watching and a web UI. Meant as a companion to paperless-ngx.

Documentation: see the [wiki](https://github.com/Ghardo/sarah/wiki).

## Build

Requirements: [mise](https://mise.jdx.dev) (Go, Node, pnpm as defined in `mise.toml`) and Docker or Podman. sarah uses cgo (`libsane`), so every target architecture is cross compiled in its own builder container.

| Target | Binary | Runs on |
| --- | --- | --- |
| `arm64` | `dist/sarah-linux-arm64` | Raspberry Pi 3, 4, 5, Zero 2 W with 64-bit OS, any other arm64 Linux |
| `armv7` | `dist/sarah-linux-armv7` | Raspberry Pi 2, 3, 4, 5 with 32-bit OS (armhf) |
| `amd64` | `dist/sarah-linux-amd64` | 64-bit x86 Linux |

```sh
mise run build:arm64   # one target: web UI, builder image, binary
mise run build         # all targets
```

Docker is used by default; for Podman set `CONTAINER=podman` (e.g. `CONTAINER=podman mise run build`, or put it into a `mise.local.toml`).

The builder images are based on Debian Bookworm, so the target needs glibc 2.36 or newer (Raspberry Pi OS / Debian 12 Bookworm, Ubuntu 23.04 and newer) and `libsane` installed. Pi 1 and Zero (ARMv6) are not supported.

Separately:

```sh
mise run ui              # web UI only (web/ -> src/ui/dist)
mise run builder:arm64   # builder image only (sarah-builder-<target>)
mise run vendor          # go mod vendor
docker run --rm -v "$PWD:/app:Z" sarah-builder-arm64 sh ./build.sh   # binary only
```

The binary also runs without a built web UI; `/ui/` is just unavailable then.

On the target, copy the binary as `sarah` (e.g. `/home/pi/sarah/sarah`, see `systemd/sarah.service`).

### Developing the web UI

```sh
cd web
SARAH_API=http://raspberrypi.local:7575 pnpm dev
```

The Vite dev server proxies the API calls to the given sarah instance.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `SARAH_PORT` | `7575` | Port |
| `SARAH_SCAN_DIR` | `scans` | Target directory for scans |
| `SARAH_WATCHER_CONFIG` | `watchers.json` | Saved watchers |
| `SARAH_POST_PROCESS_DIR` | `post-process` | Post-processes including `manifest.json` (see [Post-processes](#post-processes)) |
| `SARAH_ALLOWED_ORIGINS` | `*` | CORS, comma separated |
| `SARAH_USE_TLS`, `SARAH_CERT_FILE`, `SARAH_KEY_FILE` | `false` | TLS |
| `SARAH_UI_ENABLED` | `true` | Serve the web UI at `/ui/` |
| `SARAH_LOCALES_DIR` | `locales` | Additional language files for the web UI |

### Fine tuning

Everything that is not set per scan or watcher in the web UI. Durations as Go duration (`50ms`, `2s`, `1m`) or as a plain number in milliseconds. Invalid values stop the start, the message is in the log.

| Variable | Default | Meaning |
| --- | --- | --- |
| `SARAH_POLL_INTERVAL` | `50ms` | How often the scanner buttons are polled (at least `10ms`). Larger values may miss short presses |
| `SARAH_SCAN_COOLDOWN` | `2s` | Pause of the button polling of a device after a watcher scan was triggered |
| `SARAH_BUTTON_COOLDOWN` | `1s` | Debounce of a button without watcher, so a press is reported only once |
| `SARAH_SENSOR_ERROR_LIMIT` | `5` | Consecutive errors reading the buttons after which the connection is reopened |
| `SARAH_WATCHER_START_DELAY` | `5s` | Delay at startup before the saved watchers are loaded |
| `SARAH_PREVIEW_RESOLUTION` | `75` | Resolution of the preview for the area selection in dpi (the driver picks the closest allowed value) |
| `SARAH_JPEG_QUALITY` | `85` | JPEG quality if a scan or watcher does not set one; also the default in the web UI |
| `SARAH_FILENAME_TEMPLATE` | `scan_{index}_{date}_{time}` | File name template if a scan or watcher does not set one; also the placeholder in the web UI |
| `SARAH_DATE_FORMAT` | `20060102` | Format of `{date}` as [Go layout](https://pkg.go.dev/time#Layout), e.g. `2006-01-02` |
| `SARAH_TIME_FORMAT` | `150405` | Format of `{time}` as Go layout, e.g. `15-04-05` |
| `SARAH_FILE_MODE` | `0644` | Permissions of saved scans (octal), e.g. `0664` so paperless may delete them via the group |
| `SARAH_DIR_MODE` | `0755` | Permissions of the scan directory if sarah creates it |
| `SARAH_EVENTS_PING` | `25s` | Interval of the keep-alive pings in the event stream (`/events`), at least `1s` |

## Post-processes

After every scan a post-process can run – any executable file (shell, Ruby or Python script with a shebang, or a binary). They are managed in the web UI on the "Post-process" tab (name, description, default, file with download/upload). Scans and watchers each pick exactly one; the default is preselected.

Everything lives in `SARAH_POST_PROCESS_DIR`, the metadata in `manifest.json`:

```json
{
  "processes": [
    { "id": "example", "name": "Example", "description": "…", "default": true, "file": "post-process.sh" }
  ]
}
```

The file is started without arguments, the working directory is `SARAH_POST_PROCESS_DIR`. It inherits sarah's environment and additionally gets:

| Variable | Meaning |
| --- | --- |
| `SARAH_SCAN_DIR` | Scan directory (absolute) |
| `SARAH_SCAN_FILENAME` | File name of the scan |
| `SARAH_SCAN_PATH` | Full path of the scan |
| `SARAH_SCAN_FORMAT` | `png` or `jpeg` |
| `SARAH_SCAN_SOURCE` | `api` or `button` |
| `SARAH_SCAN_DEVICE` | SANE device |
| `SARAH_SCAN_SENSOR` | Button of the watcher (empty for `api`) |
| `SARAH_SCAN_DOC`, `SARAH_SCAN_INDEX` | Counters `{doc}` and `{index}` |
| `SARAH_SCAN_NEW_DOC` | `1` if the scan started a new document |
| `SARAH_POST_PROCESS_ID`, `SARAH_POST_PROCESS_NAME`, `SARAH_POST_PROCESS_DIR` | Details of the process |
| `SARAH_VERSION` | sarah version |

Post-processes run one after another, never in parallel. An exit code ≠ 0 counts as failure (log and notification in the web UI). Dependencies have to be bundled or checked by the file itself.

`PATH` is the one of the service (under systemd only `/usr/local/bin:/usr/bin` …), `.bashrc` and profiles are not read. So call interpreters installed with mise, rbenv etc. by absolute path in the shebang:

```ruby
#!/usr/bin/env -S /home/<user>/.local/bin/mise exec ruby@3 -- ruby
```

`SARAH_POST_SCAN_PROCESS` is no longer used: move the script into `SARAH_POST_PROCESS_DIR` and add it to `manifest.json` (or create it in the web UI).

## Web UI

Available at `http://<host>:7575/ui/` (browsers are redirected there from `/`). Disable it with `SARAH_UI_ENABLED=false`.

### Languages

English and German are built in (`src/locales/`). More languages without rebuilding: put a file `<code>.json` (e.g. `fr.json`) into `SARAH_LOCALES_DIR` and reload the page. Use `src/locales/en.json` as a template; missing texts are shown in English. A file named like a built-in language (e.g. `de.json`) replaces it.

```json
{
  "_name": "Français",
  "app": { "tabs": { "scan": "Numériser" } }
}
```

## API

| Method | Path | Description |
| --- | --- | --- |
| GET | `/version` | Version |
| GET | `/defaults` | Defaults for the web UI (`filenameTemplate`, `jpegQuality`) |
| GET | `/list` | Scanners |
| GET | `/config?device=…` | Options of a scanner |
| POST | `/scan` | Start a scan |
| GET | `/scans/:filename` | Fetch a saved scan |
| POST | `/preview` | Preview of the whole bed at 75 dpi as PNG (no file, no post-scan script), used by the area selection in the web UI |
| GET | `/preview/last` | Last scan from memory (even if the post-scan script has already moved the file) |
| POST / DELETE | `/watch` | Create / remove a watcher |
| DELETE | `/watch/all` | Remove all watchers |
| GET | `/watch/list` | List watchers |
| GET | `/post-process` | List post-processes |
| POST | `/post-process` | Create a post-process (`name`, `description`, `default`) |
| PUT / DELETE | `/post-process/:id` | Update / delete a post-process including its file |
| GET / PUT | `/post-process/:id/file` | Download / upload the file (multipart, field `file`, overwrites) |
| GET | `/events` | Server-Sent Events: `scan-started`, `scan-completed`, `scan-failed`, `button-pressed`, `watchers-changed`, `post-processes-changed`, `post-process-finished` |
| GET | `/i18n`, `/i18n/:code` | Languages of the web UI (only with the UI enabled) |

Examples in `rest.http`.

### File format

`POST /scan` and `POST /watch` additionally accept `Format`, `Quality` and `PostProcess` (ID, `none`, or omitted for the default). In the web UI this can be set under "File" when scanning and in the watcher editor.

| Field | Values | Default |
| --- | --- | --- |
| `Format` | `png` (lossless) or `jpeg` | `png` |
| `Quality` | 1–100, only with `jpeg` | `85` |

The extension (`.png` or `.jpg`) is appended to the file name template to match the format. Grayscale and lineart scans are saved as single-channel JPEG. The resolution of the scan is written into the image (PNG `pHYs`, JPEG JFIF), so converters such as ImageMagick get the right page size. The preview (`POST /preview`) is always PNG.

## License

MIT, see [LICENSE](LICENSE).
