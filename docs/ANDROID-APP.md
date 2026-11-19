# Android-App: Kopplung und Anmeldung (PLAN 116)

Die Android-Apps „TitanOS“ (`io.github.ra5on.titanos`) und „Agent“ (`io.github.ra5on.titanos.agent`) liegen im Repo `ra5on/TitanOS-Android`. Dieses Dokument beschreibt, was das NAS dafür bereitstellt. Technikplan und Entwürfe stehen in `Entwuerfe/116-android/`, außerhalb des Repos.

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

## Prüfstand (19.11.2026)

- **Go-Tests:** `TestAppPairingAndSession`, `TestAppPairingRefusals`, `TestAppSessionRefusals`, `TestAppDeviceEndsWithStay` sowie die Unit-Tests von `appdevices`.
- **Durchgespielt** mit `titand -dev` (nur HTTP) und einem Programm, das das Handy spielt: Koppeln im Browser am Handy-Format, Bestätigen mit Passwort, Sitzung über die Herausforderung, Anzeige unter „Angemeldete Geräte“, „Beenden“.
- **Emulator** (Android 17, Testfassung 0.01, Debug- und signierte Release-APK): Adresse von Hand über HTTPS mit selbst signiertem Zertifikat (Vorbau vor `titand -dev`), Fingerabdruck stimmt, Kopplung mit Code, Bestätigen im Browser, Startseite mit echten Daten, neue Sitzung von selbst nach einem Neustart von titand.
- **Nicht geprüft:** echtes Handy, echtes NAS, QR-Kamera, Tailscale, Dateien mit Freigaben (das Test-NAS hat keine).
