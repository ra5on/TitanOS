# Android-App: Kopplung und Anmeldung (PLAN 116)

Die Android-Apps „TitanOS“ (`io.github.ra5on.titanos`), „Agent“ (`io.github.ra5on.titanos.agent`) und die Uhr-App (`io.github.ra5on.titanos.watch`, Wear OS) liegen im Repo `ra5on/TitanOS-Android`. Dieses Dokument beschreibt, was das NAS dafür bereitstellt. Technikplan und Entwürfe stehen in `Entwuerfe/116-android/`, außerhalb des Repos.

## Grundsatz

- **Die App ist eine interaktive Sitzung wie der Browser, kein API-Token.** Sie bekommt eine normale Sitzung (`auth.Sessions`) und schickt sie als Cookie.
  - Sie fällt deshalb unter dieselben Regeln: Rechte des Kontos, Sperre, Sitzung sperren, `browserOnly` ist erlaubt.
  - API-Tokens, MCP und Agent bleiben weiter draußen.
- **Ein Gerät ist ein Schlüsselpaar** (ECDSA P-256) aus dem Android Keystore. Das NAS speichert nur den öffentlichen Schlüssel.
  - Ablage: `users/<konto>/apps.json` auf dem Stick.
  - Geschrieben wird nur beim Koppeln, Beenden und Löschen, nie bei Benutzung.
- **Die Sitzung ist kurzlebig.** Fehlt sie, etwa nach einem Neustart des NAS, nach Ablauf oder nach 401, holt die App eine neue: Sie unterschreibt eine Herausforderung mit dem Geräteschlüssel. Eine erneute Kopplung ist dafür nicht nötig.
- **Dauer eines Geräts** folgt der NAS-Einstellung „Angemeldet bleiben“ (Einstellungen › Zugang):
  - aus: 30 Tage nach der Kopplung, wie die längste Browser-Sitzung
  - 30/90/365 Tage: so lange nach der Kopplung
  - „Ohne Ende“: bis jemand es beendet
- **Wann ein Gerät endet:**
  - Kontopasswort oder 2FA ändert sich (Fingerabdruck der Zugangsdaten wie bei den Gerätepasswörtern, S-25)
  - das Konto wird gelöscht
  - jemand beendet es unter Mein Konto › Geräte
  - Seine Sitzungen enden jeweils mit.

## Ablauf

1. **Browser:** `POST /api/v1/account/apps/pair` (Sitzung im Browser).
   - Antwort: `{id, code, manualCode, expires, link, addresses:[{server, kind, certificate, spki}]}`.
   - Der Code gilt 5 Minuten und nur einmal.
   - `link` steht im QR-Code und hinter „Auf diesem Handy öffnen“: `titanos://pair?v=1&c=<code>&a=<server>!<spki>&a=…`.
   - `spki` ist der SHA-256 des öffentlichen Schlüssels im Zertifikat, das das NAS für diese Adresse ausliefert, base64url ohne Auffüllung.
2. **App:** `POST /api/v1/app/pair/claim` mit `{code, name, app, publicKey}`.
   - `code` ist der lange Code oder `manualCode` (8 Zeichen, Bindestrich egal, Groß/klein egal).
   - `app` ist `titanos` oder `agent`.
   - `publicKey` ist SPKI-DER base64 (P-256).
   - Antwort: `{state:"claimed"}`.
   - Falsche Codes zählen gegen die Adresse (Mengenbegrenzung wie beim Login).
3. **Browser:** fragt `GET /api/v1/account/apps/pair/{id}` ab.
   - Antwort: `{state, name, app}`.
   - Bei `claimed` zeigt die Seite „<Name> möchte sich anmelden“ mit Bestätigen und Ablehnen: `POST …/pair/{id}/confirm` mit `{password, code}` (Kontopasswort und ggf. 2FA-Code, wie beim Gerätepasswort) bzw. `…/reject`.
   - `DELETE …/pair/{id}` bricht ab.
4. **App:** `POST /api/v1/app/pair/wait` mit `{code, signature}`.
   - `signature` ist ECDSA-SHA256 (ASN.1/DER, base64) über `titanos-pair\n<code>`. Der Code ist dabei der, den die App bei claim geschickt hat.
   - Antwort: `{state}`, bei `confirmed` zusätzlich `{device, user}`.
   - Mögliche Zustände: `claimed`, `confirmed`, `rejected`, `expired`.
5. **App:** `POST /api/v1/app/challenge` mit `{user, device}`, Antwort `{nonce}`. Die Antwort kommt für jedes Konto und Gerät gleich, sie verrät also nichts. Die Herausforderung gilt 2 Minuten und nur einmal.
6. **App:** `POST /api/v1/app/session` mit `{user, device, nonce, signature}`.
   - `signature` geht über `titanos-app-session\n<user>\n<device>\n<nonce>`.
   - Antwort: `{cookie, token, user}`. Danach schickt die App `Cookie: <cookie>=<token>`.
   - `401 app_unknown`: Das Gerät ist beendet, abgelaufen oder unbekannt. Die App zeigt die Kopplung wieder.

**Die zweite App ohne zweite Kopplung** (seit 1.19, Inhaber 20.11.2026: „Warum klappt das nicht simultan?“): Eine App kann den Schlüssel der anderen nicht mitbenutzen (jede App hat ihren eigenen Keystore). Deshalb koppelt eine gekoppelte App die andere auf demselben Handy:
1. **Gekoppelte App:** `POST /api/v1/app/pair/sibling` mit `{app}` aus ihrer App-Sitzung (`appOnly`, sonst 403 `app_session_required`). `app` ist die jeweils andere App (dieselbe: 400 `invalid_app`). Antwort `{code, expires}`, 2 Minuten, nur der lange Code.
2. Die App gibt den Code über eine Schnittstelle weiter, die Android nur Apps mit derselben Signatur öffnet (signature-Berechtigung), nie über Zwischenablage, Link oder Datei.
3. **Andere App:** `claim` und `wait` wie oben, mit eigenem Schlüssel. Kommt `claim` von derselben Adresse wie die Anfrage, mit der genannten App, solange Zugangsdaten des Kontos und die fragende App unverändert sind, ist die Kopplung damit bestätigt (`claim` antwortet `confirmed`). Sonst gilt der Code als unbekannt und ist verbraucht.
- Die Bestätigung mit dem Kontopasswort im Browser hat schon die erste App bekommen; die Sitzung der App hat dieselben Rechte wie das Konto und gibt damit nichts Neues frei. Unter Mein Konto › Geräte stehen beide Apps getrennt, jede mit „Beenden“. Aktivitätsprotokoll `app.sibling`.

