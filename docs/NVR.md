# NVR – Kameras, Aufnahme und Erkennung (PLAN 79-7)

Stand: **Backend-Kern** (Phase 1 des Bauplans in PLAN 79-7). Ereignisse, Aufnahmen/Zeitleiste, Export, PTZ-Befehle, Gesichter/Kennzeichen-Verwaltung, die Speichergrenze, die Meldungswege je Kamera, die Aufnahme in die Konfigurationssicherung und die gesamte Oberfläche folgen (Abschnitt „Was noch fehlt“). Ab Werk aus.

Der NVR nimmt Netzwerkkameras (RTSP, ONVIF) auf und erkennt Personen, Fahrzeuge, Tiere, auf Wunsch Kennzeichen und Gesichter. Die Arbeit macht **Frigate** (MIT-Lizenz) in einem Container, den TitanOS verwaltet und nirgends als App zeigt. Der Benutzer sieht nur TitanOS: Er schreibt keine Konfigurationsdatei und öffnet nie Frigates Oberfläche.

## Aufbau

```
Browser ──HTTPS/WebSocket, Anmeldung, Kamerarecht──▶ titand ──HTTP/WebSocket, Port 5000, nur Container-Netz──▶ Frigate (Container titanos-nvr)
                                                        │                                                        ├─ go2rtc: holt die Kameras ab (ein Abruf je Strom), verteilt sie
 /config/nvr/*.json (Stick) ─▶ nvr.Render ─▶ config.yml ┘                                                        ├─ ffmpeg: Aufnahme (Hauptstrom), Erkennung (Nebenstrom)
                                                                                                                 └─ Detektor: NPU, iGPU, Coral, Hailo oder CPU
```

- **Paket `backend/internal/nvr`**: Einstellungen und Kameras (`model.go`), Erzeugung von Frigates `config.yml` (`render.go`), Container und Abbild (`manager.go`, `watch.go`), Zustand und Diagnose (`status.go`), Geräte des NAS (`hardware.go`), Ursachen aus dem Protokoll (`classify.go`), Kamerasuche und -prüfung (`discover.go`, `onvif.go`, `rtsp.go`, `probe.go`, `target.go`), Kamerarechte (`access.go`), Entwicklungsmodus (`dev.go`).
- **`backend/internal/docker/nvr.go`**: die einzigen Aufrufe an Docker. Jeder Aufruf, der etwas ändert, prüft Label `titanos.nvr=1` **und** den Namen `titanos-nvr`; titand fasst nie einen anderen Container an. Der Container ist keine App: Er fehlt in der Docker-Übersicht, in den automatischen Updates, im Startplan und in den Sicherungen, und die Docker-Routen für Apps (und damit MCP/Agent) lehnen ihn ab.
- **`backend/internal/api/nvr.go`, `nvr_live.go`**: die Routen. Keine davon steht in `publicRoutes` oder im Werkzeugkatalog (kein Token, kein MCP, kein Agent).

### Frigate-Version

Eine feste, geprüfte Version je TitanOS-Release: **0.18.2**, Abbild `ghcr.io/blakeblackshear/frigate`, festgenagelt über den Digest des amd64-Abbilds (`nvr.FrigateVersion`, `nvr.FrigateDigest` in `nvr/manager.go` – beide zusammen ändern, dazu `frigateConfigVersion` in `render.go`). Das Abbild (etwa 1,5 GB zu laden, 6,3 GB entpackt) wird beim ersten Einschalten über Docker geladen; der Zustand zeigt den Fortschritt. Edge-TPU- und Hailo-Laufzeit sind im Standard-Abbild enthalten. Ein anderes Abbild als das festgenagelte startet TitanOS nicht; nach einem TitanOS-Update mit neuem Digest wird der Container ersetzt.

### Container

