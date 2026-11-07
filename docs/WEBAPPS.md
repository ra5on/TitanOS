# Web-Apps direkt (PLAN 66)

Seit 0.63.0 laufen Programme, die es als reine Browser-Seite gibt (die Craft-Programme von storytold: WordCraft, GridCraft, PdfCraft u. a.), **direkt im Browser** im TitanOS-Fenster: kein Stream, keine Last auf dem NAS, auch über Cloudflare und am Handy. TitanOS liefert ihre Dateien selbst aus und reicht Dateien hinein und wieder heraus („Öffnen mit“, Standard-Apps, Speichern ins Original). Entscheidungen des Projektinhabers vom 7.11.2026 (PLAN 66, 66-1, 66-2).

## Katalog und Herkunft

- `webapps/catalog.json` im Repository: je App `id` (z. B. `wordcraft`), `name`, `repo` (`storytold/wordcraft`), `version`, `upstreamAsset` (`wordcraft-web-0.16.0.zip`), `sha256` des Upstream-Archivs, `bridge` (`host` = Craft-Host-Brücke `craft-web-host`, `adapter` = ohne Brücke), `types` (Dateiendungen) und `default` (gehört zur Grundausstattung). Grundausstattung: WordCraft (`docx doc odt rtf`), GridCraft (`xlsx xls ods csv`), PdfCraft (`pdf`).
- Die Release-CI lädt jedes Archiv von GitHub, prüft den SHA-256 aus dem Katalog, prüft den Inhalt (nur reguläre Dateien, keine Pfade mit `..`/absolut, Größe ≤ 64 MiB entpackt, enthält `index.html`) und legt es als `titanos-webapp-<id>-<version>.zip` ins Release; das signierte `manifest.json` bekommt `"webapps": [{"id","version","asset","sha256","size"}]` (optional, ältere titand ignorieren es).
- titand lädt eine App nur aus dem Manifest **seines eigenen Releases** (Signatur wie beim Abbild der Fernansicht), prüft Größe und SHA-256, entpackt sicher nach `<volume>/.titanos/webapps/<id>/<version>/` (root, 0755/0644) und merkt sich installierte Apps in `/config/webapps.json` (`{"installed":[{"id","version","volume"}]}`). Nie auf dem Stick. Im Entwicklungsmodus: Archive aus einem lokalen Ordner (`-webapps-dir`), Ablage in einem Temp-Ordner.

## API

- `GET /api/v1/webapps` (jedes Konto): `{"apps":[{"id","name","version","installed":bool,"state":"missing|downloading|ready|error","progress","error","bridge","types":[...],"default":bool,"url":"/webapps/<id>/<version>/"}],"volume":"<name>"}`.
- `POST /api/v1/webapps/{id}/install`, `DELETE /api/v1/webapps/{id}` (Administratoren), `POST /api/v1/webapps/defaults/install` (Grundausstattung, für Ersteinrichtung und Hinweis nach Update).
- `GET /webapps/{id}/{version}/{path...}`: die Dateien der App, **ohne Anmeldung** (öffentlicher Open-Source-Code; der abgeschottete Rahmen sendet keine Cookies), nur für installierte Apps, richtige MIME-Typen (`.wasm` = `application/wasm`), `Cache-Control: immutable` für Dateien mit Hash, `no-cache` für `index.html`, gzip, Kopfzeilen `Content-Security-Policy: sandbox allow-scripts allow-downloads allow-modals allow-popups; frame-ancestors 'self'`, `X-Content-Type-Options: nosniff`, `Cross-Origin-Resource-Policy: cross-origin` und `Access-Control-Allow-Origin: *` (der abgeschottete Rahmen hat den Ursprung `null`; ES-Module und `.wasm` brauchen dann CORS – die Dateien sind ohnehin öffentlich). `index.html` liefert titand ergänzt aus: `data-host="page"` am Canvas der App (Craft-Brücke im selben Dokument) und `<script src="/webapps/_host/host.js">` vor dem App-Skript.
- `GET /webapps/_host/host.js`: der TitanOS-Vermittler im abgeschotteten Dokument (Teil der Weboberfläche, statisch).
- Dateien: Lesen über `GET /api/v1/files/download?path=…` (wie bisher), Schreiben über **neu** `PUT /api/v1/files/content?path=…&expect=<mtime-ns|none>` (Rumpf = Datei, Rechte des Kontos wie beim Hochladen, atomar über temp + rename, `409 file_changed` wenn sich die Datei seit `expect` geändert hat, `expect=none` = neue Datei, darf nicht existieren), Antwort `{"mtime":…,"size":…}`.

