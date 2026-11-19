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

## Prüfstand (19.11.2026)

- **Go-Tests:** `TestAppPairingAndSession`, `TestAppPairingRefusals`, `TestAppSessionRefusals`, `TestAppDeviceEndsWithStay` sowie die Unit-Tests von `appdevices`.
- **Durchgespielt** mit `titand -dev` (nur HTTP) und einem Programm, das das Handy spielt: Koppeln im Browser am Handy-Format, Bestätigen mit Passwort, Sitzung über die Herausforderung, Anzeige unter „Angemeldete Geräte“, „Beenden“.
- **Emulator** (Android 17, Testfassung 0.01, Debug- und signierte Release-APK): Adresse von Hand über HTTPS mit selbst signiertem Zertifikat (Vorbau vor `titand -dev`), Fingerabdruck stimmt, Kopplung mit Code, Bestätigen im Browser, Startseite mit echten Daten, neue Sitzung von selbst nach einem Neustart von titand.
- **Nicht geprüft:** echtes Handy, echtes NAS, QR-Kamera, Tailscale, Dateien mit Freigaben (das Test-NAS hat keine).