| Punkt | Wert |
|---|---|
| Name, Netz | `titanos-nvr`, eigenes Bridge-Netz `titanos-nvr` (nicht „internal“: Frigate erreicht die Kameras im Heimnetz darüber) |
| Rechte | nicht privilegiert, `no-new-privileges` |
| Daten | `<Volume>/.titanos/nvr/config` → `/config` (Datenbank, Modelle, `config.yml`), `<Volume>/.titanos/nvr/media` → `/media/frigate` (Aufnahmen, Schnappschüsse, Exporte), `/etc/localtime` nur lesend |
| tmpfs | `/tmp/cache` 1 GB (Segmente, bevor sie aufs Volume wandern) |
| `/dev/shm` | 128 MB + 64 MB je eingeschalteter Kamera, mindestens 256 MB. Frigate hält dort dekodierte Bilder des Erkennungsstroms (etwa 1,6 MB je Bild bei 1280×720, bestenfalls 50 je Kamera) plus rund 50 MB für Protokolle; das reicht je Kamera für mindestens 30 Bilder bei 720p (Frigate warnt unter 20). Ein Nebenstrom über 720p braucht mehr – die Diagnose zeigt die Belegung. |
| Geräte | Render-Knoten der Intel-/AMD-GPUs (`/dev/dri/renderD*`, Videodekodierung und iGPU-Erkennung), `/dev/accel/accel*` nur bei NPU, `/dev/bus/usb` mit Geräteregel `c 189:* rmw` nur bei Coral, `/dev/hailo0` nur bei Hailo. Eine GPU, die an eine VM vergeben ist, bekommt der Container nicht. |
| Veröffentlichte Ports | **8555 tcp+udp** (WebRTC, fürs Gegensprechen) immer; **8554 tcp** (RTSP-Weitergabe für Home Assistant) nur mit `rtspOut`, dann mit Anmeldung (`titanos` + erzeugtes Passwort, nur Administratoren sehen es). **Nie** 5000 (API ohne Anmeldung), 8971, 1984 (go2rtc). |
| Neustart | Dockers Neustart-Regel ist `no`. titand startet Frigate selbst neu und zählt dabei mit; Dockers eigene Regel würde eine Absturzschleife vor titand verbergen. |
| Stoppen | 30 Sekunden (Frigate schließt Datenbank und laufende Segmente). |

titand erreicht Frigate über die Adresse des Containers im Netz `titanos-nvr`, Port 5000. Port 5000 fragt nie nach einer Anmeldung; deshalb wird er nicht veröffentlicht. Frigates zweiter Port 8971 (TLS, Anmeldung) ist in `config.yml` auf `127.0.0.1` im Container gebunden (`networking.listen.external`), also von nirgends erreichbar; `auth` und `tls` sind aus.

### Daten

| Was | Wo | Geschrieben |
|---|---|---|
| Einstellungen | `/config/nvr/settings.json` (Stick) | nur bei Änderung durch den Besitzer |
| Kameras samt Passwörtern | `/config/nvr/cameras.json` (Stick) | nur bei Änderung |
| Kamerarechte | `/config/nvr/camera-access.json` (Stick) | nur bei Änderung |
| Zustand (aktives Erkennungsgerät, letztes Ausweichen, „angehalten“) | `<Volume>/.titanos/nvr/state.json` | bei Ausweichen/Anhalten – **nicht** auf dem Stick, weil es sich ohne Zutun des Besitzers ändert |
| `config.yml` für Frigate | `<Volume>/.titanos/nvr/config/config.yml`, Modus 0600 | nur wenn sich der Inhalt ändert |
| Frigates Datenbank, Modelle, Aufnahmen | `<Volume>/.titanos/nvr/config`, `…/media` | von Frigate |

`.titanos/nvr` gehört root, ist nicht von anderen beschreibbar und nicht verlinkt (`volumedata.Open`). Ein Volume, das nicht eingebunden ist, ist nie ein Ersatzordner im RAM: Der NVR steht dann still (`no_volume`). Ältere TitanOS-Versionen ignorieren die drei Dateien auf dem Stick.

**Passwörter:** Kamerapasswörter stehen wie andere Geheimnisse (Backup-Ziele, DDNS) in ihrer JSON-Datei auf dem Stick; der Ordner `nvr` ist im Stick-Dateimanager nicht sichtbar und nicht im Diagnosepaket. Die API gibt nie ein Passwort zurück (`hasPassword`); ein leeres Passwort beim Ändern behält das gespeicherte, aber nur solange Adresse und Benutzer dieselben bleiben – ein Passwort wandert nie durch eine Bearbeitung an eine andere Adresse. `config.yml` enthält die Passwörter (Frigate braucht sie) und liegt deshalb 0600 im privaten Ordner; Protokollzeilen, die TitanOS anzeigt oder speichert, laufen durch `nvr.Redact`.

**Schlafende Platten:** Aufnahmen halten die Platten des NVR-Volumes dauerhaft wach. Ein Volume, dessen Platten schlafen dürfen, nimmt der NVR nur nach ausdrücklicher Bestätigung (`nvr_volume_sleeps`).

## Einstellungen und Kameras

```jsonc
// GET/PUT /api/v1/nvr/settings (Administratoren)
{ "enabled": false, "volume": "tank",
  "detector": { "kind": "auto" },            // auto | npu | igpu | coral | hailo | cpu
  "storage": { "limitGB": 0 },               // 0 = keine Grenze von TitanOS
  "privacyNoticeDismissed": false,
  "rtspOut": false, "rtspUser": "", "rtspPassword": "" }   // die letzten beiden nur lesen
```

