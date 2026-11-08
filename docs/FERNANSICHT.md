# Fernansicht für Docker-Apps (PLAN 63)

Seit 0.61.0 öffnet das Fenster einer Docker-App die App **nicht mehr im Browser des Nutzers**, sondern TitanOS öffnet sie auf dem NAS in einem eigenen Browser (Chromium in einem Container) und überträgt Bild und Ton über die TitanOS-Adresse ins Fenster. Das geht überall, wo TitanOS selbst erreichbar ist (Heimnetz, VPN, Cloudflare-Tunnel, Handy), weil nur HTTPS und WebSocket zur TitanOS-Adresse nötig sind. Entscheidung des Projektinhabers vom 6.11.2026: „Immer Fernansicht“ (PLAN 62-4, 63, 63-1).

## Aufbau

```
Browser (Fenster)  ──WebSocket (TitanOS-Adresse, Anmeldung)──▶  titand  ──WebSocket (internes Netz, Sitzungsschlüssel)──▶  Agent im Viewer-Container
   WebCodecs: H.264 → Canvas, Opus → Lautstärke                     Rechte, Ende bei Abmelden/Sperre/Trennzeit          Xvfb + Chromium (Kiosk, nur die App) + GStreamer + PulseAudio
```

- **Abbild** `titanos-viewer`: Debian trixie, Chromium, Xvfb, GStreamer (x264, VA-API), PulseAudio, xclip, Schriften, der Agent `titanos-viewer` (Go, `backend/cmd/titanos-viewer`). Gebaut im Release, als signierte Release-Datei (`manifest.json` → `viewer`), auf dem NAS bei der ersten Fernansicht geladen, geprüft (Signatur des Manifests, SHA-256, Größe) und mit `docker load` eingespielt. Kein Zug aus einer Registry.
- **Container** je Benutzer und App (`titanos-viewer-<hash>`, Labels nennen Konto und App): nicht root (UID 1000), `--cap-drop ALL`, `no-new-privileges`, Wurzel nur lesbar, beschreibbar nur `/profile` (Browserprofil auf einem Daten-Volume unter `.titanos/viewer/profiles/<user>/<app>`) und tmpfs `/tmp`. GPU nur mit Einstellung (`/dev/dri`).
- **Chromium-Sandbox: aus (`--no-sandbox`).** Chromiums eigene Sandbox braucht Benutzer-Namensräume (oder das setuid-Hilfsprogramm). Mit `--cap-drop ALL` und Dockers Standard-Seccomp/AppArmor sind Namensräume verboten, und das setuid-Hilfsprogramm geht mit `no-new-privileges` nicht. `--disable-namespace-sandbox` bzw. `--disable-setuid-sandbox` statt `--no-sandbox` wurden mit den Container-Einstellungen von titand geprüft (7.11.2026): Chromium startet dann nicht („No usable sandbox“); die Seccomp-BPF-Schicht lässt sich ohne die erste Schicht nicht allein einschalten. Ohne Sandbox laufen die Renderer daher ohne eigene Trennung voneinander und vom Browserprozess; eine Lücke in Chromium gibt die Rechte von UID 1000 **im Container**. Die Grenze ist der Container: Wurzel nur lesbar, keine Capabilities, `no-new-privileges`, Docker-Seccomp, eigenes internes Netz ohne Weg nach draußen und zum NAS, Richtlinie nur lesbar eingebunden, beschreibbar nur `/tmp` und das Profil dieses Benutzers für diese App. Deshalb wird das Abbild bei Sicherheitsupdates von Chromium (und x264, GStreamer, Mesa) und spätestens nach 7 Tagen neu gebaut, nicht wiederverwendet (`scripts/viewer-image-reuse.py`).
- **Platz:** Hochladen nur, wenn auf dem Profil-Volume danach mindestens 1 GiB frei bleibt und Hochgeladenes plus Downloads je Profil höchstens 4 GiB sind (geprüft vor dem Hochladen und vor jedem Teil; sonst `uploaded` mit `error: "no_space"`). Laufende und dann die ältesten Downloads werden gelöscht, wenn die Grenzen überschritten sind. „Hochgeladen“ wird beim Start und Ende des Containers geleert, Downloads älter als 24 Stunden beim Start gelöscht.
- **Netz:** je Ansicht ein eigenes internes Docker-Netz `titanos-viewer-<hash>` (ohne Weg nach draußen, Bridge `tvw<12 hex>` auf dem Host); darin nur dieser Viewer und der Web-Container dieser App, solange die Ansicht besteht. Der Host verwirft neue Verbindungen aus diesen Netzen (nft, siehe unten). Chromium-Richtlinien (`proto.ChromiumPolicy`) erlauben nur die Adresse der App (`URLBlocklist: *`, `URLAllowlist: <App>`), keine Entwicklerwerkzeuge, kein `file://`; titand schreibt sie auf dem Host und bindet die Datei nur lesbar ein.