**Die Uhr (Wear OS, seit 1.19, PLAN 116 Paket G):** Die Uhr-App `io.github.ra5on.titanos.watch` spricht selbst mit dem NAS über WLAN, das Handy (Bluetooth-Proxy des Systems) oder LTE, ohne Google: keine Play-Dienste, keine Wearable Data Layer API. Sie hat keine Kamera für den QR-Code und kann deshalb weder Link noch Fingerabdruck vom Handy übernehmen. Gekoppelt wird so:
1. **Handy:** Die TitanOS-App zeigt unter „Uhr koppeln“ die Adressen des NAS. Eine davon wird auf der Uhr eingetippt.
2. **Uhr:** baut die TLS-Verbindung auf und merkt sich den SPKI-Hash des Zertifikats, das sie dort sieht (noch ungeprüft). Dann `POST /api/v1/app/pair/watch` mit `{name, publicKey, commit}`.
   - `commit` = base64url ohne Auffüllung von SHA-256(`titanos-watch-commit\n<publicKey>\n<spki>\n<1|0>\n<secret>`). `secret` ist ein Zufallswert der Uhr, `publicKey` genau wie gesendet, `spki` und `1|0` (öffentliches Zertifikat) wie die Uhr sie beim Lesen des Zertifikats gesehen hat. Die Festlegung bindet damit alles, was in die Ziffern eingeht (Sicherheitsdurchsicht 22.11.2026: vorher ließen sich `spki` und die Angabe „öffentlich“ nach dem Erfahren der Ziffern frei wählen). Keiner der Werte darf einen Zeilenumbruch enthalten.
   - Antwort `{id, nonce, expires}`. `nonce` ist der Zufallswert des NAS. Die Kopplung gilt 5 Minuten.
   - Grenzen: höchstens 50 offene Uhr-Kopplungen am NAS, 3 offene und 10 Starts in 5 Minuten je Adresse, IPv6 je /64 (`429 pairing_limit`).