```jsonc
// Kamera (GET/POST/PUT /api/v1/nvr/cameras[/{id}], Administratoren)
{ "id": "haustuer",                          // fest, [a-z][a-z0-9_]{0,31}; leer beim Anlegen: aus dem Namen
  "name": "Haustür", "enabled": true, "order": 1,
  "source": { "kind": "rtsp",                // rtsp | onvif
              "host": "192.168.1.61", "port": 554, "onvifPort": 0,
              "user": "viewer", "hasPassword": true,       // beim Schreiben: "password"; "clearPassword": true löscht es
              "mainURL": "rtsp://192.168.1.61:554/…", "subURL": "rtsp://…",   // ohne Zugangsdaten
              "allowPublic": false },
  "detect": { "enabled": true, "objects": ["person", "car"], "plates": false, "faces": false },
  "record": { "mode": "events", "retainDays": 14, "preSeconds": 5, "postSeconds": 5 },   // off | events | motion | continuous
  "audio": { "enabled": false, "twoWay": false },
  "ptz": false,
  "notify": { "titan": true, "push": false, "email": false, "homeAssistant": false, "agent": false, "kinds": ["person", "offline"] },
  "state": { "online": true, "known": true, "fps": 15, "detectionFps": 1.2, "offlineSince": null } }
```

Erlaubte Objekte: `person, bicycle, car, motorcycle, bus, bird, cat, dog, horse, sheep, cow, bear` (`GET …/cameras` liefert die Liste als `objects` mit). Keine feste Obergrenze unter 64 Kameras; gebaut und geprüft für 4.

Geprüft wird streng (`nvr.ValidateCamera`): nur `rtsp://`/`rtsps://`, keine Zugangsdaten und kein `#…` in der Adresse (go2rtc liest `#` als eigene Optionen), keine geschweiften Klammern (Frigate wendet `str.format()` auf jede Stromadresse an), nie dieses NAS (`127.0.0.1`, `localhost`). Eine eingefügte Adresse `rtsp://user:pass@…` zerlegt die API in Adresse und Zugangsdaten. Eine IP-Adresse außerhalb des Heimnetzes (privat, Tailscale/NetBird) wird nur mit `allowPublic` angenommen (`nvr_target_public`).

### Was daraus in `config.yml` wird (`nvr.Render`)

Reine Funktion, gleiche Eingabe → gleiche Bytes (Kameras nach `order`, dann `id`; feste Reihenfolge aller Schlüssel). Golden-Dateien: `backend/internal/nvr/testdata/{cpu,npu,igpu,coral,hailo}.yml`.

- Fest: `version: 0.18-0` (sonst hält Frigate die Datei für eine von 0.13, „migriert“ sie und schreibt sie zurück), `mqtt.enabled: false`, `auth.enabled: false`, `tls.enabled: false`, `networking.listen.external: 127.0.0.1:8971`, `telemetry.version_check: false`, `birdseye.enabled: false`.
- `detectors`/`model` je Gerät wie in Frigates Doku: OpenVINO mit `device: NPU`/`GPU`/`CPU` und dem mitgelieferten SSDLite MobileNet v2 (300×300, COCO); Coral `edgetpu`/`usb` (Modell des Abbilds); Hailo `hailo8l`/`PCIe` (YOLOv6n, lädt Frigate beim ersten Start).
- `ffmpeg.hwaccel_args`: `preset-vaapi`, wenn ein Render-Knoten durchgereicht ist (findet den Codec selbst; die QSV-Voreinstellungen sind je Codec), sonst ausdrücklich leer (Frigates Vorgabe `auto` würde selbst suchen).
- `go2rtc.streams`: je Kamera `<id>` (Hauptstrom) und `<id>_sub` (Nebenstrom, falls vorhanden), Zugangsdaten URL-kodiert, mit `#backchannel=0`; ohne Ton zusätzlich `#media=video`. Mit Gegensprechen ein dritter Strom `<id>_twoway` ohne jede Option (nur so öffnet go2rtc den Rückkanal) und ein Opus-Ableger für WebRTC. `go2rtc.webrtc.candidates` sind die Heimnetz-Adressen des NAS mit Port 8555 – immer gesetzt, sonst trägt Frigate einen öffentlichen STUN-Server ein.
- `cameras.<id>`: Eingänge über die lokale Weitergabe (`rtsp://127.0.0.1:8554/<id>`, `preset-rtsp-restream`); Hauptstrom `record`, Nebenstrom `detect` (ohne Nebenstrom beides auf dem Hauptstrom); `detect.fps: 5`; `objects.track` (Kennzeichen ziehen `car`, Gesichter `person` nach); Schnappschüsse an, solange erkannt wird; Frigates Geräuscherkennung aus (`audio.enabled` der Kamera meint den Ton zum Bild: `preset-record-generic-audio-copy` statt `preset-record-generic`); `onvif` nur bei PTZ.
- Aufnahme: `continuous` → alles für `retainDays`; `motion` → nur Segmente mit Bewegung; `events` → nur Segmente mit aktivem erkanntem Objekt (`active_objects`). Alarme und Erkennungen bekommen dieselben Tage und die Sekunden davor/danach.
- `lpr.enabled`/`face_recognition.enabled` global genau dann, wenn eine eingeschaltete Kamera es will, dazu je Kamera.

