# Mitteilungen auf Handy und Browser (Web Push)

Seit 0.34.0 kann TitanOS seine Meldungen als echte Mitteilungen an Browser
und an die Web-App auf dem Home-Bildschirm schicken: Sperrbildschirm,
Mitteilungszentrale, auch wenn TitanOS gerade nicht offen ist.
Einstellungen › Benachrichtigungen › **Mitteilungen auf diesem Gerät**, nur
Besitzer und Administratoren (wie die Glocke).

## Voraussetzungen

- **HTTPS mit gültigem Zertifikat.** Browser erlauben Service Worker und Push
  nur dann. Am einfachsten über den Tailnet-Namen
  (`https://<name>.<tailnet>.ts.net`, siehe `docs/TAILSCALE.md`), sonst Let's
  Encrypt oder ein eigenes Zertifikat. Unter der IP-Adresse oder `.local` mit
  dem selbst signierten Zertifikat geht es nicht; die Seite sagt das.
- **iPhone und iPad (ab iOS/iPadOS 16.4):** nur für TitanOS auf dem
  Home-Bildschirm. In Safari „Teilen“ › „Zum Home-Bildschirm“, TitanOS von dort
  öffnen und „Mitteilungen einschalten“ tippen.
- **Internet für das NAS:** Die Meldung geht vom NAS an den Push-Dienst des
  Browsers (Apple `web.push.apple.com`, Google `fcm.googleapis.com`, Mozilla,
  Microsoft) und von dort aufs Gerät. Das Gerät muss das NAS dafür nicht
  erreichen; Tailscale braucht es nur zum Öffnen von TitanOS.

## Was ankommt

Dieselben Meldungen wie in der Glocke, ab der Stufe unter „Verschicken ab“
(wie E-Mail, ntfy usw.), Titel mit dem Namen des NAS. Fehler werden mit
hoher Dringlichkeit geschickt. Ein Tippen öffnet TitanOS. Jedes Gerät hat
„Testmitteilung“ und „Entfernen“; die Liste zeigt nur die eigenen Geräte.

## App-Symbol

Einstellungen › Darstellung › **App-Symbol** (nur Besitzer und Administratoren,
für das ganze NAS): Standard, das Logo auf einer eigenen Farbe (dunkles oder
weißes „T“, je nach Kontrast) oder ein eigenes Bild (JPEG/PNG/WebP bis 5 MiB,
mindestens 64 × 64 Pixel, mittig quadratisch zugeschnitten, auf Schwarz
gelegt, neu kodiert ohne Metadaten). titand zeichnet daraus
`/icons/app-180|192|512.png` selbst; ohne Wahl antworten die eingebauten
Dateien. Die Dateien sind ohne Anmeldung abrufbar (Home-Bildschirme holen sie
ohne). Ablage `/config/system/app-icon.json` und `app-icon.png`, nur beim
Speichern geschrieben. iOS übernimmt das Symbol nur beim Hinzufügen zum
Home-Bildschirm und zeigt es auch in Mitteilungen.

## Technik

- Paket `webpush`: RFC 8030 (Zustellung), RFC 8291 (Verschlüsselung,
  `aes128gcm`), RFC 8292 (VAPID, ES256), nur Standardbibliothek.
- `/config/notify/webpush.json`: VAPID-Schlüssel des NAS (beim ersten Öffnen
  der Seite erzeugt) und die Geräte (Endpunkt, Schlüssel des Browsers, Konto,
  Name). Geschrieben nur beim Hinzufügen und Entfernen eines Geräts bzw. wenn
  der Push-Dienst ein Gerät nicht mehr kennt (404/410, dann entfernt). Der
  Ordner `notify/` ist im Stick-Dateimanager ausgeblendet und nicht im
  Diagnosepaket.
- Endpunkte nur per HTTPS bei den bekannten Push-Diensten (Hostname gleich
  oder unter `push.apple.com`, `fcm.googleapis.com`,
  `push.services.mozilla.com`, `notify.windows.com`, Port 443, keine
  Weiterleitungen): Die Adresse kommt aus dem Browser und darf das NAS nicht
  an beliebige Ziele schicken lassen.
- Höchstens 20 Geräte je Konto, 100 insgesamt. Gesendet wird nur an Konten,
  die Besitzer oder Administrator und nicht gesperrt sind.
- Service Worker `/sw.js` zeigt nur Mitteilungen und cacht nichts; Manifest
  `/manifest.webmanifest`, Symbole unter `/icons/`.
- API: `GET`/`POST /api/v1/notifications/push`,
  `DELETE /api/v1/notifications/push/{id}`,
  `POST /api/v1/notifications/push/{id}/test`.

## Prüfstand

Verschlüsselung mit Entschlüsselung wie im Browser und VAPID-Signatur gegen
einen nachgebauten Push-Dienst geprüft (`webpush_test.go`), dazu Rechte,
Gerätegrenze, abgelaufene Geräte und fremde Endpunkte. **Nicht geprüft:**
echte Zustellung über Apple, Google, Mozilla und Microsoft und Safari auf dem
iPhone.
