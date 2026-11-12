# NVR – Kameras, Aufnahme und Erkennung (PLAN 79-7)

Stand: **Backend vollständig** (Phasen 1 und 2 des Bauplans in PLAN 79-7): Kern, Ereignisse, Aufnahmen und Wiedergabe, Export, Schwenken, Gegensprechen, Gesichter und Kennzeichen, Speichergrenze, Meldungswege je Kamera, Sicherung. Die Oberfläche entsteht getrennt (Abschnitt „Was noch fehlt“). Ab Werk aus.

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

`nvr_camera_offline`, die Meldungen je Ereignis und `nvr_storage_cap` stehen in den Abschnitten „Meldungen je Kamera“ und „Speichergrenze“. Texte deutsch/englisch in `notify/texts.go`; die Oberfläche hat eigene Texte unter `notify.kind.nvr_*` (`web/src/lib/i18n/{de,en}.json`).

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

Dazu die Routen des Abschnitts „API Phase 2“ (Ereignisse, Feed, Aufnahmen, Wiedergabe, Export, PTZ, Gegensprechen, Gesichter, Kennzeichen).

Fehlercodes (Texte in `web/src/lib/i18n/features/errors.{de,en}.json`): `nvr_event_not_found`, `nvr_export_not_found`, `nvr_export_running`, `nvr_export_failed`, `nvr_ptz_unsupported`, `nvr_face_not_found`, `nvr_face_exists`, `nvr_face_rejected`, `nvr_faces_off`, `rate_limited`, `nvr_unavailable`, `nvr_docker_off`, `nvr_no_volume`, `nvr_volume_sleeps`, `nvr_target_public`, `nvr_camera_not_found`, `nvr_camera_exists`, `nvr_camera_limit`, `nvr_not_running`, `nvr_disabled`, `nvr_invalid`, `nvr_access_admin`, `nvr_no_picture`.

## API Phase 2

> **Vertrag für die Oberfläche.** Routen und Formen dieses Abschnitts gelten und sind gebaut. **Abweichungen vom ersten Stand dieses Abschnitts** (beim Bau entstanden): (1) die nicht zugeordneten Gesichter liegen unter `/face-attempts/{file}` statt `/faces/attempts/{file}` (eine Person könnte sonst „attempts“ heißen); (2) Personennamen ohne Leerzeichen; (3) `count`/`lastSeen` der gelesenen Kennzeichen stammen aus den neuesten 1000 Fahrzeug-Ereignissen; (4) Fehler von `…/talk` kommen als Schließgrund des WebSockets, nicht als `{"t":"error"}`; (5) im Entwicklungsmodus spielt jede Wiedergabe dasselbe 4-Sekunden-Testbild. `titand -dev` liefert für jede Route Beispieldaten (Abschnitt „Entwicklungsmodus“). Zeiten sind **Unix-Sekunden** (Zahl, UTC), `null` heißt „noch offen/unbekannt“. Alle Routen unter `/api/v1/nvr`; keine in `publicRoutes`. „Recht“ meint das Kamerarecht des Kontos; Besitzer/Administratoren haben beide. Eine Kamera oder ein Ereignis ohne Recht wird wie ein nicht vorhandenes beantwortet (`nvr_camera_not_found`, `nvr_event_not_found`). Läuft der NVR nicht: `409 nvr_not_running`.

### Ereignisse

| Route | Recht | |
|---|---|---|
| `GET /events?camera=&label=&from=&to=&limit=&before=&reviewed=` | `recordings` | Liste, neueste zuerst. `camera`/`label` mehrfach mit Komma; `limit` 1–200 (Vorgabe 50); `before` = `next` der vorigen Seite; `reviewed=0|1` filtert. Nur Kameras mit Recht. |
| `GET /events/{id}` | `recordings` | ein Ereignis |
| `GET /events/{id}/thumbnail.jpg` | `recordings` | kleines Bild (JPEG) |
| `GET /events/{id}/snapshot.jpg` | `recordings` | Schnappschuss; `404 nvr_no_picture`, wenn es keinen gibt |
| `GET /events/{id}/clip.mp4` | `recordings` | Ausschnitt; `Range` wird durchgereicht (`206`), `?download=1` setzt `Content-Disposition: attachment` |
| `POST /events/{id}/reviewed` | `recordings` | `{reviewed: true|false}` → das Ereignis |
| `DELETE /events/{id}` | Administrator | löscht Ereignis samt Schnappschuss; `204` |