## Ablauf

`nvr.Manager.Run` sieht alle 10 Sekunden nach (Dockers Socket und Frigates API im Container-Netz: keine Platte, nie `/config`) und sofort, wenn eine Anfrage etwas geändert hat. Anfragen ändern nur Einstellungen; den Container fasst allein diese Schleife an.

| Zustand (`state`) | Bedeutung |
|---|---|
| `off` | ausgeschaltet; kein Container |
| `docker_off` / `docker_starting` | Docker ist aus / an, aber noch nicht oben |
| `no_volume` | das NVR-Volume ist nicht eingebunden; Container entfernt |
| `pulling` | das Abbild wird geladen (`image.bytes`/`size`/`progress`) |
| `starting` | Container läuft, Frigate antwortet noch nicht |
| `running` | Frigate antwortet, die Konfiguration ist angenommen, der Detektor lebt |
| `stopped` | nichts hat funktioniert; wartet auf den Besitzer (`error.code`) |
| `error` | Docker hat etwas abgelehnt; nächster Versuch nach einer Minute |

- **Einschalten** (`POST /nvr/enable`): prüft Volume und Docker. Ist Docker aus, antwortet die Route `nvr_docker_off`; die Oberfläche fragt „Docker einschalten?“ und schickt dieselbe Anfrage mit `enableDocker: true` – dann schaltet der NVR Docker über denselben Schalter wie die Docker-Seite ein (Docker behält sein Volume, sonst nimmt es das des NVR). Danach: Abbild laden, `config.yml` schreiben, Netz und Container anlegen, starten, auf `GET /api/version` warten.
- **Ausschalten**: Container stoppen und entfernen. Einstellungen, Kameras, Datenbank und Aufnahmen bleiben.
- **Änderungen** (Einstellungen, Kameras): `config.yml` wird nur geschrieben, wenn sich ihr Inhalt ändert; dann startet Frigate im selben Container neu (Lücke von grob einer Minute). Ändert sich der Container selbst (Geräte, `/dev/shm` wegen der Kamerazahl, Port 8554), wird er ersetzt. Dieselbe Eingabe noch einmal ändert nichts und startet nichts neu.
- **App-Update von TitanOS**: Frigate läuft weiter; der neue titand übernimmt den Container.
- **Neustart/Herunterfahren des NAS**: Docker beendet beim Stoppen alle Container, Frigate bekommt seine 30 Sekunden. Nach dem Start bringt titand den Container wieder hoch (das zählt nicht als Absturz).
- **Volume aushängen/sperren**: Ein Volume, das ausgehängt oder gesperrt wird, zählt sofort nicht mehr als eingebunden; der NVR entfernt seinen Container beim nächsten Nachsehen (wie Docker es für sein eigenes Volume tut).

### Gesund oder nicht

Ein Start gilt als gelungen, wenn (1) `GET /api/version` antwortet, (2) `GET /api/config` **nicht** `safe_mode: true` meldet – eine abgelehnte `config.yml` beendet Frigate nicht, es startet dann leer und antwortet, als wäre alles gut – und (3) `GET /api/stats` für jeden Detektor einen Prozess nennt.

### Ausweichen des Erkennungsgeräts

Frigate kennt kein Ausweichen: Stirbt der Detektor, beendet es sich. TitanOS erkennt das und stellt um:

