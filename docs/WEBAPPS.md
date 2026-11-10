# Web-Apps direkt (PLAN 66)

Seit 0.63.0 laufen Programme, die es als reine Browser-Seite gibt (die Craft-Programme von storytold: WordCraft, GridCraft, PdfCraft u. a.), **direkt im Browser** im TitanOS-Fenster: kein Stream, keine Last auf dem NAS, auch über Cloudflare und am Handy. TitanOS liefert ihre Dateien selbst aus und reicht Dateien hinein und wieder heraus („Öffnen mit“, Standard-Apps, Speichern ins Original). Entscheidungen des Projektinhabers vom 7.11.2026 (PLAN 66, 66-1, 66-2), dazu „Docker-Versionen ersetzen“ (7.11.2026).

## Katalog und Herkunft

- `webapps/catalog.json` im Repository (Kopie in `backend/internal/webapps/catalog.json`, per `go:embed` in titand; ein Test hält beide gleich): je App `id` (z. B. `wordcraft`; Kleinbuchstaben, beginnt nie mit `_`), `name`, `repo` (`storytold/wordcraft`), `version`, `upstreamAsset` (`wordcraft-web-0.16.0.zip`), `sha256` und `size` des Upstream-Archivs, `license`, `bridge` (`host` = Craft-Host-Brücke `craft-web-host`, `adapter` = ohne Brücke), `types` (Dateiendungen ohne Punkt, klein) und `default` (gehört zur Grundausstattung). Grundausstattung: WordCraft (`docx doc odt rtf`), GridCraft (`xlsx xls ods csv`), PdfCraft (`pdf`). Optional `dirtyTitle` (nur `adapter`, seit 0.76): die App zeigt ungespeicherte Änderungen von Anfang an mit einem `*` im Seitentitel; der Vermittler (`host.js`, `data-dirty="title"`) glaubt dann sofort dem Titel statt aus Tasten und Maus zu schließen.
- Stand 0.63.0 (jeweils neuestes Release, SHA-256 mit den `SHA256SUMS.txt` von storytold abgeglichen):

| id | Version | Brücke | Typen | Canvas im `index.html` |
|---|---|---|---|---|
| wordcraft | 0.16.0 | host | docx doc odt rtf | `wordcraft_canvas` |
| gridcraft | 0.17.0 | host | xlsx xls ods csv | `gridcraft_canvas` |
| pdfcraft | 0.19.0 | host | pdf | **`pdfcraft`** |
| deckcraft | 0.16.0 | host | pptx potx ppsx | `deckcraft_canvas` |
| cadcraft | 0.18.0 | host | dxf | `cadcraft_canvas` |
| figcraft | 0.16.0 | host | figcraft | `figcraft_canvas` |
| filmcraft | 0.18.0 | host | fcproj | `filmcraft_canvas` |
| lightcraft | 0.19.0 | host | – | `lightcraft_canvas` |
| vectorcraft | 0.22.0 | adapter | svg ai | `vectorcraft_canvas` |
| designcraft | 0.18.0 | adapter | – | `designcraft_canvas` |
| effectcraft | 0.22.0 | adapter | ecproj | `effectcraft_canvas` |
| soundcraft | 0.18.0 | adapter | – | `soundcraft_canvas` |
| texturecraft | 0.4.0 | adapter | – | `texturecraft_canvas` |
| materialcraft | 0.2.0 | adapter | – | `materialcraft_canvas` |