```jsonc
// GET /events → { "events": [Event], "next": 1731400000.5 | null }
{ "id": "1731412345.123456-abc123", "camera": "haustuer",
  "label": "person",                 // person, car, dog, …
  "subLabel": "Anna" ,               // erkanntes Gesicht bzw. bekannter Kennzeichen-Name; "" wenn keins
  "plate": "M-AB 1234",              // gelesenes Kennzeichen; "" wenn keins
  "score": 0.87,                     // 0–1
  "start": 1731412345.1, "end": 1731412361.9,   // end null: läuft noch
  "hasClip": true, "hasSnapshot": true,
  "zones": [], "reviewed": false,
  "kept": false }                    // „behalten“: wird nie von der Speichergrenze gelöscht
```

`reviewed` gilt für den NVR insgesamt, nicht je Konto (Frigate führt „gesehen“ je Review-Abschnitt; titand setzt den Abschnitt, zu dem das Ereignis gehört).

**Live-Meldungen** – `GET /feed` (WebSocket, angemeldet). titand hält **eine** Verbindung zu Frigate und verteilt; jedes Fenster bekommt nur Nachrichten seiner Kameras mit Recht `live`. Endet wie eine Konsole (`{"t":"bye","reason":"logout"}`); entzogene Rechte wirken binnen Sekunden. Der Browser sendet nichts.

```jsonc
{ "t": "hello", "running": true }                                  // direkt nach dem Verbinden und bei jedem Wechsel von running
{ "t": "event", "phase": "new" | "update" | "end", "event": Event }
{ "t": "activity", "camera": "haustuer", "motion": true, "objects": ["person"] }   // was gerade im Bild ist; bei jeder Änderung
```

### Aufnahmen

| Route | Recht | |
|---|---|---|
| `GET /cameras/{id}/recordings/summary` | `recordings` | Tage und Stunden mit Aufnahmen (Zeitzone des NAS) |
| `GET /cameras/{id}/recordings?from=&to=` | `recordings` | Segmente für die Zeitleiste; Fenster höchstens 24 h (`400 nvr_invalid`) |
| `GET /cameras/{id}/vod/{from}/{to}/master.m3u8` (und alles darunter) | `recordings` | HLS-Wiedergabe; die Wiedergabelisten verweisen relativ, der Player braucht nur die Adresse der `master.m3u8`. `from`/`to` ganze Unix-Sekunden. |
| `GET /cameras/{id}/recordings/frame.jpg?at=&height=` | `recordings` | Standbild zum Zeitpunkt `at`; `404 nvr_no_picture` in einer Lücke |

```jsonc
// summary
{ "days": [ { "day": "2026-11-12",
              "hours": [ { "hour": 14, "start": 1731416400, "seconds": 3540, "motion": 812, "objects": 96, "events": 3 } ] } ] }
// recordings?from&to  – Lücken sind Lücken zwischen den Segmenten
{ "from": 1731412800, "to": 1731499200,
  "segments": [ { "start": 1731416400.0, "end": 1731416410.0, "motion": 120, "objects": 2 } ] }   // motion/objects: Zähler, 0 = nichts
```

### Export

Exporte gehören dem Konto, das sie angelegt hat; es sieht nur die eigenen, Administratoren alle. Anlegen braucht `recordings` auf der Kamera. Exporte zählen zur Belegung (`status.storage.exportsBytes`) und werden nie von der Speichergrenze gelöscht.

| Route | |
|---|---|
| `POST /exports` | `{camera, from, to, name}` (höchstens 24 h, `name` 1–80 Zeichen, sonst aus Kamera und Zeit) → `201` Export |
| `GET /exports` | `{exports: [Export]}`, neueste zuerst |
| `GET /exports/{id}` | ein Export (für den Fortschritt abfragen) |
| `GET /exports/{id}/download` | MP4 als Anhang, `Range` durchgereicht; `409 nvr_export_running`, solange er läuft |
| `GET /exports/{id}/thumbnail.jpg` | Vorschaubild |
| `PUT /exports/{id}` | `{name}` |
| `DELETE /exports/{id}` | `204` |

```jsonc
{ "id": "haustuer_ab12cd", "camera": "haustuer", "name": "Paketbote",
  "from": 1731412300, "to": 1731412420,   // 0, wenn der Export nicht über TitanOS angelegt wurde
  "created": 1731413000,             // wann er angelegt wurde (bei fremden Exporten: Beginn des Ausschnitts)
  "state": "running" | "done" | "failed",
  "sizeBytes": 18234567,             // null solange running
  "owner": "anna" }                  // "" wenn das Konto nicht mehr bekannt ist
```