3. **Uhr:** `POST /api/v1/app/pair/watch/{id}/reveal` mit `{secret, spki, publicCert}`.
   - `spki` ist der Hash, den die Uhr gesehen hat. `publicCert` ist `true`, wenn die Uhr ein gültiges öffentliches Zertifikat selbst geprüft hat.
   - Das NAS rechnet `commit` aus `secret`, `spki` und `publicCert` nach; weicht etwas ab, ist die Kopplung verbraucht.
   - Zertifikat: Über titands eigenes TLS muss `spki` genau das Zertifikat sein, das titand für den Servernamen dieser Verbindung ausliefert, öffentlich oder nicht. Endet TLS vor titand (Tailscale Serve), zählt `publicCert: true` nur für einen Namen, den titand mit gültigem öffentlichem Zertifikat führt (Let's Encrypt, Tailscale); sonst muss `spki` eines der Zertifikate sein, die titand für seine Adressen ausliefert.
   - Passt eines nicht, ist die Kopplung verbraucht: `404 pairing_code` bzw. `409 watch_certificate`.
4. **Uhr und NAS** rechnen dieselben sechs Ziffern: SHA-256 über `titanos-watch-code\n` + `id`, `publicKey`, `nonce`, `secret`, `spki` und `1`/`0` für `publicCert`, jeweils durch `\n` getrennt. Davon die ersten 8 Bytes als Zahl (big-endian) modulo 10⁶, mit führenden Nullen. Die Uhr zeigt sie als „482 913“.
5. **Handy:** Die gekoppelte TitanOS- oder Agent-App schickt die eingetippten Ziffern mit `POST /api/v1/app/pair/watch/confirm` `{code}` aus ihrer App-Sitzung.
   - Nur App-Sitzungen (`403 app_session_required`), nur Handy-Apps (eine Uhr bestätigt keine Uhr: `403 watch_confirmer`).
   - Falsche Ziffern: `404 pairing_code`, zählen je Konto (danach 429). Antwort `{state:"confirmed", id, name, app:"watch"}`.
   - Die Uhr wird ein eigenes Gerät des Kontos (`app: "watch"`), unter Mein Konto › Geräte einzeln mit „Beenden“. Aktivitätsprotokoll `app.pair`.
6. **Uhr:** fragt `POST /api/v1/app/pair/watch/{id}/wait` mit `{signature}` ab, unterschrieben über `titanos-watch-wait\n<id>`.
   - Zustände `started`, `revealed`, `confirmed`.
   - Bei `confirmed` kommen `{device, user, addresses}` dazu, einmal. `addresses` sind alle Adressen mit SPKI wie beim Koppeln im Browser, damit die Uhr das NAS auch unterwegs erreicht.
   - Danach `challenge`/`session` wie jede App.

Warum so: Die Bestätigung im Browser mit Kontopasswort hat schon das Handy bekommen. Die Sitzung der Handy-App hat dieselben Rechte wie das Konto und gibt mit der Uhr nichts Neues frei, wie bei der zweiten App. Anders als dort kommt die Uhr aber nicht von derselben Adresse, und ihr Zertifikat stammt aus einer eingetippten Adresse. Deshalb gilt:
- Die sechs Ziffern hängen am Schlüssel der Uhr, an beiden Zufallswerten und am gesehenen Zertifikat.
- Wer dazwischen sitzt, hat auf jeder Seite andere Werte und damit andere Ziffern. Weil die Uhr ihren Zufallswert vor dem des NAS festlegt, kann er auch keinen Schlüssel mit denselben Ziffern suchen (Trefferchance 1 zu 10⁶ je Versuch, Versuche zählen).
- Ein Gerät, das sich nur als Uhr ausgibt, sieht die Ziffern auf der echten Uhr nicht.
- `/app/pair/sibling` nimmt `watch` nicht an (`400 invalid_app`), und eine Uhr bekommt dort keinen Code.

**Rechte der Uhr** (Sicherheitsdurchsicht 22.11.2026): Die Sitzung einer Uhr erreicht nur, was die Uhr-App braucht (`api/appwatch_rights.go`); alles andere ist `403 watch_forbidden`.
- Erlaubt: `GET /session`, `/system`, `/storage`, `/services/state`, `/app/notifications`; NVR `GET /nvr/cameras`, Standbild, `GET /nvr/events` und Ereignisbilder; KI-Agent Status, Chats lesen, anlegen, Nachricht, `decide`, `transcribe`, `speech/say`, `speech/hear`; Home Assistant `GET /homeassistant/entities` und `POST /homeassistant/call` nur für `light`, `switch`, `fan`, `input_boolean`, `scene`, `script` mit `turn_on`/`turn_off`/`toggle`; `GET /docker` und Container `start`/`stop`/`restart`; `GET /vms` und VM `start`/`shutdown`.
- Gesperrt unter anderem: `/app/vpn`, `/app/pim/*`, `/app/photos/*`, `/app/pair/sibling`, `/app/pair/watch/confirm`, Dateien, Konto- und Sicherheitsrouten, alle Einstellungen. Die Kontorechte gelten zusätzlich wie immer.
- `POST /app/vpn` verlangt außerdem eine Handy-App (`403 app_phone_required`).

- **Prüfstand der Uhr (22.11.2026):** Go-Tests `TestWatchPairing`, `TestWatchRefusals` und `TestWatchCodeIsStable` (feste Werte, die die App in `WatchCodeTest` genauso prüft) in `appdevices`, `TestAppWatchPairing` in `api`. Geprüft werden: falsches Zertifikat, falsches Geheimnis, vertauschter Schlüssel, Ablauf, Grenzen je Adresse und für das ganze NAS, Wiederholung, nur Handy-Apps bestätigen, falsche Ziffern zählen, kein Sibling-Code für oder von der Uhr, geänderte Zugangsdaten.
  - **Emulator:** Wear OS 7 (Uhr) und Android 17 (Handy) gegen einen Wegwerf-Testserver mit TLS direkt im Harness (`internal/tlscert`, Port 8463), KI-Agent mit nachgebautem Ollama.
    - Handy über Link gekoppelt; auf der Uhr die Adresse `10.0.2.2:8463` eingetippt, Erlaubnis „Geräte in der Nähe“ (lokales Netzwerk) erteilt.
    - Die Uhr zeigte sechs Ziffern; am Handy unter „Uhr koppeln“ eingegeben, danach Gerät `watch` unter `/account/apps`.
    - Zustand, Meldungen (Web-Push-Spiegel), Kachel, Agent getippt mit Antwort und Vorlesen, Freigabe des Agenten auf der Uhr erlaubt (Datei am Testserver ersetzt).
    - „Beenden“ am NAS meldete die Uhr ab, sie zeigte wieder die Kopplung.
  - Nach der Sicherheitsdurchsicht (22.11.2026) zusätzlich: geändertes `spki` oder `publicCert` nach der Festlegung, Zeilenumbruch in einem Wert, „öffentlich“ ohne öffentliches Zertifikat, Starts je Adresse und IPv6 als /64, Rechte der Uhr-Sitzung (erlaubte und gesperrte Routen, Home-Assistant-Dienste, Container- und VM-Aktionen), `server` von `/app/vpn` ohne Recht verborgen.
  - Nicht geprüft: echte Uhr, Bluetooth-Proxy des Handys, LTE, Komplikation auf einem Zifferblatt (der Emulator hat keines, das sich per ADB belegen lässt), Selbst-Update der Uhr, Diktat ohne Spracheingabe des Systems.

**Liste und Beenden:**
- `GET /api/v1/account/apps` liefert `{apps:[{id, name, app, created, expires}]}`.
- `DELETE /api/v1/account/apps/{id}` beendet ein Gerät.
- Beides nur aus einer Sitzung des Kontos.

## TLS

Die App vergleicht beim Verbindungsaufbau den SPKI-Hash des Serverzertifikats mit dem Wert aus der Kopplung.

- **Mit QR-Code oder Link** kommt der Wert aus `link`.
- **Ohne QR-Code** (Adresse von Hand) zeigt die App den Hash in Vierergruppen an. Die Seite im Browser zeigt unter „Ohne QR-Code“ denselben Wert und `manualCode`.
- **Adressen mit gültigem öffentlichem Zertifikat** (Let's Encrypt, `*.ts.net`) prüft die App zusätzlich normal. Erneuert sich das Zertifikat, wechselt bei Let's Encrypt meist auch der Schlüssel. Für `certificate: "valid"` gilt deshalb eine normale Prüfung als Ersatz.

## Anzeige und Fehler

- **Fingerabdruck zum Vergleichen:** die ersten 16 Bytes des SPKI-SHA-256, hexadezimal in Großbuchstaben, in acht Vierergruppen. Browser und App zeigen ihn gleich.
- **Fehler der App-Routen** (immer `{"error":{"code","message"}}`):

  | Status | Code | Bedeutung |
  |---|---|---|
  | 404 | `pairing_code` | Code unbekannt, schon benutzt oder abgelaufen |
  | 400 | `invalid_device_name` | Gerätename ungültig |
  | 400 | `invalid_app` | unbekannte App |
  | 400 | `invalid_key` | Schlüssel ungültig |
  | 429 | – | zu viele Fehlversuche von dieser Adresse, mit `Retry-After` |
  | 401 | `app_unknown` | Gerät unbekannt, beendet oder abgelaufen |
  | 503 | `apps_unavailable` | Stick nicht lesbar |

- **Fehler der Browser-Routen:**

  | Code | Bedeutung |
  |---|---|
  | `pairing_state` | Bestätigen, bevor das Handy sich gemeldet hat |
  | `pairing_limit` | mehr als drei offene Kopplungen |
  | `app_limit` | mehr als 20 Apps je Konto |

## Foto-Sicherung

Die App sichert Fotos und Videos in eine Freigabe und einen Ordner nach Wahl (TECHNIK §3, Entwürfe 30–33). Hochgeladen wird über den vorhandenen Upload in Stücken des Dateimanagers; neu sind drei Routen, die **nur die Sitzung einer gekoppelten App** erreicht.

- Eine Browser-Sitzung ohne Gerät bekommt `403 app_session_required`.
- Nicht in `publicRoutes`, nicht im Werkzeugkatalog: API-Tokens, MCP und der KI-Agent kommen nicht heran.
- Rechte gelten immer so, wie das Konto sie gerade auf der Freigabe hat (`fileRoot`). Ohne Schreibrecht kein Upload, ohne Zugriff keine Antwort.

### Ablauf eines Fotos

1. **Prüfen:** `POST /api/v1/app/photos/check` mit `{root, dir, items:[{sha256, size, path?}]}` (höchstens 500 Einträge, 1 MiB).
   - `root` ist z. B. `share/Fotos`, `dir` der Ordner der Sicherung, etwa `Handy Zeus`. Unterordner zählen mit.
   - `sha256` sind 64 Hex-Ziffern in Kleinbuchstaben, gerechnet über genau die Bytes, die die App hochladen würde.
   - `path` (wahlweise) ist der Ort relativ zu `dir`, an den die App das Foto legen würde, z. B. `2026/11/IMG_1.jpg`.
   - Antwort `{items:[{sha256, state, root?, path?}]}`, `state`:
     - `saved`: liegt schon im Ziel, auch wenn es am Handy umbenannt wurde.
     - `missing`: hochladen.
     - `unsure`: konnte in dieser Anfrage nicht nachgerechnet werden, später noch einmal fragen.
   - **Gesichert** heißt: Der Index des Kontos kennt den Inhalt (Index von irgendeinem Gerät des Kontos, damit ein neu gekoppeltes Handy nicht alles noch einmal hochlädt), in derselben Freigabe unter `dir`, und dort liegt eine gewöhnliche Datei mit derselben Größe und Änderungszeit.
   - Kennt der Index das Foto nicht, liegt unter `path` aber eine Datei gleicher Größe (etwa per SMB kopiert), rechnet das NAS ihre Prüfsumme einmal nach. Stimmt sie, gilt das Foto als gesichert und kommt in den Index.
2. **Ordner:** Jahr und Monat legt die App mit `POST /api/v1/files/mkdir` an, eine Ebene nach der anderen; `409 file_exists` heißt „schon da“.
3. **Hochladen:** `POST /api/v1/files/uploads` mit `conflict: "rename"`, dann `PATCH …/uploads/{id}?offset=…` wie im Web.
   - Bei gleichem Namen mit anderem Inhalt entsteht `IMG_1 (2).jpg`.
   - Abgebrochene Uploads setzt die App fort; nach einem Neustart des NAS findet `GET /api/v1/files/uploads?root&path` sie wieder.
4. **Abschließen:** `POST /api/v1/app/photos/uploads/{id}/finish` mit `{sha256}` statt der normalen Finish-Route.
   - Das NAS bestimmt die Prüfsumme selbst, während die Stücke geschrieben werden. Ging dieser Stand verloren (Schreibfehler, Upload nach einem Neustart fortgesetzt), liest es die fertige Teildatei noch einmal.
   - Weicht die Prüfsumme von der der App ab, wird nichts veröffentlicht: `409 upload_hash_mismatch`. Die App bricht den Upload ab (`DELETE …/uploads/{id}`) und beginnt neu.
   - Antwort `{id, size, name, path, sha256, root, recorded}`. `path` ist der endgültige Ort in der Freigabe. `recorded: false` heißt, die Datei liegt da, der Index ließ sich aber gerade nicht schreiben; der nächste `check` mit `path` findet sie dann.
   - Ohne Volume für Systemdaten wird nichts abgeschlossen: `503 photos_unavailable`.

### Speicher freigeben

`POST /api/v1/app/photos/verify` mit `{items:[{sha256, size}]}` (höchstens 500) direkt bevor die App etwas vom Handy löscht.

- `saved` nur, wenn ein Eintrag im Index in einer Freigabe liegt, die das Konto jetzt erreicht, und die Datei dort eine gewöhnliche Datei mit derselben Größe ist.
- Hat sich ihre Änderungszeit geändert, rechnet das NAS die Prüfsumme nach (höchstens 1 GiB je Anfrage). Was nicht mehr hineinpasst, ist `unsure` und wird nicht gelöscht.
- Antwort wie bei `check`, mit `root` und `path` der bestätigten Datei.
- Gelöscht wird nur am Handy und nur nach Tippen: über `MediaStore.createTrashRequest`, Android fragt noch einmal, alles liegt 30 Tage im Papierkorb des Handys. Aufnahmen der letzten 30 Tage bleiben.

### Ablage am NAS

- `.titanos/apps/<konto>/<gerät>/photos.jsonl` auf dem Volume für Systemdaten (`volumedata.Home`, Funktion `apps`, zieht mit der Vorgabe um wie Kalender und Kontakte). Nie auf dem Stick, nie nur im RAM.
- Ordner root 0700, Datei 0600. Lesen legt nichts an, erst der erste Eintrag.
- Eine Zeile je fertiger Datei (`sha256`, `size`, `root`, `path`, `mtime`, `at`), angehängt und mit `fsync`. So schreibt die erste Sicherung von 10 000 Fotos nicht 10 000-mal den ganzen Index. Mehrfache Zeilen für denselben Ort fasst das NAS gelegentlich zusammen (atomar neu geschrieben). Eine beim Absturz abgeschnittene letzte Zeile wird übergangen.
- Wird das Konto gelöscht, geht sein Ordner mit. Beendet man nur ein Gerät, bleibt sein Index: ein neu gekoppeltes Handy erkennt damit, was schon gesichert ist.
- **Fehler:**

  | Status | Code | Bedeutung |
  |---|---|---|
  | 403 | `app_session_required` | keine Sitzung einer gekoppelten App |
  | 503 | `photos_unavailable` | kein Volume für Systemdaten |
  | 409 | `upload_hash_mismatch` | Prüfsumme der hochgeladenen Datei passt nicht, nichts veröffentlicht |
  | 400 | `invalid_checksum` | Prüfsumme fehlt oder ist kein SHA-256 |
  | 400 | `photos_too_many` | mehr als 500 Einträge |

- **Prüfstand (19.11.2026):** Go-Tests `TestAppPhotoBackup`, `TestAppPhotoRefusals` (Upload aus der App-Sitzung bis zum Index, falsche Prüfsumme, „(2)“, umbenannt, per SMB vorhanden, neu gekoppeltes Gerät, Änderungszeit/Inhalt geändert, Rechte entzogen, ohne Volume), `TestFinishUploadHashed*` und die Unit-Tests von `appphotos`.
  - **Emulator** (Android 17, App-Testfassung 0.02) gegen einen Wegwerf-Testserver mit Freigabe auf einem temporären Volume (wie die Go-Tests, hinter einem HTTPS-Vorbau): Upload in `Ordner/2026/11/`, Prüfsummen auf dem NAS gleich dem Original, eine Index-Zeile je Datei, umbenannte Kopie übersprungen, neues Foto über den Content-Auslöser hochgeladen.
  - Nicht geprüft: echtes NAS mit echtem Volume (`titand -dev` hat keine Freigaben), echtes Handy, „Speicher freigeben“ mit echten Kandidaten (keine Aufnahme älter als 30 Tage im Emulator).

## Mitteilungen

Die App bekommt dieselben Meldungen wie Web Push im Browser, ohne Google und ohne Firebase (TECHNIK §5, Entwürfe 70–72). Am NAS sammelt das Paket `appnotify` sie im RAM; der Spiegel sitzt in `webpush.Manager` (`SetMirror`), also an der Stelle, an der jede Push-Mitteilung entsteht, auch wenn das Konto keinen Browser angemeldet hat. Die Routen erreicht **nur die Sitzung einer gekoppelten App** (`403 app_session_required`, nicht in `publicRoutes`, nicht im Werkzeugkatalog), wie die Foto-Sicherung.

- **Was ankommt** (`category`):
  - `nas`: was die Glocke an die Wege schickt (ab „Verschicken ab“, mit der täglichen Sammelmitteilung und dem Bericht über das NAS), nur für Konten, die beim Abruf Besitzer oder Administrator und nicht gesperrt sind.
  - `mail`: neue Mail in Mail (`SendMail`), mit Absender und Betreff nur, wenn Web Push sie auch zeigen dürfte.
  - `calendar`: Erinnerungen an Termine, wenn das Konto im Kalender „Auch als Mitteilung an meine Geräte (Push)“ eingeschaltet hat (`reminderPush`, ab Werk aus; wie Web Push).
  - `agent`: Antworten, Rückfragen, Aufträge, Notfallzugang und Ransomware-Wächter des KI-Agenten (`SendTo`).
  - `nvr`: Meldungen einer Kamera an die Konten, die dort als Empfänger mit Push gewählt sind (`SendCamera`).
  - Die Schalter je Gerät für Web Push gelten nicht für die App; sie hat eigene Schalter je Art (Entwurf 72) und Android-Kanäle.
- **Abruf:** `GET /api/v1/app/notifications?since=<marke>`.
  - Antwort `{epoch, latest, reset, items:[{id, time, category, severity, title, text, tag?, link?, app}]}`, älteste zuerst.
  - `id` und `latest` sind Marken `<epoch>-<nummer>`. Die App merkt sich `latest` und fragt damit das nächste Mal.
  - Nach einem Neustart von titand gibt es eine neue Epoche. Eine Marke einer anderen Epoche (oder eine, die nicht passt) liefert alles Gehaltene mit `reset: true`; die App zeigt dann nichts davon als neue Mitteilung an, nimmt es nur in die Liste.
  - `severity` ist `info`, `warning` oder `error` (NAS-Meldungen; sonst `info`). `tag` ersetzt am Handy eine ältere Mitteilung mit demselben Tag, wie im Browser. `link` ist die Seite in TitanOS (`/mail`, `/agent?chat=…`). `app` ist `titanos` oder `agent` (`webpush.AppOf`).
- **Dauerverbindung:** `GET /api/v1/app/notifications/stream?since=<marke>` (Server-Sent Events).
  - Zuerst `ready` `{epoch, latest, reset}`, dann was seit `since` kam, dann jede neue Meldung als `notification` mit `id: <marke>`.
  - Alle 90 Sekunden ein Kommentar `: keep-alive`, damit Heimrouter die Verbindung nicht schließen. Die App setzt ihre Lesezeit auf mehr als das Doppelte.
  - `resync` `{latest}`: Meldungen passten nicht mehr in den Puffer (64); die App liest die Liste.
  - Höchstens 8 Ströme je Konto, ein neuer beendet den ältesten. Der Strom endet mit der Anmeldung (Gerät beendet, Konto gesperrt oder gelöscht).
- **Grenzen:** höchstens 200 Meldungen je Konto und 200 an alle Administratoren, keine älter als 7 Tage. Nur RAM: nichts auf dem Stick, nichts auf einem Volume; nach einem Neustart von titand ist die Liste am NAS leer, die App behält ihren Stand. Gelöscht wird ein Konto: seine Meldungen gehen mit.
- **Gelesen** hält die App nur für sich; das NAS kennt keine Gelesen-Marke je Gerät.
- **UnifiedPush** (ntfy & Co.) kommt später: Web Push geht nur an die bekannten Push-Dienste der Browser (`docs/rules/SYSTEM.md`). Ein Verteiler hat eine beliebige, oft eigene Adresse, auch im Heimnetz; das zu erlauben ist eine Entscheidung des Inhabers. In der App steht der Schalter deshalb als „kommt später“.
- TECHNIK §5 nannte den Strom `/api/v1/app/events`; gebaut ist `/api/v1/app/notifications/stream`.
- **Prüfstand (20.11.2026):** Go-Tests `TestAppNotifications`, `TestAppNotificationStream` (ohne Browser-Abo, fremdes Konto, Marke, alte Epoche, Browser 403, Strom mit Rückstand, Live-Meldung und Herzschlag) und die Unit-Tests von `appnotify` (Arten, Administratoren beim Abruf, Grenzen, Puffer, Ströme je Konto).
  - **Emulator** (Android 17, App-Testfassung 0.03 vor dem Release) gegen `titand -dev` hinter einem HTTPS-Vorbau: Koppeln, Schalter „Mitteilungen“ beim Einrichten mit Berechtigung und Akku-Ausnahme, Vordergrunddienst `specialUse` verbunden, „Bericht über das NAS jetzt senden“ kommt sofort als Android-Mitteilung (Kanal NAS) mit „Ansehen“ (öffnet die Liste) und „Gelesen“; nach einem Neustart von titand (neue Epoche) verbindet die App von selbst neu und bekommt die nächste Meldung; nach einem App-Update läuft der Dienst wieder.
  - Nicht geprüft: echtes Handy, Doze über Stunden, Neustart des Handys (BOOT_COMPLETED), Mail, Kalender, KI-Agent und NVR als Quelle am echten NAS.

## Kontakte und Kalender (1.19)

Die App trägt ein Android-Konto „TitanOS“ in Kontakte und Kalender des Handys ein (SyncAdapter, TECHNIK §4) und gleicht in beide Richtungen ab. Sie nimmt dafür **nicht** das CalDAV/CardDAV mit Gerätepasswort, sondern die Sitzung der App und eigene Routen, die **nur die Sitzung einer gekoppelten App** erreicht (`403 app_session_required`, nicht in `publicRoutes`, nicht im Werkzeugkatalog, nur dieser Knoten), wie Foto-Sicherung und Mitteilungen.

- **Warum nicht die Routen der Webseiten:** `GET /calendar/events` liefert die Vorkommen eines Zeitfensters (höchstens 400 Tage) in einer eigenen Form – Wiederholungen nur als einfache Regel (`recurUnsupported` für den Rest), ohne EXDATE, ohne RECURRENCE-ID-Ausnahmen, ohne VTIMEZONE, ohne fremde Eigenschaften, ohne Änderungsmarke. Ein Abgleich darauf verlöre, was ein iPhone oder ein anderes CalDAV-Programm geschrieben hat. Die Routen hier sind ein dünner Spiegel von `davsync`: derselbe Speicher (`pim/store`), dieselbe Vorbereitung vor dem Schreiben (`davsync.PrepareEntry`, das Bild einer Karte wird verkleinert), ETags als `If-Match`.
- **Routen** (`api/apppim.go`), `{kind}` ist `calendar`, `contacts` oder `tasks` (Erinnerungen, VTODO):
  - `GET /api/v1/app/pim/{kind}` → `{collections:[{id, name, color, order, description?, ctag, syncToken, count, readOnly, virtual?}]}`.
  - `GET /api/v1/app/pim/{kind}/{col}/changes?token=&limit=` → `{token, truncated, changes:[{name, etag?, deleted?}]}`: ohne Marke jeder Eintrag, sonst was sich seither geändert hat (höchstens 1000 je Antwort, `truncated` heißt: mit `token` weiterfragen). Eine Marke einer anderen Epoche oder älter als das Protokoll ist `410 pim_sync_token`: die App fängt ohne Marke neu an.
  - `POST /api/v1/app/pim/{kind}/{col}/objects` `{names:[…]}` (höchstens 100) → `{objects:[{name, etag, data}], missing:[…]}` mit den gespeicherten Bytes.
  - `PUT /api/v1/app/pim/{kind}/{col}/objects/{name}` mit dem Eintrag als Inhalt (`text/calendar` bzw. `text/vcard`, UTF-8): `If-None-Match: *` legt nur neu an, `If-Match: "<etag>"` ersetzt nur diesen Stand, sonst `412 pim_conflict`. Antwort `{etag, created, rewritten}` und `ETag`; `rewritten`: das NAS hat andere Bytes gespeichert (Bild verkleinert), die App holt den Eintrag neu. Dieselbe UID unter anderem Namen ist `409 pim_uid_conflict` mit dem Namen des vorhandenen Eintrags in `X-Conflict-Name`.
  - `DELETE /api/v1/app/pim/{kind}/{col}/objects/{name}` mit `If-Match`.
  - Fehler: `404 pim_not_found`, `400 pim_invalid`/`pim_unsupported`/`pim_invalid_name`, `413 pim_too_large`, `415 pim_type`, `507 pim_full`, ohne Volume `503 calendar_unavailable`/`contacts_unavailable`.
- **In der App** (`feature-pim`, Zweig `feat/1.00-pim` im App-Repo): ein Android-Konto je Kopplung („konto@nas“, Typ `io.github.ra5on.titanos`, ohne Passwort; „Kopplung aufheben“ nimmt es samt Kontakten und Terminen vom Handy, am NAS bleibt alles). Kalender: ein Eintrag ist eine Hauptzeile (`_SYNC_ID` = Name, `SYNC_DATA1` = ETag) mit RRULE/RDATE/EXDATE/DURATION und Ausnahmen (`ORIGINAL_SYNC_ID`); Kontakte sind RawContacts (`SOURCE_ID`, `SYNC1` = ETag, `SYNC2` = UID), Gruppen Groups mit Mitgliedschaften. Beim Hochladen holt die App den gespeicherten Eintrag und ändert nur die Eigenschaften, die Android anders hat; alles andere (X-Eigenschaften, VTIMEZONE, Teilnehmer, absolute Erinnerungen, vCard-Gruppen) bleibt. Konflikt (`412`): ab Werk gewinnt das NAS, wählbar das Handy. Erinnerungen (VTODO) bleiben in der App: Android hat keine allgemeine Schnittstelle für Aufgaben.
- **Live:** Jede Änderung über diese Routen ist eine Änderung des Speichers und erscheint im Strom `/calendar/stream` der Webseiten und bei CalDAV-Geräten wie eine Änderung vom iPhone.
- Kalender anlegen, umbenennen, Farbe und Gruppen, Zusammenführen und alles Übrige laufen über die Routen der Webseiten (`browserOnly` lässt die App-Sitzung durch).
- **Prüfstand (20.11.2026):** Go-Tests `TestAppPIMSync` (Browser 403, ohne Anmeldung 401, Anlegen nur mit `If-None-Match`, Bytes unverändert mit RRULE/EXDATE/Ausnahme/zwei Erinnerungen, Änderungen ab Marke, falsche Marke 410, Multiget mit fehlendem Eintrag, veraltetes/schwaches ETag, falsche Daten/Art/Sammlung, Löschen mit ETag, Karte und UID-Konflikt, Erinnerungen) und `TestAppPIMOtherAccount` (Sammlungen eines anderen Kontos weder gelistet noch lesbar).
  - **Emulator** (Android 17, App `feat/1.00-pim`) gegen einen Wegwerf-Testserver mit Kalender-/Kontaktspeicher wie in den Go-Tests (hinter einem HTTPS-Vorbau; `titand -dev` hat kein Systemdaten-Volume): Koppeln, Konto „alex@pc“ in den Android-Konten, erster Abgleich mit Serie (RRULE, EXDATE, Ausnahme), ganztägigem Termin, zwei Kalendern, Kontakten und Gruppe in die Provider (`content query` geprüft). Vom Handy (als normale App über den Provider): geänderter Titel/Ort einer Serie (X-APPLE-Zeile, Ausnahme und VALARM blieben, SEQUENCE+1), neuer Termin mit VTIMEZONE, neue Ausnahme einer Instanz (neues VEVENT mit RECURRENCE-ID), abgesagte Instanz (EXDATE), gelöschter Termin, neuer Kontakt mit Gruppe, gelöschter Kontakt – alles am NAS angekommen (Export geprüft). Live: ein Termin, im Web angelegt, war 1 s später über `/calendar/stream` am Handy. Die Kontakte-App von Google zeigt die Kontakte des Kontos und bietet „Bearbeiten“ mit den Feldern aus `CONTACTS_STRUCTURE`; ein dort geänderter Vorname kam beim nächsten Abgleich am NAS an (übrige vCard-Zeilen wie X-SOCIALPROFILE blieben).
  - Nicht geprüft: ob Android das Hochladen nach einer Änderung in der Kontakte-App von Google von selbst anstößt (in der Probe kam sie mit „Jetzt abgleichen“ an; zwischendurch lief auf dem geteilten Emulator eine andere Fassung der App), die Kalender-App von Google (zeigt ohne Google-Konto keine Kalender an), echtes Handy, echtes NAS.

## Arbeitsplatz (1.19)

Die App zeigt am Monitor (Android-Desktop-Modus) und in breiten Fenstern ab 1280 dp denselben Arbeitsplatz wie das Web (Fenster, Dock, Widgets, Symbole, Hintergrund; App-Zweig `feat/1.00-workspace`, Modul `feature-workspace`). Einstellung in der App: Automatisch (ab Werk), Immer, Nie.

- **Dieselbe Einrichtung wie im Web:** gelesen aus `GET /api/v1/preferences` › `desktop`, geschrieben mit `PUT /api/v1/preferences/desktop` (Form wie `web/src/lib/desktop/prefs.ts`). Die App schreibt erst, wenn sie gelesen hat, 1,5 s nach der letzten Änderung, und ersetzt im Stand des NAS nur die Felder, die sie geändert hat (vorher neu gelesen): Felder, die die App nicht kennt (`phone`, `nvr`, `lockMinutes` …), und Änderungen aus dem Web bleiben. Symbole und Programme im Dock, die die App nicht öffnen kann, bleiben in den Listen.
- **Eigene Einrichtung für Android:** Schalter in der App. An: die Einstellung `androiddesktop` (neu in 1.19, dieselbe Form wie `desktop`, das Web schreibt sie nie); beim ersten Mal beginnt sie mit der Einrichtung des Webs. Ein NAS vor 1.19 antwortet auf `PUT /preferences/androiddesktop` mit `404`; die App behält sie dann auf dem Gerät.
- Offene Fenster bleiben auf dem Gerät (wie im Web im Browser).
- **Prüfstand (22.11.2026):** Go-Test `TestAndroidDesktopIsStoredApartFromTheWebDesktop` (beide Einstellungen getrennt gespeichert, nur Objekte angenommen), `TestPreferences` mit der neuen Einstellung.

## VPN (1.19)

Die App „TitanOS“ hat einen eigenen VPN-Dienst mit WireGuard (PLAN 116, Inhaber 21.11.2026: „die App legt je Handy selbst einen WireGuard-Zugang an, sichtbar unter WireGuard, endet mit ‚Beenden‘; Tailscale bleibt möglich“). Das NAS stellt dafür den Zugang je gekoppeltem Gerät bereit; die Routen erreicht **nur die Sitzung einer gekoppelten App** (`403 app_session_required`, nicht in `publicRoutes`, nicht im Werkzeugkatalog, nur dieser Knoten), wie Foto-Sicherung und Mitteilungen.

- **Schlüssel:** Das Schlüsselpaar (X25519) entsteht am Handy, der private Schlüssel verlässt es nie. Das NAS speichert den öffentlichen Schlüssel, einen eigenen Preshared Key und die Adresse wie bei jedem Gerät unter Einstellungen › WireGuard (`/config/wireguard/server.json`, zusätzlich `account` und `device`, `docs/WIREGUARD.md`).
- **Routen** (`api/appvpn.go`), alle mit `Cache-Control: no-store`:
  - `GET /api/v1/app/vpn` → `{available, reason?, mayCreate, peer, server}`.
    - `peer` ist `null` oder `{name, address, publicKey, created, active, lastHandshake?, received, sent}`; `active: false` heißt: gerade nicht im Tunnel.
    - `server`, solange WireGuard an ist und das Konto Geräte anlegen darf (`mayCreate`) oder dieses Gerät schon einen Zugang hat, sonst `null` (Sicherheitsdurchsicht 22.11.2026): `{publicKey, port, network, address, homeNetworks, dns, fullTunnel, keepalive, endpoint, endpoints, nasAddresses, nas:{server, spki}}`.
      - `endpoints`: alle Adressen aus dem Internet, die eingetragene zuerst, dann die Namen aus Dynamisches DNS und Let's Encrypt.
      - `nasAddresses`: die IPv4-Adresse des NAS im Heimnetz. Mit „nur NAS“ leitet die App sie als /32 durch den Tunnel, damit die gekoppelten Heimnetz-Adressen beider Apps unterwegs gehen.
      - `nas`: das NAS im Tunnel als Adresse der App (`<address>[:Port]`, SPKI wie bei der Kopplung für IP-Adressen).
      - `dns`: das DNS-Feld der WireGuard-Seite. Das NAS hat keinen eigenen DNS-Dienst.
    - `reason`: `wireguard_off`, `wireguard_no_endpoint` oder `wireguard_admin_only`.
  - `POST /api/v1/app/vpn` mit `{publicKey}` (base64, 32 Bytes) → wie GET, dazu einmal `presharedKey`. Gibt es schon einen Zugang dieses Geräts, bekommt er den neuen Schlüssel und einen neuen Preshared Key an derselben Adresse („neu erzeugen“).
  - `DELETE /api/v1/app/vpn` → `{removed}`.
- **Fehler:**

  | Status | Code | Bedeutung |
  |---|---|---|
  | 409 | `wireguard_off` | WireGuard am NAS nicht eingerichtet oder aus (Einstellungen › WireGuard) |
  | 409 | `wireguard_no_endpoint` | keine Adresse aus dem Internet (WireGuard-Seite oder Dynamisches DNS) |
  | 403 | `wireguard_admin_only` | Konto ist weder Besitzer noch Administrator |
  | 400 | `invalid_key` | kein WireGuard-Schlüssel |
  | 409 | `wireguard_key_used` | ein anderes Gerät hat den Schlüssel |
  | 409 | `wireguard_too_many` | 50 Geräte erreicht |
  | 401 | `app_unknown` | Gerät beendet oder abgelaufen |

- **Wer darf:** Anlegen nur Besitzer und Administratoren, wie jedes WireGuard-Gerät. Der Zugang erreicht über NAT das ganze Heimnetz, mehr als die Rolle Benutzer sonst darf. Lesen und Entfernen des eigenen Zugangs darf jede gekoppelte App.
- **Ende:** Der Zugang hängt am Gerät. Er geht mit:
  - „Beenden“ unter Mein Konto › Geräte;
  - neuem Passwort oder 2FA;
  - gelöschtem oder gesperrtem Konto;
  - Rolle ohne Verwaltungsrecht, auch über den Cluster.

  Abgelaufen nach „Angemeldet bleiben“: stündliche Prüfung nur im RAM, der Schlüssel wird vom Tunnel genommen, gelöscht wird beim nächsten Start. Aktivitätsprotokoll `app.vpn` (Anlegen, neu erzeugen, auch abgelehnt) und `app.vpn.remove`.
- **Die Agent-App** legt keinen eigenen Zugang an. Ihre Verbindungen laufen durch den systemweiten Tunnel der TitanOS-App.
- **Prüfstand (22.11.2026):**
  - Go-Tests (`api/appvpn_test.go`, `wgserver/apppeers_test.go`), auch mit `-race`.
  - **Emulator** (Android 17, App `feat/1.00-vpn`) gegen einen Wegwerf-Testserver mit TLS und eine WireGuard-Gegenstelle aus wireguard-go im Userspace (gVisor-netstack, ein UDP-Socket am Host, keine Schnittstelle; liest `server.json` und `server.key` des Testservers, TCP im Tunnel geht an den Testserver). Geprüft:
    - Zugang anlegen: der Schlüssel entsteht am Handy, Handshake, Verkehr.
    - Die App spricht über `10.253.0.1:8443` im Tunnel mit dem NAS, SPKI gepinnt.
    - Mit „nur NAS“ führt Android nur die NAS-Adressen über `tun0`.
    - Automatisch: aus im eingetragenen Heim-WLAN, an über Mobilfunk, wieder aus im WLAN.
    - Von Hand, Kachel, Mitteilung mit Datenmenge, Always-on von Android an und aus.
    - Zugang entfernen und neu anlegen: am Testserver weg und wieder da, der Tunnel kommt von selbst.
    - „Neu erzeugen“: neuer Schlüssel an derselben Adresse, der Tunnel läuft danach weiter.
    - Entkoppeln: der Zugang verschwindet am NAS, Schlüssel und Einstellungen am Handy sind weg.
  - Nicht geprüft:
    - echter Tunnel über `titanoswg` und `wg syncconf`; im Testserver ist der WireGuard-Manager `Disabled`;
    - dass ein abgelaufener Zugang live aus dem Tunnel fällt (nur Unit-Tests);
    - die Heimnetz-Adresse des NAS im Tunnel (die Gegenstelle bediente nur `10.253.0.1`), damit auch die Agent-App durch den Tunnel;
    - Routing „Heimnetz“ und „Alles“, DNS über das NAS, Wahl der Adresse, MTU und Keepalive im Betrieb;
    - das Lernen der Heim-WLANs im Emulator;
    - Netzwechsel und Start aus dem Hintergrund bei geschlossener App (Mitteilung zum Antippen);
    - echtes Handy, echtes NAS.

## Prüfstand (19.11.2026)

- **Go-Tests:** `TestAppPairingAndSession`, `TestAppPairingRefusals`, `TestAppSessionRefusals`, `TestAppDeviceEndsWithStay` sowie die Unit-Tests von `appdevices`.
- **Durchgespielt** mit `titand -dev` (nur HTTP) und einem Programm, das das Handy spielt: Koppeln im Browser am Handy-Format, Bestätigen mit Passwort, Sitzung über die Herausforderung, Anzeige unter „Angemeldete Geräte“, „Beenden“.
- **Emulator** (Android 17, Testfassung 0.01, Debug- und signierte Release-APK): Adresse von Hand über HTTPS mit selbst signiertem Zertifikat (Vorbau vor `titand -dev`), Fingerabdruck stimmt, Kopplung mit Code, Bestätigen im Browser, Startseite mit echten Daten, neue Sitzung von selbst nach einem Neustart von titand.
- **Nicht geprüft:** echtes Handy, echtes NAS, QR-Kamera, Tailscale, Dateien mit Freigaben (das Test-NAS hat keine).