- **Auslöser:** 3 Enden des Containers innerhalb von 5 Minuten; oder 5 Minuten nach dem Start noch keine Antwort; oder dreimal nacheinander (30 s) kein Detektorprozess in `/api/stats`. (Ein Frigate, das lief und 5 Minuten nicht mehr antwortet, wird neu gestartet; das zählt wie ein Ende.)
- **Ursache:** aus dem Protokoll des Containers seit Beginn der Störung (`nvr.Classify`):

  | Code | Erkannt an |
  |---|---|
  | `npu_firmware_old` | `MAPPED_INFERENCE_VERSION is NOT compatible` – die NPU-Firmware des NAS ist älter als der Treiber in Frigates Abbild |
  | `npu_turbo_unsupported` | `Unsupported configuration key: NPU_TURBO` – bekannter Fehler von Frigate 0.18 auf Arrow Lake (Core Ultra 200), behoben erst im Zweig 0.19 |
  | `npu_missing` | kein `/dev/accel` bzw. OpenVINO kennt kein Gerät „NPU“ |
  | `coral_missing` | `No EdgeTPU was detected` |
  | `not_healthy` | Frigate hat nach dem Start nie geantwortet |
  | `detector_unknown` | etwas anderes; die letzte Fehlerzeile steht im Detail |
  | `config_invalid` | Frigate hat `config.yml` abgelehnt – **kein** Ausweichen (das Gerät ist nicht schuld), der NVR hält an |

- **Kette:** `auto` nimmt das erste verfügbare Gerät in der Reihenfolge **Coral, Hailo, NPU, iGPU, CPU** (wer einen Stick hat, hat ihn dafür gekauft; ohne Stick also NPU → iGPU → CPU). Beim Ausfall geht es in dieser Reihenfolge weiter, nie zurück: NPU → iGPU → CPU; Coral/Hailo → NPU → iGPU → CPU (die beiden Sticks vertreten einander nicht, außer bei `auto`); eine gewählte iGPU fällt nur auf die CPU. Ein ausdrücklich gewähltes Gerät wird auch versucht, wenn es gerade nicht verfügbar scheint – so sieht der Besitzer in der Diagnose, warum es nicht geht.
- **Umstellen:** `state.json` bekommt `active` und `failover {from, to, at, reason, detail}`, `config.yml` wird fürs nächste Gerät geschrieben, der Container ersetzt (andere Geräte), Meldung `nvr_failover` („NPU → iGPU“).
- **Ende der Kette** (auch die CPU geht nicht) oder `config_invalid`: Container weg, Zustand `stopped`, Meldung `nvr_stopped`. Kein Dauerversuch.
- **Zurück:** `POST /nvr/detector/retry` versucht wieder das gewählte Gerät; ebenso ein Neustart des NAS (nicht der von titand allein), das Wählen eines anderen Geräts oder Volumes und Aus-/Einschalten. Von selbst wechselt der NVR nie zurück – kein Hin und Her.

### Weitere Meldungen

`nvr_camera_offline`: eine eingeschaltete Kamera liefert seit 5 Minuten kein Bild (einmal, bis sie wieder liefert; nur wenn die Kamera „TitanOS-Meldung“ und die Art `offline` gewählt hat). `nvr_storage_cap` (älteste Aufnahmen gelöscht) hat seine Texte schon, wird aber erst von der Speichergrenze in Phase 2 ausgelöst. Texte deutsch/englisch in `notify/texts.go` und `web/src/lib/i18n/{de,en}.json` (`notify.kind.nvr_*`).

## Kameras finden und prüfen

- **Suche** (`POST /nvr/cameras/discover`, nur auf Anforderung eines Administrators, nie von selbst): ein WS-Discovery-Probe (UDP-Multicast 239.255.255.250:3702, Typ `NetworkVideoTransmitter`) auf jeder Schnittstelle mit Heimnetz-Adresse, dann 3 Sekunden lauschen. Kein Passwort wird gesendet. Als Adresse einer Kamera gilt, woher die Antwort kam – was das Gerät in seine Antwort schreibt, liefert nur Port und Texte. Antworten von außerhalb des Heimnetzes werden verworfen.
- **Prüfen** (`POST /nvr/cameras/probe`): `kind: "onvif"` fragt Gerät, Profile und Stromadressen ab (WS-Security mit Passwort-Digest, die Uhr der Kamera wird vorher gelesen) und prüft dann Haupt- und Nebenstrom; `kind: "rtsp"` prüft die angegebene Adresse per RTSP `DESCRIBE` (Digest oder Basic; Codec und, wo die Kamera es nennt, Bildgröße aus dem SDP). Zugangsdaten stehen nie in der Anfragezeile. Ein falsches Passwort kostet genau einen Versuch an der Kamera (Kameras sperren Konten); das Ergebnis heißt dann `outcome: "unauthorized"` bzw. `"locked"`, kein Fehler der Anfrage. Stromadressen aus ONVIF werden auf die Adresse umgeschrieben, unter der die Kamera erreicht wurde, und ohne Zugangsdaten zurückgegeben.
- **Ziele:** Jede Verbindung wird im Moment des Verbindens geprüft (auch nach DNS-Auflösung): nur private Adressen und Tailscale/NetBird, nie dieses NAS, link-local oder Multicast; mit `allowPublic` (Bestätigung des Administrators) auch öffentliche Adressen, weiterhin nie dieses NAS.
- Frigates eigene Prüfung (`/api/ffprobe`) wird bewusst nicht benutzt: Sie nimmt die Adresse samt Passwort als Abfrageparameter, den Frigates Webserver protokolliert.