Fehler: `nvr_export_not_found`, `nvr_export_running`, `nvr_export_failed` (Frigate hat den Export abgelehnt, etwa weil im Zeitraum nichts aufgenommen ist).

### Schwenken (PTZ)

| Route | Recht | |
|---|---|---|
| `GET /cameras/{id}/ptz` | `live` | `{supported, features: ["pt", "zoom", …], presets: ["home", …]}`; `supported: false`, wenn die Kamera nicht als PTZ angelegt ist oder Frigate keine Steuerung findet |
| `POST /cameras/{id}/ptz` | `live` | `{action, preset}`; `action`: `move_up`, `move_down`, `move_left`, `move_right`, `zoom_in`, `zoom_out`, `stop`, `preset` (mit `preset`: Name). `204`. |

Eine Bewegung läuft, bis `stop` kommt. titand schickt `stop` von selbst, wenn 5 Sekunden lang keine Anfrage für diese Kamera kommt – die Oberfläche wiederholt die Bewegung also spätestens alle 4 Sekunden, solange die Taste gehalten wird, und schickt beim Loslassen `stop`. Höchstens 10 Befehle je Sekunde und Konto (`429 rate_limited`). `409 nvr_ptz_unsupported`, wenn die Kamera nicht schwenkt.

### Gegensprechen

`GET /cameras/{id}/talk` (WebSocket, Recht `live`, nur Kameras mit `audio.twoWay`): reicht go2rtcs WebRTC-Signalisierung für den Strom `<id>_twoway` unverändert durch (`{"type":"webrtc/offer","value":sdp}` → `{"type":"webrtc/answer",…}`, `webrtc/candidate` in beide Richtungen). Weil der Kanal go2rtcs eigene Nachrichten trägt, kommen Fehler als **Schließgrund** des WebSockets (`nvr_camera_not_found`, `nvr_not_running` – auch für eine Kamera ohne Gegensprechen –, `nvr_no_picture`, `nvr_unavailable`; bei Abmelden/Rechteentzug `logout`/`forbidden`). Der Browser gibt das Mikrofon nur über **https** frei (oder `localhost`); der Medienweg geht nicht durch titand, sondern direkt zum NAS auf **Port 8555 (UDP, sonst TCP)** – von unterwegs also nur über VPN/Tailscale.

### Gesichter und Kennzeichen (nur Administratoren)

Bilder von Gesichtern sind personenbezogene Daten: nur Administratoren, nie im Protokoll, nie im Diagnosepaket.

| Route | |
|---|---|
| `GET /faces` | `{enabled, faces: [{name, images: ["a1.webp", …]}], attempts: [Attempt]}`; `enabled: false`, solange keine Kamera Gesichter erkennt |
| `POST /faces` | `{name}` legt eine Person an (`201`) |
| `PUT /faces/{name}` | `{name}` benennt um |
| `DELETE /faces/{name}` | löscht Person und Bilder (`204`) |
| `POST /faces/{name}/images` | Bild als Rumpf (`image/jpeg`, `image/png`, `image/webp`, höchstens 8 MB); Frigate verlangt genau ein erkennbares Gesicht (`400 nvr_face_rejected`) |
| `GET /faces/{name}/images/{file}` | das Bild |
| `DELETE /faces/{name}/images/{file}` | `204` |
| `GET /face-attempts/{file}` | Bild eines gesehenen, nicht zugeordneten Gesichts |
| `POST /face-attempts/{file}/assign` | `{name}` ordnet es einer Person zu (lernt es an; die Person wird angelegt, wenn es sie nicht gibt) |
| `DELETE /face-attempts/{file}` | verwirft es (`204`) |
| `GET /plates/recognized` | `{plates: [{plate, count, lastSeen, camera}]}` – gelesene Kennzeichen, häufigste zuerst; `count`, `lastSeen`, `camera` aus den neuesten 1000 Fahrzeug-Ereignissen (ältere Kennzeichen: `0`, `null`, `""`) |

Jede ändernde Route antwortet mit der Bibliothek im neuen Stand (Form wie `GET /faces`), außer den `204`-Antworten.

```jsonc
// Attempt
{ "file": "1731412345.123-abc-1731412346.5-anna-0.62.webp", "at": 1731412346.5,
  "guess": "anna" | "",              // wen Frigate vermutet hat
  "score": 0.62 }
```