## Protokoll Browser ↔ titand ↔ Agent

Ein WebSocket, `GET /api/v1/viewer/{app}/stream` (Anmeldung, Recht auf die App wie beim App-Fenster). titand reicht Nachrichten unverändert zwischen Browser und Agent durch (der Agent ist unter `ws://<container>:7300/stream` mit dem Sitzungsschlüssel im Kopf `X-Viewer-Key` erreichbar; ein Schlüssel in der Adresse gilt nicht), prüft aber Richtung Agent die Länge und den Typ.

**Binäre Nachrichten** – erstes Byte ist der Typ:

| Typ | Richtung | Inhalt |
|---|---|---|
| `0x01` Video | Agent → Browser | Byte 1: Flags (Bit 0 = Schlüsselbild); Byte 2–9: Zeitstempel in µs (uint64, big endian); Rest: eine H.264-Zugriffseinheit (Annex B, mit Startcodes) |
| `0x02` Ton | Agent → Browser | Byte 1–8: Zeitstempel in µs; Rest: ein Opus-Paket (48 kHz, Stereo, 20 ms) |
| `0x03` Datei-Teil | Browser → Agent | Hochladen: Byte 1–4 Upload-Nummer (uint32), Rest Daten (höchstens 256 KiB je Nachricht) |
| `0x04` Kachel (seit 0.68) | Agent → Browser | Byte 1: 1 = JPEG, 2 = PNG (verlustfrei), 3 = WebP verlustfrei (seit 0.69, nur wenn der Browser `webp: true` in `size`/`quality` schickt); Byte 2–9: x, y, Breite, Höhe (je uint16, big endian); Rest: das Bild dieses Bereichs. Nur während `hello.codec` `"tiles"` ist |

**Text-Nachrichten** – JSON mit `"t"`:

Agent → Browser:
- `{"t":"hello","codec":"avc1.42E01F","width":1280,"height":800,"gpu":"vaapi"|"nvenc"|"cpu","audio":true}` – nach dem Verbinden und nach jeder Größenänderung (neuer Decoder, danach kommt ein Schlüsselbild).
- `{"t":"hello","codec":"tiles",…}` (seit 0.68): Kachel-Betrieb, kein Decoder; danach kommen `0x04`-Kacheln, zuerst der ganze Bildschirm.
- `{"t":"mode","mode":"tiles"|"video"}` (seit 0.68) – welche Übertragung gerade läuft (Anzeige im ⋯-Menü).
- `{"t":"stats","fps":59,"kbps":4200,"latencyMs":18}` – jede Sekunde.
- `{"t":"clipboard","text":"…"}` – die Zwischenablage im NAS-Browser hat sich geändert (höchstens 1 MiB).
- `{"t":"download","name":"bild.jpg","size":12345,"id":"d1"}` – eine Datei wurde heruntergeladen; abholen über `GET /api/v1/viewer/{app}/downloads/{id}`.
- `{"t":"cursor","shape":"pointer"|"text"|"default"|…}` – Mauszeigerform (der Zeiger wird nicht ins Bild gezeichnet).
- `{"t":"bye","reason":"idle"|"logout"|"stopped"|"error","text":"…"}`.