## Rechte

Wie bei den Apps (`appaccess`): Besitzer und Administratoren sehen immer alle Kameras. Ein normales Konto sieht eine Kamera nur mit Haken, getrennt für **Live ansehen** (`live`) und **Aufnahmen ansehen** (`recordings`). Neue Konten und neue Kameras: kein Haken. Geprüft wird serverseitig bei jeder Anfrage gegen das Konto, wie es jetzt ist (gesperrt → nichts).

- `GET/PUT /api/v1/users/{name}/cameras` (Administratoren) – neben `…/apps` für den Reiter „Kameras“ unter Konten und Zugang › Nutzer: `{admin, cameras: [{id, name, live, recordings}]}` bzw. `{cameras: {"<id>": {live, recordings}}}`.
- Löschen einer Kamera oder eines Kontos entfernt die Haken (ein neues Konto gleichen Namens fängt bei null an).
- Für normale Konten: `GET /nvr/cameras` und `/nvr/cameras/{id}` liefern nur sichtbare Kameras und nur `{id, name, enabled, order, audio, ptz, state, rights}` – nichts über Adresse, Zugang, Erkennung oder Aufnahme; eine nicht sichtbare Kamera wird wie eine nicht vorhandene beantwortet. `GET /nvr/status` liefert nur `{enabled, state, cameras, privacyNoticeDismissed}` mit den Zahlen der eigenen Kameras; Gründe für einen Stillstand erscheinen als `unavailable`.

## Live-Bild

- `GET /api/v1/nvr/cameras/{id}/snapshot.jpg[?height=90…2160]` – das letzte Bild (Frigates `latest.jpg`), Recht `live`, `Cache-Control: no-store`.
- `GET /api/v1/nvr/cameras/{id}/live[?stream=sub]` – WebSocket. titand reicht go2rtcs MSE-Protokoll unverändert durch (`/live/mse/api/ws?src=<id>` bzw. `<id>_sub`): Textnachrichten für die Codecs, binäre mit dem Video. Herkunftsprüfung wie bei allen WebSockets; die Verbindung gehört zur Anmeldung und endet wie eine Konsole bei Abmelden, Sperre oder Entzug des Rechts (alle 3 s geprüft) mit `{"t":"bye","reason":"logout"|"forbidden"}`. Fehler kommen nach dem Aufbau als `{"t":"error","code":…}` (`nvr_camera_not_found`, `nvr_not_running`, `nvr_no_picture`, `nvr_unavailable`). Eine Trennzeit ohne Eingabe gibt es hier nicht (ein Live-Bild hat keine Eingabe).

## Diagnose

`GET /api/v1/nvr/diagnosis` (Administratoren) – alles, um ohne Terminal zu sehen, warum die Erkennung nicht läuft: Zustand; Container (`running`/`exited`/`missing`, Exit-Code, Zeiten, Enden im Fenster); Abbild (Referenz, Version, vorhanden); was Frigate über sich sagt; gewähltes und aktives Gerät, Zeit je Bild, Prozess des Detektors, letztes Ausweichen; letzter Fehler mit Code und Protokollzeile; die letzten 200 Protokollzeilen (von Frigate, sonst vom Container; Passwörter entfernt); ob `/dev/accel` da ist; die Kernel-Zeilen des NPU-Treibers `intel_vpu` (mit dem Baudatum der Firmware); GPU-/NPU-Auslastung, wie Frigate sie nennt; je Kamera Bilder je Sekunde und Verbindung; Belegung von `/dev/shm`; die Geräte des NAS (`hardware`). Kein Passwort.

`hardware` (auch im Zustand): `devices` (nur vorhandene Geräte, mit `available` und `reason`: `driver`, `vm`, `plugin` – ein Coral-Stick ohne das Plugin „Coral USB“), `auto`, `videoDecode`, `accelDevice`, `avx2`, `memoryBytes`, `enrichmentsPossible`/`enrichmentsReason` (Kennzeichen und Gesichter brauchen AVX2 und 4 GB RAM).

## Routen