Personennamen: 1–40 Zeichen, Buchstaben (auch Umlaute) und Ziffern, dazu `-` und `_`, **keine Leerzeichen** (Frigate macht daraus Unterstriche), nicht `train` (`nvr_invalid` sonst). Fehler: `nvr_face_not_found`, `nvr_face_exists`, `nvr_face_rejected`, `nvr_faces_off` (keine Kamera erkennt Gesichter – dann ist auch `GET /faces` leer mit `enabled: false`).

Bekannte Kennzeichen sind Teil der Einstellungen (`GET/PUT /settings`), neu:

```jsonc
"plates": [ { "name": "Annas Auto", "plates": ["M-AB 1234", "MAB12*"] } ]   // Muster wie in Frigate (reguläre Ausdrücke erlaubt); höchstens 100 Namen
```

### Zustand: Speicher

`status.storage` wächst (alte Felder bleiben):

```jsonc
"storage": { "usedBytes": 21474836480,        // Aufnahmen + Exporte
             "recordingsBytes": 20401094656, "exportsBytes": 1073741824,
             "limitGB": 200,
             "spanDays": 12.4,                // so weit reichen die Aufnahmen zurück; null wenn keine
             "cap": "off" | "ok" | "cleaning" | "exports_exceed",   // cleaning: über der Grenze, es wird gelöscht (oder nichts ist alt genug); exports_exceed: die Exporte allein liegen über der Grenze – nichts wird gelöscht
             "lastCleanup": 1731400000 | null }
```

## Speichergrenze

`settings.storage.limitGB` (0 = aus). Frigate selbst räumt nur, wenn das Volume fast voll ist; die Grenze setzt TitanOS durch (`nvr/storage.go`, alle 5 Minuten zusammen mit den Speicherzahlen):

- **Belegung** = Aufnahmen (wie Frigate sie je Kamera meldet, `/api/recordings/storage`) + Exporte (Größe jeder fertigen Exportdatei). Schnappschüsse, Vorschauen und Frigates Datenbank zählen nicht mit.
- Liegt die Belegung über der Grenze, löscht TitanOS die **ältesten Aufnahmen aller Kameras stundenweise** (`DELETE /api/recordings/start/{s}/end/{e}`), bis höchstens **95 %** der Grenze belegt sind; je Durchgang höchstens 72 Stunden.
- **Nie gelöscht:** Exporte; die Aufnahmen **behaltener Ereignisse** (`kept`, in Frigate „retain indefinitely“) samt einer Minute davor und danach – aus der Stunde wird um sie herum gelöscht; die jüngste Stunde. Lässt sich nicht erfahren, was behalten ist (Frigate antwortet nicht), wird aus der Stunde nichts gelöscht.
- Liegen **die Exporte allein** über der Grenze, wird nichts gelöscht und der Zustand sagt es (`cap: "exports_exceed"`): Der Besitzer muss Exporte löschen oder die Grenze anheben.
- Meldung `nvr_storage_cap` höchstens **einmal am Tag** (der Tag steht in `state.json` auf dem Volume, damit ein Neustart von titand nicht erneut meldet).
- `spanDays`: wie weit die Aufnahmen zurückreichen (Beginn der ältesten Stunde über alle Kameras – auf die Stunde genau).

## Meldungen je Kamera

Jede Kamera wählt Wege (`notify.titan`, `push`, `email`, `homeAssistant`, `agent`) und Arten (`notify.kinds`: Objektnamen wie `person`, `car`, `dog`, dazu `plate`, `face`, `offline`). Ausgewertet wird, was Frigate über seinen WebSocket meldet (`nvr/routing.go`):

