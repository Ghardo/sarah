# sarah

REST-API für SANE-Scanner mit Überwachung der Scanner-Tasten und Web-UI. Gedacht als Ergänzung zu paperless-ngx.

## Bauen

Voraussetzungen: [mise](https://mise.jdx.dev) (Go, Node, pnpm laut `mise.toml`) und Podman.

```sh
mise run builder   # einmalig: Builder-Image für arm64 erstellen
mise run build     # Web-UI bauen und sarah-pi4 erzeugen
```

Einzeln:

```sh
mise run ui        # nur Web-UI (web/ -> src/ui/dist)
podman run --rm -v ./:/app:Z sarah-builder sh ./build-pi.sh   # nur Binary
```

Ohne gebautes Web-UI läuft das Binary auch, `/ui/` ist dann nur nicht verfügbar.

### Web-UI entwickeln

```sh
cd web
SARAH_API=http://raspberrypi.local:7575 pnpm dev
```

Der Vite-Dev-Server leitet die API-Aufrufe an die angegebene sarah-Instanz weiter.

## Konfiguration

| Variable | Standard | Bedeutung |
| --- | --- | --- |
| `SARAH_PORT` | `7575` | Port |
| `SARAH_SCAN_DIR` | `scans` | Zielverzeichnis der Scans |
| `SARAH_WATCHER_CONFIG` | `watchers.json` | Gespeicherte Watcher |
| `SARAH_POST_SCAN_PROCESS` | – | Skript nach jedem Scan (`SARAH_SCAN_DIR`, `SARAH_SCAN_FILENAME` als Umgebung) |
| `SARAH_ALLOWED_ORIGINS` | `*` | CORS, kommagetrennt |
| `SARAH_USE_TLS`, `SARAH_CERT_FILE`, `SARAH_KEY_FILE` | `false` | TLS |
| `SARAH_UI_ENABLED` | `true` | Web-UI unter `/ui/` ausliefern |
| `SARAH_LOCALES_DIR` | `locales` | Zusätzliche Sprachdateien fürs Web-UI |

## Web-UI

Erreichbar unter `http://<host>:7575/ui/` (Browser werden von `/` dorthin umgeleitet). Abschalten mit `SARAH_UI_ENABLED=false`.

### Sprachen

Eingebaut sind Englisch und Deutsch (`src/locales/`). Weitere Sprachen ohne Neubau: eine Datei `<code>.json` (z. B. `fr.json`) in `SARAH_LOCALES_DIR` ablegen und die Seite neu laden. Vorlage ist `src/locales/en.json`, fehlende Texte werden auf Englisch angezeigt. Eine Datei mit dem Namen einer eingebauten Sprache (z. B. `de.json`) ersetzt diese.

```json
{
  "_name": "Français",
  "app": { "tabs": { "scan": "Numériser" } }
}
```

## API

| Methode | Pfad | Beschreibung |
| --- | --- | --- |
| GET | `/version` | Version |
| GET | `/list` | Scanner |
| GET | `/config?device=…` | Optionen eines Scanners |
| POST | `/scan` | Scan starten |
| GET | `/scans/:filename` | Gespeicherten Scan abrufen |
| GET | `/preview/last` | Letzten Scan aus dem Speicher (auch wenn das Post-Scan-Skript die Datei schon verschoben hat) |
| POST / DELETE | `/watch` | Watcher anlegen / entfernen |
| DELETE | `/watch/all` | Alle Watcher entfernen |
| GET | `/watch/list` | Watcher auflisten |
| GET | `/events` | Server-Sent Events: `scan-started`, `scan-completed`, `scan-failed`, `button-pressed`, `watchers-changed` |
| GET | `/i18n`, `/i18n/:code` | Sprachen des Web-UI (nur bei aktivem UI) |

Beispiele in `rest.http`.