| Route | Wer | Zweck |
|---|---|---|
| `GET /api/v1/nvr/status` | angemeldet | Zustand (für normale Konten gekürzt) |
| `GET`, `PUT /api/v1/nvr/settings` | Administrator | Einstellungen; `PUT` nimmt zusätzlich `enableDocker`, `confirmSleepingVolume` |
| `POST /api/v1/nvr/enable`, `…/disable` | Administrator | ein/aus; `enable` nimmt `{volume, enableDocker, confirmSleepingVolume}` |
| `GET /api/v1/nvr/diagnosis` | Administrator | Diagnose |
| `POST /api/v1/nvr/detector/retry` | Administrator | zurück zum gewählten Erkennungsgerät |
| `GET /api/v1/nvr/cameras`, `GET …/{id}` | angemeldet | Kameras (gefiltert) |
| `POST /api/v1/nvr/cameras`, `PUT`/`DELETE …/{id}` | Administrator | anlegen, ändern, löschen |
| `POST /api/v1/nvr/cameras/discover`, `…/probe` | Administrator | Suche, Prüfung |
| `GET /api/v1/nvr/cameras/{id}/snapshot.jpg` | Recht `live` | Standbild |
| `GET /api/v1/nvr/cameras/{id}/live` | Recht `live` | Live (WebSocket) |
| `GET`, `PUT /api/v1/users/{name}/cameras` | Administrator | Kamerarechte eines Kontos |

Fehlercodes (Texte in `web/src/lib/i18n/features/errors.{de,en}.json`): `nvr_unavailable`, `nvr_docker_off`, `nvr_no_volume`, `nvr_volume_sleeps`, `nvr_target_public`, `nvr_camera_not_found`, `nvr_camera_exists`, `nvr_camera_limit`, `nvr_not_running`, `nvr_disabled`, `nvr_invalid`, `nvr_access_admin`, `nvr_no_picture`.

## Entwicklungsmodus

`titand -dev` hat kein Docker. Der NVR läuft dort auf einem Ersatz (`nvr/dev.go`, nur dort benutzt): ein Motor, der sich einen Container merkt, und ein Ersatz für Frigates API auf einem Port von `127.0.0.1` (Version, Statistik, Protokoll, Standbilder als Farbverlauf je Kamera, ein Live-WebSocket ohne Video). Ohne Kameradatei gibt es vier Beispielkameras (nur im RAM; eine ausgeschaltet, eine „offline“), Geräte „NPU“ und „iGPU“, die Suche findet zwei Beispielkameras, die Prüfung gelingt außer mit dem Passwort `wrong`. Daten liegen in einem temporären Ordner, nie auf einem Volume.

## Prüfstand (12.11.2026) und Grenzen

**Geprüft**

- Go-Tests ohne Docker: Konfigurationserzeugung (fünf Golden-Dateien, Reihenfolge, Zugangsdaten, Klammern), Prüfregeln, Geräteerkennung und Ketten, Ursachen aus dem Protokoll, Schwärzen von Passwörtern, Kamerarechte, der Ablauf mit Ersatzmotor und Ersatz-Frigate (Einschalten, Ändern ohne unnötigen Neustart, Übernahme nach titand-Neustart, Ausweichen NPU → iGPU → CPU → angehalten, „Wieder versuchen“, Vergessen nach NAS-Neustart, abgelehnte Konfiguration, nie gesund, toter Detektor, hängendes Frigate, Abbild laden und Fehlschlag, Kamera offline, Volume weg), WS-Discovery (aufgezeichnete Antworten, Probe über Loopback-UDP), ONVIF (aufgezeichnete SOAP-Antworten), RTSP `DESCRIBE` mit Digest und Basic, Routen (Rollen, Filter, Rechte, Standbild, Live-Durchreichung mit Entzug des Rechts und Abmelden, fremde Herkunft).
- **Frigates eigener Prüfer:** Alle fünf Golden-Dateien wurden mit dem echten Abbild 0.18.2 geprüft (`python3 -m frigate --validate-config`): „Your config file is valid“, keine Migration. Dazu go2rtcs Konfigurationserzeugung und die nginx-Einstellungen aus derselben Datei (`listen.external: 127.0.0.1:8971`).
- **Echter Lauf** auf der Entwicklungsmaschine (Debian-VM, Docker 26.1, ohne GPU/NPU/Kamera): `TestRealFrigate` (`backend/internal/nvr/real_test.go`, läuft nur mit `TITANOS_NVR_REAL=<Ordner>`) führt den Manager gegen das echte Docker und das echte Abbild: Abbild über den Digest geladen und erkannt, Netz und Container angelegt, Frigate 0.18.2 mit CPU-Detektor nach etwa 5 Sekunden gesund (`/api/version`, kein `safe_mode`, Detektorprozess in `/api/stats`, 256 MB `/dev/shm`, Protokoll über `/api/logs/frigate`); danach eine ONVIF-Kamera mit PTZ, Ton, Gegensprechen und Sonderzeichen im Passwort hinzugefügt (Adresse ohne Gerät dahinter): Frigate startet im selben Container neu, nimmt die Konfiguration an und führt die Kamera als „offline“; Ausschalten entfernt den Container. Von Hand dazu: Mit der erzeugten Datei antwortet Port 5000 an der Container-Adresse, Port 8971 nimmt von außerhalb des Containers keine Verbindung an.