| Art | Wann | Meldung |
|---|---|---|
| Person | ein Ereignis mit `person` beginnt | `nvr_event_person` |
| Fahrzeug | `car`, `motorcycle`, `bus`, `bicycle` | `nvr_event_vehicle` |
| Tier | `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `bear` | `nvr_event_animal` |
| Kennzeichen | Frigate hat ein Kennzeichen gelesen (einmal je Ereignis); ein bekanntes mit seinem Namen | `nvr_event_plate` |
| Gesicht | Frigate hat ein **bekanntes** Gesicht erkannt (einmal je Ereignis) | `nvr_event_face_known` |
| Offline | 5 Minuten kein Bild | `nvr_camera_offline` |

- Gemeldet wird ein Objekt nur, wenn sein Name in `kinds` steht (also `car`, nicht „Fahrzeug“); die Gruppe bestimmt nur den Text und die Sperrzeit.
- **Sperrzeit:** höchstens eine Meldung je Kamera und Art in 60 Sekunden (`nvr.NoticePause`).
- **Wege:** Glocke und persönliche Meldungen (`titan`), Push an angemeldete Geräte (`push`), E-Mail über den eingerichteten Mailweg (`email`, nur wenn E-Mail in den Benachrichtigungen an ist), KI-Agent (`agent`: die Meldung geht an denselben Beobachter wie alle TitanOS-Meldungen). `notify.Manager.NotifyVia` schickt genau über die gewählten Wege – unabhängig von der Mindeststufe und der Sammelzustellung der allgemeinen Benachrichtigungen. Ein Bild hängt nicht an (die vorhandenen Wege tragen keins).
- **Home Assistant** (`homeAssistant`): keine eigene Verbindung. Ist MQTT in der Home-Assistant-Anbindung eingerichtet und dort die neue Sensorgruppe **`cameras`** gewählt, erscheint je Kamera ein Gerät mit `Online` (Verbindung), je einem Binärsensor `Person`, `Vehicle`, `Animal`, `License plate`, `Known face` (an für 90 Sekunden nach dem Ereignis; veröffentlicht wird im MQTT-Takt, ab Werk 30 s) und `Last event` (Text). Nur Kameras mit `notify.homeAssistant`.
- **Unbekannte Gesichter** meldet Frigate nicht als Ereignis; sie erscheinen nur in der Bibliothek unter „nicht zugeordnet“. Eine Meldung dafür gibt es deshalb nicht.
- Ausweichen, Anhalten und die Speichergrenze melden weiter über alle eingerichteten Wege (`notify.Manager.Notify`), unabhängig von den Kameras.

## Sicherung

- **Einstellungen, Kameras, Kamerarechte und bekannte Kennzeichen** liegen in `/config/nvr/` und laufen mit jeder Sicherung des Sticks mit.
- **Bekannte Gesichter** liegen bei Frigate auf dem NVR-Volume (`.titanos/nvr/media/clips/faces/<Person>/<Bild>`). Sie kommen als Zusatzdateien `volume-data/nvr/faces/<Person>/<Bild>` in dieselbe Sicherung (`configbackup.Manager.Extra`, `nvr/backup.go`). Nicht zugeordnete Gesichter (`train`) bleiben draußen. Grenzen: 2 MB je Bild, 2000 Bilder, 12 MB – eine größere Bibliothek wird als Ganzes ausgelassen (Protokollzeile), nie gekürzt, und lässt die Sicherung nie scheitern.
- **Schlafende Platten** werden dafür nie geweckt, ein nicht eingebundenes Volume nie gelesen: Dann kommt die Bibliothek so in die Sicherung, wie sie zuletzt gelesen wurde (RAM); wurde sie seit dem Start von titand nie gelesen, fehlt sie in dieser Sicherung (Protokollzeile).
- **Wiederherstellen:** Die Wiederherstellung des Sticks schreibt die Zusatzdateien unter demselben Namen nach `/config/volume-data/nvr/faces/`. Sobald der NVR das nächste Mal startet (Volume da, vor dem Start des Containers), kopiert er sie in die Bibliothek auf dem Volume – nur Bilder, die dort fehlen – und entfernt sie vom Stick. Bis dahin gehen sie weiter in jede neue Sicherung ein. Aufnahmen, Ereignisse und Frigates Datenbank sind **nicht** Teil der Sicherung.
- **Diagnosepaket:** `nvr.json` enthält die Diagnose (Zustand, Container, Erkennungsgerät, letzter Fehler, Protokoll ohne Passwörter, Kameranamen), solange der NVR eingeschaltet ist; keine Bilder, keine Gesichter, keine Zugangsdaten.

## Entwicklungsmodus

`titand -dev` hat kein Docker. Der NVR läuft dort auf einem Ersatz (`nvr/dev.go`, `nvr/dev_data.go`, nur dort und in Tests benutzt): ein Motor, der sich einen Container merkt, und ein Ersatz für Frigates API auf einem Port von `127.0.0.1`. Daten liegen in einem temporären Ordner, nie auf einem Volume. Was die Oberfläche vorfindet:

- **Kameras:** vier Beispielkameras, solange es keine Kameradatei gibt (nur im RAM): `haustuer` (Ton, Gegensprechen, Gesichter), `einfahrt` (Kennzeichen, Aufnahme bei Bewegung), `garten` (ONVIF, PTZ, dauernde Aufnahme, **offline**), `garage` (ausgeschaltet). Geräte „NPU“ und „iGPU“. Die Suche findet zwei Kameras, die Prüfung gelingt außer mit dem Passwort `wrong`.
- **Live:** Standbilder als Farbverlauf je Kamera; der Live-WebSocket verbindet, liefert aber kein Video; `…/talk` antwortet auf ein Angebot mit `{"type":"error",…}`.
- **Ereignisse:** je eingeschalteter Kamera sechs Ereignisse über die letzten Stunden (die älteren drei „gesehen“), bei `haustuer` eins mit erkanntem Gesicht („Anna“), **eins, das gerade läuft** (ohne Ende, ohne Schnappschuss) und ein **behaltenes** von gestern; bei `einfahrt` zwei mit Kennzeichen (eins bekannt: „Annas Auto“). Vorschaubilder sind Farbverläufe, jeder Clip ist dasselbe Testvideo (4 s, 320×180).
- **Feed:** `hello`, dann die Aktivität der laufenden Kameras (bei `haustuer` Bewegung und `person`).
- **Aufnahmen:** drei Tage zurück, mit einer **Lücke von fünf Stunden** gestern (je Kamera um eine Stunde versetzt) und einer Viertelstunde, die in einer Stunde fehlt; Bewegung in Abschnitten, Objekte rund um die Ereignisse. Die Wiedergabe (`…/vod/…/master.m3u8`) spielt für jeden Zeitraum dasselbe 4-Sekunden-Testbild (zwei fMP4-Segmente); `frame.jpg` gibt in einer Lücke `404`.
- **Exporte:** drei (keinem Konto zugeordnet, also für Administratoren): „Paketbote“ fertig, „Nacht“ läuft, „Lücke“ fehlgeschlagen. Ein neuer Export ist nach zwei Abfragen fertig; für einen Zeitraum ohne Aufnahme scheitert er (`nvr_export_failed`).
- **PTZ:** nur `garten` (`features: ["pt","zoom"]`, Voreinstellungen `home`, `tor`, `terrasse`); Befehle werden angenommen.
- **Gesichter:** „Anna“ (zwei Bilder), „Ben“ (eins), zwei nicht zugeordnete (eins mit Vermutung „Anna“). Ein hochgeladenes Bild unter 400 Bytes wird als „kein Gesicht“ abgelehnt. **Kennzeichen:** `M-AB 1234`, `B-XY 987`.
- **Speicher:** die Zahlen ergeben sich aus den Beispielaufnahmen (450 MB je Stunde und Kamera); mit einer Grenze darunter löscht der Ersatz wirklich „Aufnahmen“.

Die Beispieldateien (`backend/internal/nvr/devmedia/`, zusammen rund 113 KB) sind mit dem ffmpeg aus Frigates Abbild erzeugt und ins Binary eingebettet.

## Prüfstand (12.11.2026) und Grenzen

**Geprüft**

- **Go-Tests ohne Docker** (Ersatzmotor und Ersatz-Frigate, `httptest`):
  - Kern: Konfigurationserzeugung (fünf Golden-Dateien, Reihenfolge, Zugangsdaten, Klammern, bekannte Kennzeichen), Prüfregeln, Geräteerkennung und Ketten, Ursachen aus dem Protokoll, Schwärzen von Passwörtern, Kamerarechte, der Ablauf (Einschalten, Ändern ohne unnötigen Neustart, Übernahme nach titand-Neustart, Ausweichen NPU → iGPU → CPU → angehalten, „Wieder versuchen“, Vergessen nach NAS-Neustart, abgelehnte Konfiguration, nie gesund, toter Detektor, hängendes Frigate, Abbild laden und Fehlschlag, Kamera offline, Volume weg), WS-Discovery, ONVIF, RTSP `DESCRIBE`.
  - Phase 2: Ereignisse (Filter, Seiten, „gesehen“, Löschen, Pfadtricks), Aufnahmen (Tage/Stunden, Zeitzone, Segmente mit Lücke), Durchreichen mit `Range`, Standbild-Zwischenspeicher, Exporte (Besitzer, Zustände, Umbenennen, Löschen, gelöschtes Konto), Feed (Rechte je Nachricht, langsames Fenster), Meldungswege (Arten, Sperrzeit, einmal je Ereignis, Home-Assistant-Zustand), PTZ (Stopp von selbst, Begrenzung je Konto, Voreinstellungen), Gesichter, **die Rechnung der Speichergrenze** (älteste Stunden bis 95 %, behaltene Ereignisse ausgespart, letzte Stunde, Exporte über der Grenze, einmal am Tag), Sicherung der Gesichter und Rückweg nach einer Wiederherstellung, Zusatzdateien in `configbackup`, `nvr.json` im Diagnosepaket.
  - Routen: Rollen, Filter und Rechte für jede Route (auch: Ereignisse/Wiedergabe/Exporte fremder Kameras sind „nicht vorhanden“, Pfadtricks in Kamera, Zeit und Dateiname der Wiedergabe), Live- und Gegensprech-Durchreichung, Feed mit Abmelden, fremde Herkunft.
- **Frigates eigener Prüfer:** Alle fünf Golden-Dateien (darunter die mit bekannten Kennzeichen, Gesichtern, PTZ, Gegensprechen) mit dem echten Abbild 0.18.2 geprüft (`python3 -m frigate --validate-config`): „Your config file is valid“, keine Migration. Dazu go2rtcs Konfigurationserzeugung und die nginx-Einstellungen aus derselben Datei.
- **Echter Lauf mit Kamera** auf der Entwicklungsmaschine (Debian-VM, Docker 26.1, ohne GPU/NPU): `TestNVRRealFrigate` (`backend/internal/api/nvr_real_test.go`, nur mit `TITANOS_NVR_REAL=<Ordner>`). titands Routen, der Manager, echtes Docker, das echte Abbild und als Kamera ein zweiter Container desselben Abbilds, dessen go2rtc ein bewegtes Testbild (H.264, 640×360, 10 Bilder/s) per RTSP anbietet. Bestanden am 12.11.2026 (5 min 16 s):
  - Einschalten über `PUT /nvr/settings`: Frigate 0.18.2 mit CPU-Detektor gesund nach etwa 6 s; 256 MB `/dev/shm`.
  - `POST /nvr/cameras/probe` (titands eigener RTSP-`DESCRIBE`) gegen go2rtcs RTSP-Server: `ok`, Codec H264.
  - Kamera angelegt → Frigate startet im selben Container neu → Kamera **online** nach rund 45 s.
  - `snapshot.jpg`: JPEG. **Live:** go2rtcs MSE-Protokoll durch titand – Antwort `video/mp4; codecs="avc1.420029"`, danach Videodaten.
  - **Feed:** `hello`, dann `activity` der Kamera mit Bewegung (Frigates `/ws` läuft ohne MQTT-Broker).
  - **Aufnahmen:** Segmente erscheinen (rund 10 s lang, mit Bewegungszähler), die Zusammenfassung nennt die Stunde.
  - **Wiedergabe:** `master.m3u8` → `index-v1.m3u8` → `init-v1.mp4`, `seg-1-v1.m4s` – **alle Namen relativ**, jede Datei kam durch titands Route (`video/mp4`, 1,3 MB je Segment); `frame.jpg` zu einem aufgenommenen Zeitpunkt: JPEG.
  - **Export:** angelegt, nach rund 6 s fertig (2,6 MB), Download mit `Range` (`206`, `bytes 0-1023/2629342`, MP4-Kopf), umbenannt, gelöscht.
  - **Speicherzahlen** nach dem nächsten Nachsehen: Aufnahmen 31 MB, Exporte 2,6 MB, `spanDays` gesetzt.
  - Ausschalten entfernt den Container; Netz und Hilfscontainer entfernt.
- Von Hand: Mit der erzeugten Datei antwortet Port 5000 an der Container-Adresse, Port 8971 nimmt von außerhalb des Containers keine Verbindung an.

**Nicht geprüft**

- NPU, iGPU (OpenVINO `GPU`), Coral und Hailo auf echter Hardware; VA-API-Dekodierung im Container; das Ausweichen mit echten Fehlerbildern (die NPU-Fehlertexte stammen aus Frigates Doku und einem Fehlerbericht).
- Echte Kameras verschiedener Hersteller: ONVIF, Ton, `#media=video`, Gegensprechen (WebRTC über Port 8555), PTZ (Frigates `ptz/info` und der Befehlsweg sind nur gegen den Ersatz geprüft), WS-Discovery in einem echten Netz.
- **Ereignisse mit echten Objekten:** Das Testbild zeigt nichts Erkennbares – die Ereignisliste war im echten Lauf leer. Form und Felder der Ereignisse, „gesehen“, Clip/Schnappschuss, die Ereignisnachrichten des Feeds und damit die Meldungen je Kamera sind nur gegen den Ersatz geprüft (dessen Formen aus Frigates Quelltext stammen).
- **Speichergrenze gegen echtes Frigate:** das Löschen (`DELETE /api/recordings/…`), der Filter für behaltene Ereignisse (`favorites=1`) und die Zahlen danach – nur die Rechnung gegen den Ersatz.
- **Gesichter und Kennzeichen** im Betrieb (Modelle laden braucht Internet und RAM): alle `/api/faces…`-Aufrufe und die Namen der nicht zugeordneten Dateien nur gegen den Ersatz.
- Fehlgeschlagene und wartende Exporte gegen echtes Frigate (nur der gute Weg lief echt); das Vorschaubild eines Exports.
- Live-Bild in einem Browser (der echte Lauf prüft das Protokoll bis zu den ersten Videodaten, kein Abspielen); HLS in einem Player.
- Lauf auf einem TitanOS-NAS (Rechte im privaten Ordner als root, Firewall mit Port 8555, Docker auf einem anderen Volume, GPU-Übergabe an eine VM), Home Assistant mit den neuen Sensoren, E-Mail-/Push-Zustellung der Kameramitteilungen, Sicherung/Wiederherstellung der Gesichter auf einem echten Stick.
- Das Laden des Abbilds durch den Manager selbst (das Abbild war schon da; der Fortschritt ist nur gegen aufgezeichnete Zeilen geprüft).