Browser → Agent:
- `{"t":"size","width":1280,"height":800,"scale":2}` – sichtbare Fläche in CSS-Pixeln und Gerätefaktor; der Agent stellt den Bildschirm darauf ein (gerundet auf gerade Zahlen, 320–3840 × 240–2160).
- `{"t":"mouse","x":100,"y":50,"buttons":1}` – absolute Lage in Bildpunkten des Bildschirms und gedrückte Tasten (Bitmaske wie `MouseEvent.buttons`).
- `{"t":"wheel","dx":0,"dy":-120}`.
- `{"t":"key","code":"KeyA","key":"a","down":true,"mods":{"shift":false,"ctrl":false,"alt":false,"meta":false}}` – druckbare Zeichen ohne Strg/Alt/Meta werden über `key` getippt (unabhängig vom Tastaturlayout), alles andere über `code`.
- `{"t":"text","text":"Hallo"}` – Text tippen (Handy-Tastatur, Einfügen).
- `{"t":"clipboard","text":"…"}` – Zwischenablage des Nutzers in den NAS-Browser legen.
- `{"t":"keyframe"}` – Schlüsselbild anfordern (nach Fehler im Decoder).
- `{"t":"quality","mode":"auto"|"high"|"balanced"|"low","fps":30|60,"transport":"auto"|"video"}` (`transport` seit 0.68, darf fehlen).
- `{"t":"upload","id":1,"name":"a.pdf","size":123}` → danach Teile `0x03`, Agent antwortet `{"t":"uploaded","id":1}`; die Datei liegt im NAS-Browser unter „Hochgeladen“ zur Auswahl im Dateidialog.
- `{"t":"touch", …}` gibt es nicht: das Handy schickt `mouse`/`wheel` (Tippen = Klick, Wischen = Rad, langes Tippen = Rechtsklick).

## Einstellungen

`GET/PUT /api/v1/viewer/settings` (Administratoren), `/config/viewer/settings.json`:
`{"gpu":"auto"|"off"|"/dev/dri/renderD128","quality":"auto","fps":30,"transport":"auto"|"video","audio":true,"volume":"<Volume für Profile>"}`. Ab Werk 30 fps (seit 0.66.0, Inhaber 7.11.2026; vorher 60); ein gespeicherter Wert bleibt. `transport` seit 0.68 (fehlt: `auto`; ältere Versionen ignorieren das Feld).

## App-Gefühl im Fenster (seit 0.68)

- **Startbild:** Bis das erste Bild da ist, zeigt das Fenster das Symbol der App groß, ihren Namen und einen laufenden Balken (Entwurf 66-01) statt des Kreisels.
- **Dateien ins Fenster ziehen:** Abgelegte Dateien gehen über den vorhandenen Upload (`upload` + `0x03`) in die App unter „Hochgeladen“; ein Hinweis nennt den Ort (Entwurf 66-04).
- Im ⋯-Menü unter „Qualität“: „Kacheln, bei Bewegung Video“ oder „Immer Video“, je App im Browser gemerkt; die Statuszeile nennt die laufende Übertragung.

## Kachel-Betrieb (PLAN 66/72, seit 0.68)