**Nicht geprüft**

- NPU, iGPU (OpenVINO `GPU`), Coral und Hailo auf echter Hardware; VA-API-Dekodierung im Container; das Ausweichen mit echten Fehlerbildern (die beiden NPU-Fehlertexte stammen aus Frigates Doku und einem Fehlerbericht, nicht aus einem eigenen Lauf).
- Echte Kameras: Bildfluss, Aufnahme, Ton, Gegensprechen, `#media=video`, PTZ; ONVIF und RTSP gegen echte Geräte verschiedener Hersteller; WS-Discovery in einem echten Netz (Multicast über Bridges/VLANs).
- Live-Bild im Browser (MSE über die Durchreichung) – die Durchreichung ist mit einem Echo-Server geprüft, nicht mit go2rtc.
- Lauf auf einem TitanOS-NAS (Rechte im privaten Ordner als root, Firewall von TitanOS mit veröffentlichtem Port 8555, Docker auf einem anderen Volume als der NVR, GPU-Übergabe an eine VM während der NVR läuft).
- Kennzeichen und Gesichter im Betrieb (Modell-Download beim ersten Start braucht Internet).

**Bekannte Grenzen**

- Übergibt jemand eine GPU an eine VM, während der NVR sie benutzt, merkt der NVR das erst beim nächsten Nachsehen (bis 10 s) und baut den Container dann ohne sie neu; die Übergabe wartet nicht auf den NVR.
- Das alte Frigate-Abbild bleibt nach einem Versionswechsel liegen, bis jemand unter Docker › Speicher aufräumt.
- Gesichter/Kennzeichen laufen mit Frigates Vorgabe (kleine Modelle auf der CPU); die iGPU dafür zu nutzen ist nicht eingestellt.
- Solange gar keine Erkennung läuft (NVR `stopped`), wird nicht aufgenommen; der im Plan genannte Ersatz „bei Bewegung aufnehmen, wenn die Erkennung fehlt“ ist nicht gebaut (die CPU als letztes Glied der Kette fängt den Fall praktisch ab).

## Was noch fehlt (Phase 2 und Oberfläche) – Anschlussstellen

- **Ereignisse, Aufnahmen, Export, PTZ, Gesichter/Kennzeichen:** neue Routen in `api/nvr_*.go`, die wie `handleSnapshot`/`handleLive` das Kamerarecht prüfen (`a.allows(user, id, nvr.RightRecordings)`) und Frigate über `nvr.Manager` ansprechen; dort neben `Snapshot`/`Live` weitere Methoden auf `frigate` (`nvr/frigate.go`). Für WebRTC/Gegensprechen ist der Strom `<id>_twoway` schon in go2rtc, die Durchreichung (`relayWebSockets`) lässt sich für `/live/webrtc/api/ws` wiederverwenden. PTZ-Bewegung geht bei Frigate nur über den WebSocket `/ws`.
- **Speichergrenze:** `settings.storage.limitGB` ist gespeichert und im Zustand, `frigate.recordingsUsage` liest die Belegung schon (alle 5 Minuten, `status.storage.usedBytes`). Der Auftrag, der bei Überschreitung die ältesten Aufnahmen über `DELETE /api/recordings/start/{s}/end/{e}` löscht und höchstens einmal am Tag `nvr_storage_cap` meldet, gehört in `nvr/watch.go` (`check`).
- **Meldungswege je Kamera** (`notify.push`, `email`, `homeAssistant`, `agent`, `kinds`): gespeichert und geprüft, ausgewertet wird bisher nur `titan` + `offline`.
- **Konfigurationssicherung:** `nvr/settings.json`, `nvr/cameras.json`, `nvr/camera-access.json` liegen in `/config` und laufen damit in der Sicherung des Sticks mit; bekannte Gesichter/Kennzeichen (in Frigates `/config` auf dem Volume) noch nicht. Diagnosepaket: `nvr.Manager.Diagnosis` ist die Quelle, noch nicht eingebunden.
- **Oberfläche:** Programm „NVR“, Einrichtungsassistent, Reiter „Kameras“ bei den Konten, Kamera-Widget. Glockengruppe für `nvr_*` in `web/src/lib/notify-groups.ts` (fällt bisher unter „System“).
- **Abbild von 0.78:** NPU-Firmware prüfen/anheben (mindestens Stand Juli 2025), `release-kind`.
