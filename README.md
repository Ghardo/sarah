# sarah

**sarah** – **SA**ne **R**est **A**pi (the *h* is just there to make it readable)

REST API for SANE scanners with scanner button watching and a web UI. Meant as a companion to paperless-ngx.

Documentation: see the [wiki](https://github.com/Ghardo/sarah/wiki).

## Build

Requirements: [mise](https://mise.jdx.dev) (Go, Node, pnpm as defined in `mise.toml`) and Podman.

```sh
mise run builder   # once: create the builder image for arm64
mise run build     # build the web UI and create sarah-pi4
```

Separately:

```sh
mise run ui        # web UI only (web/ -> src/ui/dist)
podman run --rm -v ./:/app:Z sarah-builder sh ./build-pi.sh   # binary only
```

The binary also runs without a built web UI; `/ui/` is just unavailable then.

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
| `SARAH_POST_SCAN_PROCESS` | – | Script run after every scan (`SARAH_SCAN_DIR`, `SARAH_SCAN_FILENAME` in the environment) |
| `SARAH_ALLOWED_ORIGINS` | `*` | CORS, comma separated |
| `SARAH_USE_TLS`, `SARAH_CERT_FILE`, `SARAH_KEY_FILE` | `false` | TLS |
| `SARAH_UI_ENABLED` | `true` | Serve the web UI at `/ui/` |
| `SARAH_LOCALES_DIR` | `locales` | Additional language files for the web UI |

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
| GET | `/list` | Scanners |
| GET | `/config?device=…` | Options of a scanner |
| POST | `/scan` | Start a scan |
| GET | `/scans/:filename` | Fetch a saved scan |
| POST | `/preview` | Preview of the whole bed at 75 dpi as PNG (no file, no post-scan script), used by the area selection in the web UI |
| GET | `/preview/last` | Last scan from memory (even if the post-scan script has already moved the file) |
| POST / DELETE | `/watch` | Create / remove a watcher |
| DELETE | `/watch/all` | Remove all watchers |
| GET | `/watch/list` | List watchers |
| GET | `/events` | Server-Sent Events: `scan-started`, `scan-completed`, `scan-failed`, `button-pressed`, `watchers-changed` |
| GET | `/i18n`, `/i18n/:code` | Languages of the web UI (only with the UI enabled) |

Examples in `rest.http`.

### File format

`POST /scan` and `POST /watch` additionally accept `Format` and `Quality`. In the web UI this can be set under "File" when scanning and in the watcher editor.

| Field | Values | Default |
| --- | --- | --- |
| `Format` | `png` (lossless) or `jpeg` | `png` |
| `Quality` | 1–100, only with `jpeg` | `85` |

The extension (`.png` or `.jpg`) is appended to the file name template to match the format. Grayscale and lineart scans are saved as single-channel JPEG. The resolution of the scan is written into the image (PNG `pHYs`, JPEG JFIF), so converters such as ImageMagick get the right page size. The preview (`POST /preview`) is always PNG.

## License

MIT, see [LICENSE](LICENSE).