## Ablauf im Fenster

```
TitanOS-Fenster (WebAppView, Ursprung TitanOS)
   └─ <iframe sandbox="allow-scripts allow-downloads allow-modals allow-popups"   (eigener, undurchsichtiger Ursprung:
        src="/webapps/<id>/<version>/?author=<Name>">                               keine Cookies, keine TitanOS-API)
         host.js  ⇄  App (gleiches Dokument, CustomEvents der Craft-Brücke oder Adapter)
```

Nachrichten zwischen Fenster und `host.js` per `postMessage` (Fenster prüft `event.source === iframe.contentWindow`, sendet mit `targetOrigin "*"`, weil der Rahmen keinen Ursprung hat; Inhalte sind nur Dateiname und Bytes):

| Richtung | Nachricht |
|---|---|
| host → Fenster | `{"type":"webapp-ready"}` sobald die App `<id>:ready` meldet (Adapter: wenn der Canvas da ist) |
| Fenster → host | `{"type":"webapp-open","name","bytes":ArrayBuffer}` (übertragen, nicht kopiert) |
| host → Fenster | `{"type":"webapp-save","name","saveAs":bool,"bytes":ArrayBuffer}` |
| host → Fenster | `{"type":"webapp-dirty","dirty":bool}`, `{"type":"webapp-failed","text"}` |
| Fenster → host | `{"type":"webapp-saved","ok":bool,"text"}` (nur zur Anzeige in der App, falls unterstützt) |

- **Brücke** (`bridge: host`): `host.js` spricht die Craft-Host-Ereignisse `<id>:ready/open/save/dirty/failed` am Canvas (`data-host="page"`).
- **Adapter** (`bridge: adapter`): Öffnen = synthetisches `drop` mit `DataTransfer`/`File` auf den Canvas; Speichern = Abfangen von Downloads (`HTMLAnchorElement.prototype.click` bei `download`-Attribut und `blob:`-Adressen, `URL.createObjectURL`), `saveAs` immer `true` (der Nutzer wählt Ziel).
- **Speichern:** `saveAs:false` → `PUT /files/content` aufs Original mit `expect`=mtime beim Öffnen; `409` → Dialog „Datei wurde inzwischen geändert: Überschreiben / Als Kopie speichern / Abbrechen“. `saveAs:true` → Speicherdialog von TitanOS (Ordner und Name, vorausgefüllt), danach ist das die Datei des Fensters. Exporte (PDF usw.) ebenfalls über den Speicherdialog.
- **Ungespeicherte Änderungen:** Schließen des Fensters mit `dirty` fragt nach.

## Dateimanager

- Doppelklick öffnet die **Standard-App** des Dateityps; Rechtsklick „Öffnen mit ›“ listet passende Web-Apps, Docker-Apps nicht (vorerst), den Texteditor; Haken „Immer mit … öffnen“ setzt die Standard-App.
- Standard-Apps je Konto in der Einstellung `fileapps` (`{"<ext>":"webapp:<id>"}`), Vorgabe aus dem Katalog; verwaltet unter Dateien › Einstellungen › Standard-Apps.

## Ersteinrichtung und Update

- Ersteinrichtung (`/welcome`): Schritt „Office-Programme“ mit den drei vorausgewählt (abwählbar). Installiert, sobald das erste Volume eingehängt ist (titand merkt sich die Wahl in `/config/webapps.json` als `"pending":["wordcraft",…]`).
- Bestehende NAS: nach dem Update einmal eine Mitteilung für Besitzer/Administratoren „Office-Programme installieren?“ mit Knopf (`defaults/install`).
- App-Center: Abschnitt „Programme für den Browser“ mit allen Katalog-Apps, Installieren/Entfernen.