Nach dem Vorbild von KasmVNC, selbst geschrieben (`backend/cmd/titanos-viewer/tiles.go`, `stream.go`):
- **Erfassen:** eine zweite GStreamer-Leitung liefert rohe Bilder (`ximagesrc … ! videoconvert ! BGRx ! fdsink fd=3`) mit der eingestellten Bildrate.
- **Vergleichen:** Kacheln von 64 × 64 Bildpunkten, Zeile für Zeile mit dem zuletzt **gesendeten** Stand verglichen. Nur geänderte Kacheln gehen raus, waagrecht benachbarte einer Zeile als ein Rechteck.
- **Qualität je Bereich:** JPEG, wo sich etwas bewegt (in 3 der letzten 5 Bilder geändert) mit niedrigerer Qualität, sonst höher (`auto` 80/55, `high` 90/75, `balanced` 75/50, `low` 55/35).
- **Nachschärfen:** Ein Bereich, der verlustbehaftet gesendet wurde und 300 ms ruhig ist, kommt verlustfrei (höchstens 48 Kacheln je Bild). Text ist nach dem Scrollen wieder gestochen scharf.
  - Seit 0.69 kommt er als **WebP verlustfrei**, kodiert mit `nativewebp` (reines Go, MIT, ohne libwebp). Das ist kleiner und schneller als PNG.
  - Ältere Seiten melden kein `webp` und bekommen weiter PNG.
  - Verlustbehaftetes WebP gibt es in Go nicht ohne cgo; die bewegten Bereiche bleiben JPEG. So fällt auch KasmVNC ohne Zeitbudget auf JPEG zurück.
- **Alle Kerne** (seit 0.69): Die Bereiche eines Bildes werden parallel kodiert, höchstens 8 gleichzeitig, und in ihrer Reihenfolge gesendet.
- **Volle Leitung:** Passt eine Kachel nicht mehr in die Sendewarteschlange, wird sie nicht als gesendet gemerkt und beim nächsten Bild erneut geschickt; es gibt keinen Rückstau.
- **Videomodus:** Ändern sich mindestens 45 % des Bildschirms 5 Sekunden lang, startet der bisherige H.264-Weg (`hello` mit avc-Codec, `mode: video`); eine langsame Erfassung (4 Bilder/s) misst weiter. Nach 3 Sekunden unter 45 % geht es zurück zu Kacheln (neues `hello` `tiles`, alle Kacheln neu).
  - Seit 0.69 ist das Video im Videomodus **höchstens 1920 × 1080**, wie bei KasmVNC: Ein größerer Bildschirm wird mit gleichem Seitenverhältnis verkleinert (CPU: `videoscale`, VA-API: auf der GPU im Postprozessor). Die Bitrate richtet sich nach der kleineren Größe.
  - `hello` nennt weiter die Bildschirmgröße; der Browser zieht das Bild beim Zeichnen auf die Leinwand, Mauskoordinaten bleiben die des Bildschirms.
  - „Immer Video“ behält die volle Größe.
- **Skalierung im Browser** (seit 0.69, wie KasmVNC): Bei ganzzahliger Vergrößerung (in Gerätepixeln 2×, 3× …) zeichnet die Leinwand ohne Weichzeichnen (`image-rendering: pixelated`), sonst geglättet.
- **Browser:** Kacheln werden parallel dekodiert (`createImageBitmap`), aber in Ankunftsreihenfolge gezeichnet; die Leinwand wird beim Wechsel nicht geleert, wenn die Größe gleich bleibt.
- **Umgebung:** `VIEWER_TRANSPORT=auto|video` (ab Werk `auto`). „Immer Video“ ist der Weg bis 0.67.
- **Gemessen** (8.11.2026, `viewer/test-integration.sh`, 1280×800, CPU, Testseite mit blinkender Schreibmarke): erstes Bild 95 KiB (39 JPEG- und 17 PNG-Kacheln), danach ruhig etwa 7 KB/s (nur die Schreibmarke), Tippen schickt 8 % der Fläche; Vollbild-Animation schaltet nach 5–8 s auf Video und 3,3 s nach dem Ende zurück. Nicht gemessen: Verzögerung Eingabe → Bild, gedrosselte Leitungen, 4K, GPU, echte Apps.
- **Gemessen 0.69** (8.11.2026, gleicher Test): Die 21 nachgeschärften Kacheln kamen alle als WebP, ein ruhiger Bildschirm braucht etwa 2,4 KB/s, Video nach 7,8 s und zurück nach 3,3 s. Die verkleinerte Pipeline (3840 × 2160 → 1920 × 1080, x264) lief im Abbild mit `videotestsrc`. Nicht gemessen: echter 4K-Bildschirm mit Videomodus, VA-API-Verkleinerung, Gewinn durch alle Kerne auf echter Hardware.
`GET /api/v1/viewer/status`: Abbild geladen/lädt/fehlt, GPU-Geräte, offene Ansichten mit RAM, geschätzter Platz für weitere.