- Die Release-CI (`scripts/fetch-webapps.py`, Schritt „Web apps“ in `release.yml`) lädt jedes Archiv von `https://github.com/<repo>/releases/download/v<version>/<upstreamAsset>` (ohne GitHub-API), prüft Größe und SHA-256 aus dem Katalog und den Inhalt (Regeln wie in titand, siehe unten) und legt es **unverändert** als `titanos-webapp-<id>-<version>.zip` ins Release – der SHA-256 bleibt der des Katalogs. Das signierte `manifest.json` bekommt `"webapps": [{"id","version","asset","sha256","size"}]` (optional, ältere titand ignorieren es); `verify-release.py` verlangt genau die Apps des Katalogs mit dessen Version, SHA-256 und Größe. Lizenz-, NOTICE-, ATTRIBUTION-/ASSETS- und Schrift-Lizenzdateien (OFL-1.1) jeder App kommen ins Lizenzarchiv des Releases unter `webapps/<id>-<version>/` (fehlen `LICENSE-MIT`/`LICENSE-APACHE` im Archiv, aus dem Repository am Tag), `sources.txt` verlinkt den Quellstand am Tag. Mit `WEBAPPS_DIR` nimmt `make-release.sh` die geprüften Archive aus einem Ordner, mit `WEBAPPS_SOURCE` holt es sie aus einem lokalen Ordner (Tests).
- titand lädt eine App nur aus dem Manifest **seines eigenen Releases** (Signatur wie beim Abbild der Fernansicht, `plugins.Manager.ReleaseManifest`/`DownloadReleaseAsset`), prüft Größe und SHA-256, verlangt dieselbe Version wie sein Katalog (`release_mismatch` sonst) und entpackt sicher:
  - jeder Eintrag liegt unter genau `<id>-web-<version>/` (dieser Ordner entfällt), nur reguläre Dateien und Ordner (keine Links, Geräte), keine absoluten Namen, keine Teile `.`/`..`/leer, kein `\` oder NUL, keine doppelten Namen (ohne Groß-/Kleinschreibung), höchstens **2000 Dateien** und **256 MiB entpackt** (gezählt beim Lesen, nicht nach den Angaben im Archiv), `index.html` oben. Die ursprüngliche Grenze von 64 MiB war zu klein: EffectCraft 74 MB, VectorCraft 93 MB, LightCraft 100 MB (mit vorkomprimierten Kopien).
  - Ablage: `<volume>/.titanos/webapps/<id>/<version>/` (über `volumedata.Open`: Ordner root 0700, Dateien 0644; titand liefert sie selbst aus), erst in `.staging-…`, dann umbenannt; danach werden andere Versionen dieser App und das heruntergeladene Archiv (`downloads/`) entfernt. Zu jeder komprimierbaren Datei ab 1 KiB (`.js .mjs .wasm .css .json .svg .txt .md .webmanifest .ttf .otf .map`) legt titand beim Installieren eine `.gz`-Kopie an (außer `index.html`), damit Ausliefern keine CPU kostet.
  - Volume: das der bereits installierten Apps, sonst Dockers Volume, sonst das erste eingebundene; **nie der Stick**, nie RAM/Overlay (vor dem Laden mindestens 768 MiB frei auf dem Volume).
- `/config/webapps.json` (optional, neu): `{"installed":[{"id","version","volume","icon"}],"pending":["wordcraft",…],"offered":true}`. `icon` ist die Symboldatei relativ zum App-Ordner (aus `<link rel="icon">` in `index.html`, sonst `favicon.svg`/`favicon.png`/`icon-256.png`), `pending` die Apps, die auf das erste Volume warten, `offered`, dass der Update-Hinweis gesendet oder beantwortet wurde. titand liest die Datei nur beim Start und schreibt sie nur bei Änderungen (Installieren, Entfernen, Wahl).
- **Entwicklungsmodus** (`titand -dev -webapps-dir <ordner>`): Archive aus dem Ordner (`titanos-webapp-<id>-<version>.zip` oder der Upstream-Name), geprüft gegen SHA-256 und Größe des Katalogs; Ablage in einem Temp-Ordner (`titand-webapps-*`, beim Beenden gelöscht), nie auf einem Volume; installierte Apps gelten nach einem Neustart als nicht installiert.

## API

Alle Antworten von `GET /api/v1/webapps` und den Aktionen haben dieselbe Form:

```json
{
  "apps": [{
    "id": "wordcraft", "name": "WordCraft", "version": "0.16.0",
    "installed": true, "state": "ready", "progress": 1,
    "bridge": "host", "types": ["docx","doc","odt","rtf"], "default": true,
    "pending": false, "url": "/webapps/wordcraft/0.16.0/",
    "icon": "/webapps/wordcraft/0.16.0/wordcraft-fa59e09a3f921e03.ico",
    "size": 22269466, "license": "MIT OR Apache-2.0", "repo": "storytold/wordcraft"
  }],
  "volume": "data",
  "docker": [{"app": "wordcraft", "webapp": "wordcraft", "running": true, "image": "ghcr.io/storytold/wordcraft:0.15.0"}],
  "migration": {"running": false, "started": "…", "finished": "…", "install": ["wordcraft"],
                "items": [{"app": "wordcraft", "webapp": "wordcraft", "state": "removed"}]}
}
```

- `state`: `missing` | `downloading` (Laden und Entpacken; `progress` 0–1, 1 = wird entpackt) | `ready` | `error`. `error` ist ein Code: `no_volume`, `volume_unavailable` (installiert, aber das Volume ist nicht eingebunden), `not_in_release` (das Release dieser titand bringt die App nicht, z. B. Entwicklungsstand), `download_failed`, `invalid_archive`, `install_failed`, `release_mismatch`. Schlägt ein Update fehl, bleibt die alte Version `ready` und `error` nennt den Code. `version` ist bei installierten Apps die installierte, sonst die des Katalogs. `url`/`icon` nur bei `ready`; `icon` fehlt, wenn die App keins hat (Oberfläche zeichnet einen Buchstaben). `pending`: wartet auf ein Volume. `GET` liest keine Platte.
- `docker` und `migration` nur für Besitzer/Administratoren (Benutzer: `[]` und `null`).
- `GET /api/v1/webapps` (jedes Konto).
- `POST /api/v1/webapps/{id}/install` (Administratoren): startet im Hintergrund, Antwort 202. Fehler: `404 webapp_unknown`.
- `DELETE /api/v1/webapps/{id}` (Administratoren): entfernt Eintrag und Dateien. `404 webapp_not_installed`, `409 webapp_busy` (wird gerade installiert).
- `POST /api/v1/webapps/defaults/install` (Administratoren), Rumpf optional `{"apps":["wordcraft",…]}`: ohne Rumpf die Grundausstattung; ohne eingebundenes Volume merkt sich titand die Wahl als `pending` und installiert, sobald eines eingebunden ist (Prüfung alle 30 s im RAM). `{"apps":[]}` vermerkt nur die Antwort (`offered`, kein Hinweis mehr). Ersteinrichtung und der Knopf im Hinweis rufen diese Route.
- `POST /api/v1/webapps/migrate` (Administratoren, wie „App entfernen“ gesperrt während einer Anwendungssicherung) `{"install":["wordcraft"],"removeDocker":["wordcraft"]}`: installiert die Web-Apps und entfernt danach die genannten Docker-Apps. Jede Docker-App muss in `docker` stehen (`400 webapp_not_craft_docker` sonst) und ihre Web-App in `install` oder schon installiert sein (`400 webapp_not_installed`); `400 webapp_unknown` bei unbekannter id, `409 webapp_busy` während eine andere Umstellung läuft. Eine Docker-App wird **erst entfernt, wenn ihre Web-App fertig installiert ist**; scheitert die Installation, bleibt sie (`kept`). Fortschritt in `migration.items[].state`: `waiting`, `removing`, `removed`, `kept`, `failed` (mit `error`). Nur im RAM; ein Neustart von titand bricht eine laufende Umstellung ab (Docker-Apps bleiben dann stehen).
  - Entfernen ist das normale „App entfernen“ **ohne Daten löschen**: Fernansichten der App beenden, `docker compose down --remove-orphans` (Container und Netzwerke weg; benannte Volumes, gebundene Ordner, `.titanos`-/App-Datenordner auf dem Volume und Abbilder bleiben), Compose-Datei aus `/config/apps/<name>` löschen, HTTPS-Name der App vergessen. Daten muss man danach bei Bedarf selbst unter Docker löschen.
- Erkennung der Docker-Versionen (`webapps.DetectDocker`): nur von TitanOS verwaltete Apps (Compose-Datei in `/config`), nur wenn Docker antwortet. Eine App zählt, wenn **jeder** ihrer Container dieselbe Craft-App ist – Abbild `storytold/<id>` (Docker Hub, `ghcr.io/storytold/…`) bzw. ein Abbild, dessen letzter Teil `<id>`, `<id>-web`, `<id>-app` oder `<id>_web` heißt, oder ein Containername dieser Form (nicht mit Abbildern wie postgres, mysql, mariadb, redis, mongo, valkey oder `…server…`) – oder sie keine Container hat und so heißt. `-server` zählt nie (FigCraft-Server ist nicht die App). Apps mit weiteren Diensten (Datenbank, Proxy) werden nie angeboten.
- `GET /webapps/{id}/{version}/{path...}`: die Dateien der App, **ohne Anmeldung** (öffentlicher Open-Source-Code; der abgeschottete Rahmen sendet keine Cookies), nur für installierte Apps in genau dieser Version und nur bei eingebundenem Volume, sonst 404. Keine Ordnerlisten, keine Dateien oder Ordner, die mit `.` beginnen (`.htaccess`), keine Links; `index.html.gz`/`.br` direkt nie.
  - MIME fest nach Endung (`.wasm` = `application/wasm`, `.js`/`.mjs` = `text/javascript; charset=utf-8`, Schriften `font/*`, `.webmanifest`, Bilder; Unbekanntes `application/octet-stream`).
  - `Cache-Control: public, max-age=31536000, immutable` für **alle Dateien außer `index.html`** – die Version steht in der Adresse, keine Datei darunter ändert sich (LightCraft hat Dateinamen ohne Hash; die Regel „Dateien mit Hash“ wäre falsch). `index.html`: `no-cache`.
  - Komprimiert: liegt `<datei>.br` bzw. `<datei>.gz` daneben und erlaubt der Browser es (`Accept-Encoding`, `q=0` gilt als nein), kommt diese mit `Content-Encoding` und `Vary: Accept-Encoding` (dann ohne `Content-Length`); nie für `index.html`. Nichts wird beim Ausliefern komprimiert.
  - Kopfzeilen jeder Antwort: `Content-Security-Policy: sandbox allow-scripts allow-downloads allow-modals allow-popups; frame-ancestors 'self'`, `X-Content-Type-Options: nosniff`, `Cross-Origin-Resource-Policy: cross-origin`, `Access-Control-Allow-Origin: *` (der abgeschottete Rahmen hat den Ursprung `null`; ES-Module und `.wasm` brauchen dann CORS). Die CSP-Sandbox gilt auch, wenn eine Datei (z. B. ein SVG) außerhalb des Rahmens geöffnet wird: kein Zugriff auf den TitanOS-Ursprung. Die allgemeinen Kopfzeilen von titand (`X-Frame-Options: SAMEORIGIN`, `Referrer-Policy`) kommen dazu; die CSP der Oberfläche nicht.
  - `index.html` liefert titand ergänzt aus: bei `bridge: host` `data-host="page"` am **ersten `<canvas>`** (falls es noch keins hat; PdfCraft heißt `id="pdfcraft"`), und bei allen Apps `<script src="/webapps/_host/host.js" data-app="<id>" data-bridge="host|adapter"></script>` **im `<head>`** direkt vor dem ersten `<script>` (bei Trunk-Builds das Modul-Skript im Kopf) bzw. vor `</head>`, wenn das erste Skript erst im `<body>` steht (LightCraft, EffectCraft, FilmCraft, SoundCraft). Sonst bleibt die Datei unverändert (auch die SRI-`integrity`-Angaben).
- `GET /webapps/_host/{file}`: der TitanOS-Vermittler im abgeschotteten Dokument (`web/static/webapps/_host/host.js`, Teil der Weboberfläche, ohne Anmeldung). Eigener Handler: nur `.js`-Dateien, die in der eingebetteten Oberfläche unter `webapps/_host/` liegen, als `text/javascript; charset=utf-8`, `no-cache`, mit `Access-Control-Allow-Origin: *` und `nosniff`; sonst 404 (nie die `index.html` der Oberfläche, die der Browser wegen `nosniff` still verwerfen würde). `_host` ist nie eine App-id; die Route ist spezifischer als die der Apps.
- Dateien: Lesen über `GET /api/v1/files/download?root=<id>&path=…` (wie bisher), Schreiben über **neu** `PUT /api/v1/files/content?root=<id>&path=<pfad>&expect=<mtime-ns|modified|none>`:
  - Rumpf = Datei (geht direkt auf das Volume, nichts im RAM), `Content-Length` nötig (`411 length_required`), höchstens 2 GiB (`413 too_large`). Rechte des Kontos wie beim Hochladen (beschreibbare Wurzel; Stick und nur lesbare Freigaben `403 read_only_share`), Pfadprüfung wie beim Schreiben über MCP (keine geheimen/geschützten Pfade, Links werden am Ziel gespeichert, wenn es in der Wurzel liegt), freier Platz geprüft (`507 no_space`; Kontingente der Freigaben sind Projekt-Quotas des Kernels, die Teil-Datei erbt sie, ein Überschreiten endet ebenfalls in `507 no_space`).
  - `expect`: Nanosekunden seit 1970 als Ziffern (aus `mtime` einer früheren Antwort), **oder** die RFC-3339-Zeit `modified` aus der Dateiliste (die ist auf Sekunden gekürzt; dann wird auf Sekunden verglichen), **oder** `none` (neue Datei, darf nicht existieren: `409 file_exists`). Bei Links gilt auch die Zeit des Links selbst (die Liste zeigt sie). Passt die Zeit nicht oder ist die Datei inzwischen weg: `409 file_changed`. Ungültiges `expect`: `400 bad_request`.
  - Schreiben: in eine versteckte Teil-Datei im selben Ordner, Besitzer und Rechte der alten Datei übernommen (neue Datei: Konto und Gruppe der Freigabe, 0660), `fsync`, unmittelbar davor noch einmal geprüft (`409 file_changed`), dann atomar umbenannt (neue Datei mit `RENAME_NOREPLACE`). **Der alte Inhalt bleibt als `<name>.prev`** wie beim Texteditor und beim Schreiben über MCP (harter Link, auf FAT/exFAT umbenannt). Kein Papierkorb, kein Rückgängig-Eintrag.
  - Antwort `200 {"modified":"<RFC3339Nano, UTC>","mtime":"<ns als Text>","size":n}` (`mtime` als Text, weil Nanosekunden keine JSON-Zahl sind). Im Aktivitätsprotokoll als `PUT /api/v1/files/content` mit Wurzel/Pfad.
  - Grenze: Dateizeiten kommen von einer groben Kernel-Uhr (einige Millisekunden). Zwei Speicherungen in derselben Millisekunde sind nicht unterscheidbar; mit dem Sekundenwert der Liste gilt das für dieselbe Sekunde.
- Keine dieser Routen ist in `publicRoutes` oder im Werkzeugkatalog (kein Token, kein MCP, kein Agent).

## Ablauf im Fenster

```
TitanOS-Fenster (WebAppView, Ursprung TitanOS)
   └─ <iframe sandbox="allow-scripts allow-downloads allow-modals allow-popups"   (eigener, undurchsichtiger Ursprung:
        src="/webapps/<id>/<version>/?author=<Name>&bridge=host|adapter&app=<id>">  keine Cookies, keine TitanOS-API)
         host.js  ⇄  App (gleiches Dokument, CustomEvents der Craft-Brücke oder Adapter)
```

**Nie `host=parent`** an die Adresse hängen: es hat Vorrang vor `data-host` (und der Rahmen hat keinen gleichen Ursprung).

Nachrichten zwischen Fenster und `host.js` per `postMessage` (Fenster prüft `event.source === iframe.contentWindow`, sendet mit `targetOrigin "*"`, weil der Rahmen keinen Ursprung hat; Inhalte sind nur Dateiname und Bytes):

| Richtung | Nachricht |
|---|---|
| host → Fenster | `{"type":"webapp-ready"}` sobald die App `<id>:ready` meldet (Adapter: wenn der Canvas da ist) |
| Fenster → host | `{"type":"webapp-open","name","bytes":ArrayBuffer}` (übertragen, nicht kopiert) |
| host → Fenster | `{"type":"webapp-save","name","saveAs":bool,"bytes":ArrayBuffer}` |
| host → Fenster | `{"type":"webapp-dirty","dirty":bool}`, `{"type":"webapp-failed","text"}` |
| Fenster → host | `{"type":"webapp-saved","ok":bool,"text"}` (nur zur Anzeige in der App, falls unterstützt) |

- **Brücke** (`bridge: host`): `host.js` spricht die Craft-Host-Ereignisse `<id>:ready/open/save/dirty/failed` am Canvas mit `data-host="page"` (Präfix ist immer die Katalog-id, auch `pdfcraft:ready`). Die Ereignisse steigen nicht auf (`bubbles: false`), laufen aber durch die Capture-Phase: `window.addEventListener("<id>:ready", h, true)` fängt sie, auch wenn `host.js` im `<head>` vor dem Canvas läuft (geprüft mit WordCraft 0.16.0 in Chromium, abgeschotteter Ursprung: `detail` = `{"version":"1","appVersion":"0.16.0","file":false}`). Öffnen: `canvas.dispatchEvent(new CustomEvent("<id>:open",{detail:{name,bytes}}))` auf dem Canvas (`event.target` von `ready`). `save`: `detail` = `{name, saveAs, bytes}`; `dirty`: `{dirty}`; `failed`: `{error}`. Laut HOSTING.md der Apps höchstens 512 MiB je geöffneter Datei; FilmCraft importiert Nicht-Projektdateien wie abgelegte Dateien.
- **Adapter** (`bridge: adapter`): Öffnen = synthetisches `drop` mit `DataTransfer`/`File` auf den Canvas; Speichern = Abfangen von Downloads (`HTMLAnchorElement.prototype.click` bei `download`-Attribut und `blob:`-Adressen, `URL.createObjectURL`), `saveAs` immer `true` (der Nutzer wählt Ziel).
- **Speichern:** `saveAs:false` → `PUT /files/content` aufs Original mit `expect` = `mtime` aus der letzten Antwort bzw. `modified` beim Öffnen; `409 file_changed` → Dialog „Datei wurde inzwischen geändert: Überschreiben / Als Kopie speichern / Abbrechen“ (Überschreiben: mit der aktuellen Zeit der Datei erneut). `saveAs:true` → Speicherdialog von TitanOS (Ordner und Name, vorausgefüllt; `expect=none` bzw. bei vorhandener Datei nach Rückfrage deren Zeit), danach ist das die Datei des Fensters. Exporte (PDF usw.) ebenfalls über den Speicherdialog.
- **Ungespeicherte Änderungen:** Schließen des Fensters mit `dirty` fragt nach.

### Grenzen im abgeschotteten Rahmen (geprüft 7.11.2026 mit Chromium headless (Playwright 1194), abgeschotteter Ursprung über die CSP-Sandbox)

- WordCraft, GridCraft, PdfCraft, FilmCraft und LightCraft laden Modul und WebAssembly über CORS und starten (WebGL2; WebGPU nur über HTTPS bzw. wo der Browser es hat).
- **Kein Browser-Speicher** (localStorage, IndexedDB, OPFS, Web Locks): Einstellungen der Apps bleiben nicht erhalten, eigene Schriften nicht gespeichert; LightCraft arbeitet ohne Bibliothek auf Dauer.
- **Keine Web Worker** aus Dateien der App (LightCraft: Render-Worker starten nicht, die App läuft ohne sie weiter; auf einem gewöhnlichen Server starten sie, es liegt also am abgeschotteten Ursprung); Service Worker (EffectCraft) ebenso nicht.
- **PhotoCraft 0.21.0** startet nicht: `app/photocraft-web.js` lädt `app/snippets/eframe-…/inline0.js`, das im Upstream-Archiv fehlt (auch mit einem einfachen statischen Server 404, also ein Fehler im Paket von storytold; 0.20.2 hat denselben Aufbau). Upstream melden; bis dahin ist PhotoCraft **nicht im Katalog** (Entscheidung 7.11.2026: was nicht startet, kommt vorerst nicht in den Katalog).
- Nicht geprüft: die übrigen Adapter-Apps, Speichern über die Brücke im echten Fenster, Safari/Firefox.

## Dateimanager

- Doppelklick öffnet die **Standard-App** des Dateityps; Rechtsklick „Öffnen mit ›“ listet passende Web-Apps, den Texteditor und seit 0.69 Docker-Apps mit `x-titanos.opens` (`docs/OPEN-WITH-DOCKER.md`); Haken „Immer mit … öffnen“ setzt die Standard-App.
- Standard-Apps je Konto in der Einstellung `fileapps` (`PUT /api/v1/preferences/fileapps`, `{"<ext>":"webapp:<id>"|"editor"}`; Endungen klein ohne Punkt, 1–10 Zeichen, höchstens 200 Einträge, `<id>` muss im Katalog stehen, sonst `400 bad_request`; fehlt die Einstellung, gilt die Vorgabe aus dem Katalog), verwaltet unter Dateien › Einstellungen › Standard-Apps.

## Ersteinrichtung und Update

- Ersteinrichtung (`/welcome`): Schritt „Office-Programme“ mit den drei vorausgewählt (abwählbar) → `POST defaults/install` mit der Wahl (auch `{"apps":[]}`, damit später kein Hinweis kommt). Installiert, sobald das erste Volume eingehängt ist (`pending` in `/config/webapps.json`).
- Bestehende NAS: zwei Minuten nach dem Start einmal eine Mitteilung (`kind` `webapps_offer`, Info) „Office-Programme installieren?“, nur wenn ein Konto existiert, keine Web-App installiert oder vorgemerkt ist und `offered` fehlt; danach wird `offered` gespeichert (auch nach Neustart kein zweites Mal). Findet titand Docker-Versionen der Craft-Apps, heißt die Mitteilung `webapps_offer_docker` mit deren Namen als `subject` und nennt sie. Die Oberfläche zeigt dazu den Knopf (`defaults/install` bzw. `migrate`).
- App-Center: Abschnitt „Programme für den Browser“ mit allen Katalog-Apps, Installieren/Entfernen, Docker-Versionen ersetzen.
- Neue Versionen der Apps kommen nur mit TitanOS-Updates (neuer Katalog): titand installiert nach dem Start jede installierte App, deren Version nicht die des Katalogs ist, neu (im Hintergrund, auf ihrem Volume; die alte Version bleibt bis dahin nutzbar und wird danach entfernt; ein Fehlschlag wird nicht wiederholt, bis man „Installieren“ drückt).

## Ergänzungen 0.65.0 (PLAN 69)

- Der Öffnen-/Speichern-Dialog der Web-Apps (`FileDialog.svelte`) versteckt Punkt-Dateien wie der Dateimanager, auch `._name`-Begleitdateien eines Macs (sie sahen aus wie das Dokument und scheiterten in WordCraft, GridCraft und PdfCraft mit „not a readable …“).
- Der Hinweis „Office-Programme direkt im Browser“ wird je Knoten gemerkt: `webapps-0.63` für dieses NAS, `webapps-0.63@<knoten>` für einen anderen Knoten, den der Tab im Cluster verwaltet (die Hinweise liegen auf dem eigenen NAS, die Web-Apps auf dem verwalteten Knoten). Der Dialog nennt dann den Knoten.

## Ergänzungen aus dem Bau der Oberfläche (0.63.0)

- **Rahmen-Adresse:** `?author=<Name>&bridge=host|adapter&app=<id>`; `host.js` nimmt bevorzugt `data-app`/`data-bridge` am eigenen Script-Tag, die Adresse ist der Rückfall. Nie `host=parent`.
- **Ereignisse der Craft-Brücke** (`craft-libs`, `crates/web-host`): `<id>:ready/open/save/dirty/failed/handoff`, vom Host zur App zusätzlich `host`, `saved` und `handoff-done`. `save` trägt `name`, `bytes` (ArrayBuffer), optional `id` und `saveAs`; `failed` trägt `error`. Nicht blasend, am Canvas, Felder in `detail`.
- **`webapp-command {"command":"save-as"}`** (Fenster → host.js): löst „Speichern unter“ in der App aus (synthetisches F12; bei WordCraft geprüft). Kommt binnen 2,5 s nichts, zeigt das Fenster einen Hinweis.
- **Je `webapp-save` genau eine Antwort `webapp-saved`** (`ok:false` bei Abbruch, Konfliktabbruch, Fehler und für erzeugte Kopien), damit die Reihenfolge in host.js stimmt.
- **Speicherregel:** Speichert die App in einem anderen Format, das sie selbst bearbeitet (z. B. WordCraft `.doc` → `.docx`, GridCraft `.csv` → `.xlsx`), geht es über den Speicherdialog und wird danach die Datei des Fensters; Exporte (PDF usw.) überschreiben nie das Original, sondern bieten „In Freigabe speichern“ oder „Auf diesen PC“.
- **Fenster-Nachrichten** zwischen Seiten und Arbeitsplatz: `titanos-file-drag {drag}`, `titanos-open-file {root, path, app}`.
- **`fileapps`:** Wert `"editor"` = TitanOS (Vorschau bzw. Texteditor). Vorgabe: App der Grundausstattung, sonst die erste installierte; Typen, die TitanOS selbst zeigt (Bilder, SVG, Text), bleiben bei TitanOS.
- **Grenzen:** Im abgeschotteten Rahmen gibt es weder localStorage noch IndexedDB (Einstellungen und gespeicherte Schriften der Apps bleiben nicht erhalten); WebGPU nur über HTTPS, sonst WebGL2; Dateien werden ganz in den Arbeitsspeicher gelesen; Firefox und Safari ungeprüft; Konten mit eigener Symbolanordnung bekommen neue Web-Apps im Hauptmenü (Filter „Browser-Programme“) und im Dock, nicht automatisch auf den Arbeitsplatz; am Handy öffnet „⋯ › Öffnen“ eine Datei (Doppeltippen nicht).

## Hinter Cloudflare Access (0.71)

Die App lädt ihre eigenen Dateien (Module, WebAssembly) aus dem abgeschotteten Rahmen ohne die Cookies der Seite. Cloudflare Access verlangt sein Cookie und leitet diese Anfragen auf seine Anmeldung um; die App startet dann nie. `host.js` fragt deshalb beim Start einmal die eigene Seite so ab, wie die App es tut. Wird sie abgewiesen, meldet es `webapp-failed` mit `files_blocked`, und das Fenster erklärt statt „meldet sich nicht“: Für den Pfad `/webapps/` braucht Cloudflare Access eine Regel „Bypass“. Die Dateien unter `/webapps/` liefert titand ohnehin ohne Anmeldung aus (öffentliche Programmdateien, keine Daten des NAS).

## Eigene Fassungen: T-Word, T-PDF, T-Exel (0.72)

Seit 0.72 gehören **T-Word**, **T-PDF** und **T-Exel** zur Grundausstattung, nicht mehr WordCraft, PdfCraft und GridCraft. Sie stammen aus dem Repo [ra5on/TitanOS-Office](https://github.com/ra5on/TitanOS-Office) (PLAN 75-18/75-20).

- **Was sie sind:** eigene Fassungen der Craft-Originale, ohne ArtCraft-Marke und mit eigenen Kopien der Bausteine.
- **Katalog:** `repo: ra5on/TitanOS-Office`, gemeinsamer Tag `vX.Y.Z`, Archiv `t-<app>-web-X.Y.Z.zip` mit dem Ordner `t-<app>-web-X.Y.Z/`.
- **`trusted: true`:** Weil sie aus unserem eigenen Build stammen, liefert titand sie ohne CSP-Sandbox aus. Das Fenster rahmt sie ohne `sandbox`. Sie laufen damit auf der Herkunft der Weboberfläche und laden ihre Dateien mit den Cookies der Seite. Dafür bekommen sie seit 0.76 eine eigene, enge CSP (`trustedPolicy` in `webapps/serve.go`, F-2-15): `default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' blob:; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'`. Alles, was ein Programm lädt, und jede Adresse, mit der es spricht, ist damit dieses NAS: kein Skript, Stil, Bild oder Font und keine Anfrage (`fetch`, WebSocket) an einen fremden Host. Eine Lücke beim Anzeigen einer präparierten Datei kann so weder Code nachladen noch etwas wegschicken. Inline-Skripte und WebAssembly brauchen die Programme zum Starten, `data:`/`blob:` für erzeugte Dokumente, Bilder und Worker; `eval` bleibt verboten. Mit den Builds des Zweigs `durchsicht/077-office` in einem TitanOS-Fenster geprüft (10.11.2026): alle 14 Programme starten, öffnen und speichern ohne einen CSP-Verstoß. Bewusst gesperrt bleibt, was nur mit fremden Hosts ginge und im Build als Adresse steckt: Modelle von `huggingface.co`, `storage.googleapis.com` und `raw.githubusercontent.com` nachladen (T-Effect, T-Film, T-Light) und die Zusammenarbeit über `wss://collab.craftapps.dev` (T-Fig). Wer die Richtlinie weitet, nennt den genauen Bedarf und den Host; ein Test (`webapps_test.go`) hält die Direktiven fest. Hinter Cloudflare Access brauchen sie deshalb keine Bypass-Regel, und die Prüfung „Programmdateien kommen nicht an“ entfällt für sie. Bewusst gesperrt ist damit, was die Programme von fremden Servern holen wollen (Entscheidung des Inhabers vom 10.11.2026, „streng, ohne Ausnahmen“): nachladbare KI-Modelle der Originale (T-Effect: Objekterkennung, Gesichtspunkte, Plug-in-Liste; T-Film: Whisper-Untertitel, Kokoro-Sprachausgabe; T-Light: Gesichtserkennung) und die Online-Zusammenarbeit von T-Fig. Der KI-Agent von TitanOS ist nicht betroffen: er läuft auf dem NAS und gehört nicht zu den Web-Apps.
- **Fremde Builds:** Die Craft-Originale und alle anderen Apps bleiben abgeschottet wie bisher.
- **Bestehende Installationen:** Die Originale bleiben im Katalog. Wer sie installiert hat, behält sie, bis er sie im App-Center entfernt.

## Dateiauswahl der App und Nachfolger (0.73, PLAN 75-20)

- **Öffnen in der App zeigt die Dateien des NAS.** Öffnet eine App ihre eigene Dateiauswahl (Datei › Öffnen, Bild einfügen, Schrift hinzufügen: ein `<input type=file>`), fängt `host.js` das ab (`click`, `showPicker`, ein sichtbarer Knopf) und meldet `{"type":"webapp-pick","accept","multiple"}`. Das Fenster zeigt seinen Dateidialog mit den Typen aus `accept` (`.docx` → docx, `image/*` → die üblichen Bildtypen, Unbekanntes → alle Dateien) und „Von diesem Gerät …“ für eine Datei dieses PCs. Die Antwort `{"type":"webapp-picked","files":[{"name","bytes"}]}` (leer = abgebrochen) legt `host.js` in das Feld der App und löst `input`/`change` aus, bei Abbruch `cancel`. Gilt für alle Web-Apps, abgeschottet oder nicht.
- **Speichern einer so geöffneten Datei:** Die App kennt den Ort nicht und meldet beim ersten Speichern `saveAs: true` mit dem Dateinamen. Hat das Fenster unter genau diesem Namen eine Datei des NAS für die App geöffnet und gehört die Endung zu den Typen der App, schreibt es in diese Datei (`expect` = ihre Zeit beim Lesen, `409` → Rückfrage „Ersetzen?“) und macht sie zur Datei des Fensters. Sonst der Speicherdialog wie bisher.
- **`successor`** im Katalog (wordcraft → t-word, pdfcraft → t-pdf, gridcraft → t-exel und die übrigen Craft-Apps → ihre T-Fassung): Docker-Versionen einer Craft-App werden durch den Nachfolger ersetzt. Im App-Center zeigt ein installiertes Original „Durch T-Word ersetzen“: erst wird der Nachfolger installiert, wenn er bereit ist, wird das Original entfernt.
- **Fehler in 0.72 behoben:** Die Liste der Web-Apps in der Oberfläche verlor `trusted`; T-Word, T-PDF und T-Exel liefen dadurch weiter abgeschottet und über Cloudflare Access nicht ohne Bypass-Regel.

## Alle Craft-Apps als T-Fassungen (0.73, TitanOS-Office 1.1.0; Stand 0.75: TitanOS-Office 1.3.0)

| T-Fassung | Original | Brücke | Typen |
|---|---|---|---|
| T-Word, T-PDF, T-Exel | WordCraft, PdfCraft, GridCraft | host | wie oben, jetzt mit vollständigen deutschen Übersetzungen |
| T-Deck | DeckCraft | host | pptx potx ppsx |
| T-CAD | CADCraft | host | dxf |
| T-Fig | FigCraft | host | figcraft |
| T-Film | FilmCraft | host | fcproj |
| T-Light | LightCraft | host | – |
| T-Vector | VectorCraft | adapter | svg ai |
| T-Design | DesignCraft | adapter | designcraft idml |
| T-Effect | EffectCraft | adapter | ecproj |
| T-Sound | SoundCraft | adapter | scraftz wav |
| T-Texture | TextureCraft | adapter | tcraft obj glb gltf fbx stl |
| T-Material | MaterialCraft | adapter | sbs |

- Alle `trusted`, Version 1.1.0, Archiv `t-<x>-web-1.1.0.zip` aus `ra5on/TitanOS-Office`. Zur Grundausstattung gehören weiter nur T-Word, T-PDF und T-Exel.
- **Geprüft (9.11.2026):** Jede der elf neuen Fassungen startet in Chromium (SwiftShader-WebGL) von einem einfachen Server, mit Titel T-… und ohne Skriptfehler. Im TitanOS-Fenster mit Öffnen und Speichern ist nur T-Word (bzw. das baugleiche WordCraft) im UI-Test.
- **Grenzen:**
  - Beispieldokumente der Adapter-Apps (z. B. T-Effect) nennen in ihrem Inhalt noch den Originalnamen.
  - T-Fig, T-Light und T-Vector bauen ohne craft-fonts, nur mit den eingebauten Schriften.
  - T-Effect lief upstream mit Cross-Origin-Isolation (Threads). TitanOS sendet sie nicht; die App startet trotzdem.

## Alte Craft-Apps werden selbst ersetzt (0.74, PLAN 75-20)

Entscheidung des Projektinhabers vom 9.11.2026: ohne Rückfrage.
- **Web-Originale** (WordCraft, PdfCraft …): titand installiert den Nachfolger (T-Word, T-PDF …) und entfernt danach das Original. „Öffnen mit“-Zuordnungen aller Konten (`fileapps`, `webapp:wordcraft`) zeigen danach auf den Nachfolger. Scheitert die Installation, bleibt das Original.
- **Docker-Fassungen**: nach der Installation der T-App wird die Docker-App **samt ihrer Docker-Daten** entfernt (`docker compose down --volumes`, Datenordner der App). Dateien in den Freigaben bleiben. Der Fortschritt steht in `migration` wie bei der früheren Umstellung per Dialog.
- Läuft zwei Minuten nach dem Start und danach stündlich (Docker und das App-Volume können später kommen), nur wenn ein App-Volume da ist (`RunCraftReplacement`, `api/craft_replace.go`).
- Das App-Center bietet Originale mit Nachfolger nicht mehr an (installierte erscheinen bis zur Ersetzung). Der Dialog nach dem Update fragt nicht mehr nach Docker-Apps.
- Der Docker-Katalog (TitanOS-Apps) enthält die zwölf Craft-Apps mit T-Fassung nicht mehr; PhotoCraft und SolidCraft bleiben, ihre Ports bleiben gleich.

## Ereignisname der eigenen Fassungen (0.74)

Die T-Apps senden die Host-Ereignisse weiter unter dem Namen ihres Originals (`wordcraft:ready`, `wordcraft:save`). titand gibt `host.js` diesen Namen als `data-events` mit (`App.EventPrefix`); vorher kam bei den T-Apps mit `bridge: host` kein Ereignis an (Öffnen/Speichern über die Brücke ging nicht). **`startNew`** im Katalog (T-Word): ohne Datei geöffnet bleibt die App leer; das Fenster schickt dann „Neu“ (Strg+N), T-Word zeigt seine Startseite mit den Vorlagen.