**Bekannte Grenzen**

- **GPU an eine VM:** Für Apps lehnt TitanOS den Start einer VM ab, solange ein Container das Gerät benutzt; der NVR-Container ist für diese Prüfung unsichtbar (er ist keine App), und für die Fernansicht gibt es keinen Haken, an den sich der NVR hätte hängen können. Der VM-Start wartet also nicht: Der NVR bemerkt die Vergabe beim nächsten Nachsehen (bis 10 s) und baut seinen Container ohne das Gerät neu (Dekodierung dann auf der CPU, Erkennung auf dem nächsten Gerät der Kette); kommt das Gerät zurück, nimmt er es wieder. In der Zwischenzeit kann Frigate Fehler der Dekodierung melden.
- Ältere Frigate-Abbilder räumt der NVR nach einem Versionswechsel selbst weg – nur solche, die er selbst geladen hat (nur per Digest bekannt, ohne Tag, nicht in Benutzung); ein von Hand geladenes `frigate:<tag>` bleibt.
- Gesichter/Kennzeichen laufen mit Frigates Vorgabe (kleine Modelle auf der CPU).
- Solange gar keine Erkennung läuft (NVR `stopped`), wird nicht aufgenommen.
- „Gesehen“ gilt für den NVR insgesamt, nicht je Konto. Exporte kennen ihren Besitzer nur, wenn TitanOS sie angelegt hat (`exports.json` auf dem Volume); fehlt die Datei, sehen nur Administratoren die Exporte.
- Die Speicherzahlen und die Grenze zählen Aufnahmen und Exporte, nicht Schnappschüsse, Vorschauen und Frigates Datenbank.
- Standbilder werden je Kamera und Größe eine Sekunde lang wiederverwendet (Widgets mehrerer Fenster fragen dasselbe Bild).

## Was noch fehlt

- **Oberfläche:** Programm „NVR“, Einrichtungsassistent, Reiter „Kameras“ bei den Konten, Kamera-Widget (eigener Zweig).
- **Texte der Oberfläche** für die neuen Meldungsarten `notify.kind.nvr_event_person|vehicle|animal|plate|face_known` und eine Glockengruppe für `nvr_*` in `web/src/lib/notify-groups.ts` (fällt sonst unter „System“); die Sensorgruppe `cameras` in den Home-Assistant-Einstellungen braucht ihren Namen.
- **Abbild von 0.78:** NPU-Firmware prüfen/anheben (mindestens Stand Juli 2025), `release-kind`.
- Nicht gebaut: Meldung bei unbekanntem Gesicht (Frigate meldet es nicht als Ereignis); Bild an Push/E-Mail; „bei Bewegung aufnehmen, solange keine Erkennung läuft“; Gesichter/Kennzeichen auf der iGPU statt der CPU; mehrere Kennzeichen-/Gesichtsmodelle.