## Lebenslauf einer Ansicht

1. Fenster öffnet `…/stream`. titand prüft Recht, Anzahl und Speicher (siehe „Grenzen je Ansicht“), lädt bei Bedarf das Abbild, legt Profilordner an, startet den Container und hängt die App ans interne Netz.
2. Der Agent startet Xvfb, PulseAudio und Chromium mit der App-Adresse, dann Bild- und Tonstrom.
3. Ende: Fenster zu → Container läuft 2 Minuten weiter (schnelles Wiederöffnen), dann wird er gestoppt und entfernt; das Profil bleibt. Abmelden, Sperren, Passwortwechsel, Entzug des Rechts und die Trennzeit der Konsolen beenden den Strom wie bei Konsolen (Prüfung alle 3 s).

## titand-Seite im Einzelnen (Teil B)

Code: `backend/internal/viewer/` (Einstellungen, Abbild, Container, Prüfung der Nachrichten, Weiterleitung), `backend/internal/docker/viewer.go` (die einzigen Docker-Aufrufe der Fernansicht), `backend/internal/api/viewer.go` (Routen), `backend/internal/plugins/release_assets.go` (signiertes Manifest und Release-Dateien, gemeinsam mit der NVIDIA-Bauumgebung).

### Routen (nur Weboberfläche, nicht in `publicRoutes`, kein MCP/Agent)

| Route | Wer | Antwort |
|---|---|---|
| `GET /api/v1/viewer/settings` | Besitzer/Admin | `{"gpu":"auto","quality":"auto","fps":30,"audio":true,"volume":""}` |
| `PUT /api/v1/viewer/settings` | Besitzer/Admin | gleiche Form (fehlende Felder behalten den bisherigen Wert); 400 `viewer_settings_invalid` bei unbekannter GPU (nur `id` aus `status.gpus`), `quality` außer `auto|high|balanced|low`, `fps` außer 30/60, nicht eingebundenem Volume. `volume: ""` = Volume von Docker |
| `GET /api/v1/viewer/status` | jedes Konto | siehe unten; Benutzer sehen nur ihre eigenen Ansichten und keine GPU-Liste |
| `POST /api/v1/viewer/image` | Besitzer/Admin | Abbild jetzt laden (sonst mit der ersten Ansicht; nach einem Fehler sofort neu); Antwort wie `status.image` |
| `GET /api/v1/viewer/{app}/stream` | Recht auf die App | WebSocket (siehe Protokoll) |
| `GET /api/v1/viewer/{app}/downloads/{id}` | Recht auf die App, eigene offene Ansicht | Datei als `attachment` (`application/octet-stream`, `nosniff`, Name nach RFC 5987); 404 `viewer_no_session` ohne offene Ansicht, 403 `app_forbidden` |

`GET /api/v1/viewer/status`:

```json
{
  "image": {"state": "missing|downloading|loading|ready|error", "version": "0.61.0",
            "bytes": 123, "size": 456, "progress": 0.27, "error": "…", "dev": false},
  "gpu": "vaapi|nvenc|cpu",
  "gpus": [{"id": "/dev/dri/renderD128", "kind": "vaapi", "vendor": "8086", "title": "Intel … · 0000:00:02.0",
            "available": true, "reason": ""}],
  "sessions": [{"user": "anna", "app": "immich", "state": "starting|open|idle", "since": "2026-11-06T21:00:00Z",
                "windows": 1, "gpu": "vaapi", "memoryBytes": 314572800}],
  "memory": {"availableBytes": 6000000000, "reserveBytes": 734003200, "perViewBytes": 1073741824, "more": 12},
  "idleMinutes": 15
}
```

`gpu` ist der Kodierer einer jetzt geöffneten Ansicht. `idleMinutes` ist die Trennzeit der Konsolen, die auch Ansichten beendet (0 = nie). Nach einem Fehler beim Laden des Abbilds versucht titand es frühestens nach einer Minute erneut (außer per `POST …/image`). `gpus[].id` ist bei Intel/AMD der Render-Knoten, bei NVIDIA die PCI-Adresse; `reason` ist `vm` (an eine VM gegeben) oder `driver`. NVIDIA braucht das Treiber-Plugin (CDI `nvidia.com/gpu=<UUID>`). `state: idle` heißt: kein Fenster verbunden, der Container wartet noch bis zu 2 Minuten.

### Ergänzungen am Protokoll

Ein Browser kann die Antwort eines abgelehnten WebSocket-Handshakes nicht lesen. Deshalb nimmt titand die Verbindung jeder angemeldeten Sitzung an und meldet Probleme als Nachricht, danach schließt es mit Code 1008 und dem Fehlercode als Grund:

- `{"t":"starting"}` – titand → Browser, sofort nach dem Verbinden (Recht geprüft), bevor der Container startet.
- `{"t":"error","code":"…","text":"…"}` – titand → Browser, danach Ende. Codes: `app_forbidden`, `viewer_image` (mit `"image": {…}` wie im Status; das Abbild wird geladen: Status abfragen und neu verbinden), `viewer_memory` (mit `"memory": {…}`; auch wenn die Höchstzahl aus dem RAM erreicht ist), `viewer_limit` (das Konto hat schon 4 Ansichten offen), `viewer_hostnet` (App im Host-Netz: „In eigenem Tab“ anbieten), `viewer_no_webui`, `viewer_not_running`, `viewer_volume` (kein eingebundenes Volume für Profile), `viewer_agent` (Agent antwortet nicht binnen 15 s), `viewer_name`, `viewer_unavailable`, `viewer_error`.
- `{"t":"bye","reason":"…"}` von titand: `idle` (Trennzeit der Konsolen ohne Eingabe), `logout` (Abmeldung, Sperre, Passwortwechsel, Konto gesperrt/gelöscht), `forbidden` (Recht entzogen, Rolle geändert), `replaced` (dasselbe Konto hat die App in einem anderen Fenster geöffnet; das neue Fenster übernimmt denselben Container), `stopped` (Container beendet), `error` (unzulässige Nachricht vom Browser).

Richtung Agent lässt titand nur durch: Text-JSON ≤ 6 MiB (`proto.MaxTextMessage`; 1 MiB Zwischenablage kann maskiert bis zum Sechsfachen wachsen) mit `t` aus `size, mouse, wheel, key, text, clipboard, keyframe, quality, upload`; binär nur Typ `0x03` mit 4 Byte Upload-Nummer und 1 Byte bis 256 KiB Daten. Kein JSON, unbekanntes `t`, zu groß oder anderer Binärtyp beendet die Ansicht (`bye` `error`). Bekannte Nachrichten mit Werten außerhalb der Grenzen (`proto.ParseClient`) verwirft titand still, wie es der Agent täte. Als Eingabe (gegen die Trennzeit) zählen `mouse, wheel, key, text, clipboard, upload` und `0x03`. Richtung Browser geht alles unverändert durch (Lesegrenze 16 MiB je Nachricht).

### Container und Umgebung

- Ordner: `<volume>/.titanos/viewer/`, `…/image/`, `…/profiles/` und `…/profiles/<konto>/` gehören root mit 0700 (Docker bindet als root ein, der Container sieht nur `/profile`). Ausnahme von der Regel „private Betriebsdaten gehören root“ (`docs/rules/STORAGE.md`): der Profilordner `…/profiles/<konto>/<app>` gehört UID 1000 mit 0700, weil Chromium im Container dort schreibt. titand legt jeden Ordner ohne Verfolgen von Links an (`os.Root`) und lehnt fremde Besitzer oder für andere beschreibbare Ordner ab.
- Name `titanos-viewer-<16 Hex>` (SHA-256 von Konto und App, damit sich `a-b`/`c` und `a`/`b-c` nicht treffen); Labels `titanos.viewer=1`, `titanos.viewer.user`, `titanos.viewer.app`. Erstellt immer aus der Image-ID (`sha256:…`), nie aus einem Tag.
- Umgebung für den Agenten: `VIEWER_URL` (z. B. `http://172.30.0.2:3001/photos`: Adresse des App-Containers im Netz der Ansicht, **Container-Port** hinter dem veröffentlichten Port des HTTPS-Namens bzw. des Labels `titanos.webui`, Pfad aus dem Label, Schema des HTTPS-Namens), `VIEWER_KEY` (32 Zufallsbytes hex, nur im RAM), `VIEWER_GPU` (`vaapi|nvenc|cpu`), `VIEWER_AUDIO` (`1|0`), `VIEWER_QUALITY`, `VIEWER_FPS`, `VIEWER_TRANSPORT` (seit 0.68), `VIEWER_GPU_DEVICE` (Render-Knoten, nur bei VA-API), `HOME=/profile`.
- Netz: `titanos-viewer-<16 hex>` (gleicher Name wie der Container), `Internal`, Labels wie der Container, Bridge-Name `tvw<12 hex>` (`com.docker.network.bridge.name`). Der Web-Container der App wird mit dem Alias `titanos-viewer-attached` angehängt; titand nimmt nur Endpunkte mit diesem Alias wieder ab. Vor dem Verbinden prüft titand den Container per ID: läuft er nicht mehr oder hat er im Netz der Ansicht eine andere Adresse, wird die Ansicht beendet und einmal neu gestartet.
- Host-Regeln: eigene nft-Tabelle `inet titanos_viewer` (unabhängig davon, ob die Firewall von TitanOS an ist; vor jedem Start neu geladen, nicht im Entwicklungsmodus): im Eingang des Hosts von `iifname "tvw*"` nur Antworten (`ct state established,related`), alles Neue wird verworfen. Ein Browser in einer Ansicht erreicht so über das Gateway seines Netzes keinen Dienst des NAS; titand selbst verbindet sich von außen zum Agenten, dessen Antworten kommen als „established“ zurück.
- Richtlinie: `/run/titanos/viewer/<container>/titanos.json` (Ordner root 0755, Datei 0444, im RAM), nur lesbar eingebunden als `/etc/chromium/policies/managed/titanos.json` (`proto.PolicyPath`; der Ordner im Abbild gehört root und enthält sonst nichts). Wird mit der Ansicht gelöscht; beim Start von titand der ganze Ordner `/run/titanos/viewer`.
- titand wartet, bis `http://<viewer-ip>:7300/healthz` (ohne Schlüssel) antwortet, und verbindet sich dann mit `ws://<viewer-ip>:7300/stream`; Downloads holt es von `http://<viewer-ip>:7300/downloads/<id>` (der Agent nennt den Dateinamen in `Content-Disposition`). Der Sitzungsschlüssel geht immer im Kopf `X-Viewer-Key`, nie in der Adresse (insgesamt höchstens 15 s bis zur Verbindung). `hello` (mit `codec` aus dem Strom, z. B. `avc1.42C020`, und `scale`) und alle anderen Nachrichten des Agenten reicht titand unverändert durch.
- Ende: Fenster zu → 2 Minuten Nachlauf, dann Container stoppen (10 s Zeit, damit Chromium Cookies und Profil schreibt; ebenso bei Abmelden, Sperre, Rechtsentzug und beim Aufräumen nach einem Neustart von titand) und entfernen, App vom Netz der Ansicht nehmen, Netz und Richtlinie löschen. Trennzeit, Abmelden, Sperre, Rechtsentzug → sofort. Ebenso sofort: Löschen des Kontos (dazu werden seine Profile unter `.titanos/viewer/profiles/<konto>` ohne Verfolgen von Links gelöscht), Entfernen der App, Entzug des App-Rechts unter Programme (auch für Ansichten, die gerade auf ihr Fenster warten). Beim Start von titand (sobald Docker antwortet) werden übrig gebliebene Viewer-Container und Viewer-Netze entfernt (nur mit Label `titanos.viewer=1` **und** Namensanfang `titanos-viewer-`), angehängte Apps vorher abgenommen.
- Eigene Objekte: Container und Netze nur mit Label `titanos.viewer=1` und Namensanfang `titanos-viewer-`; Abbilder nur mit `titanos.viewer.image=1` und `titanos.viewer.release`. Alle Docker-Routen für Apps (Start/Stopp, Entfernen, Logs, Konsole) und damit auch MCP und Agent lehnen Viewer-Container ab (Label oder Name). Compose-Dateien mit Netzen namens `titanos-viewer…` (auch extern oder als `network_mode`) lehnt titand ab.

### Grenzen je Ansicht

- Speicher: jede Ansicht hat 1 GiB Limit (gemessen 230–400 MB in Gebrauch). Für jede offene, wartende oder startende Ansicht ist dieses Limit reserviert (was sie noch nicht nutzt, zählt zusätzlich zu MemAvailable). Eine neue Ansicht startet nur, wenn danach noch 700 MB frei bleiben, sonst `viewer_memory`.
- Höchstzahl gesamt aus dem RAM: (MemTotal − 2 GiB) / 1 GiB, mindestens 1 (`viewer_memory`); je Konto 4 (`viewer_limit`; dasselbe Programm in einem zweiten Fenster zählt nicht).
- CPU: höchstens 2 CPUs je Ansicht (`NanoCpus`), Gewicht 512 statt 1024 (`CpuShares`), damit Apps und VMs vorgehen. Starts laufen nacheinander.

### Aktivitätsprotokoll

- Aktivitätsprotokoll: `viewer_start` (App, bei Fehler mit Code) und `viewer_end` (App, Grund); keine Schlüssel.

### Abbild

1. `GET …/releases/download/v<titand-Version>/manifest.json` + `.minisig`, geprüft mit den Schlüsseln wie die NVIDIA-Bauumgebung; Version und Tag müssen passen. Die eigene titand-Version, nicht die Basis: ein App-Release bringt sein eigenes Abbild.
2. Eintrag `viewer`: `asset` = `titanos-viewer-<version>.tar.zst`, `sha256`, `size` ≤ 2 GiB, `imageId` = `sha256:<64 hex>` (Config-Digest).
3. Gibt es die Image-ID schon in Docker: fertig. Sonst Download nach `<volume>/.titanos/viewer/image/<asset>.part` (fortsetzbar, Größe und SHA-256 geprüft), umbenennen, `docker load` (zstd erkennt Docker 26 selbst, geprüft mit Debian-trixie-Docker 26.1.5), danach muss `docker image inspect <imageId>` genau diese ID liefern. Das Archiv wird danach gelöscht; Archive anderer Versionen ebenso. Danach entfernt titand ältere Abbilder mit dem Label `titanos.viewer.image=1` (aus `viewer/Dockerfile`), nie mit Gewalt: eines, das ein Container noch nutzt, bleibt.
4. Entwicklungsmodus (`titand -dev`): nur das lokale Abbild `titanos-viewer:dev` (aus `scripts/build-viewer-image.sh`), kein Download; Profile in einem temporären Ordner, nie auf einem Daten-Volume.

### Grenzen (Stand Teil B)

- Apps im Host-Netz bzw. mit `network_mode: container:/service:` gehen nicht (`viewer_hostnet`).
- Wird der App-Container während einer Ansicht neu erstellt (Update, Bearbeiten), hängt der neue nicht am Netz; die Ansicht muss neu geöffnet werden.
- HTTPS-Apps mit eigenem Zertifikat: Chromium sieht ein fremdes Zertifikat für die interne Adresse (Sache des Agenten/Abbilds).
- `imageId` setzt den klassischen Image-Speicher von Docker voraus (Debian trixie); mit dem containerd-Speicher wäre die ID eine andere.

