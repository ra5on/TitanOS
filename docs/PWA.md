# TitanOS als Handy-App (PLAN 70, seit 0.66.0)

TitanOS ist eine installierbare Web-App (`web/static/manifest.webmanifest`, Service Worker `web/static/sw.js`).

## Installieren

- **Mein Konto › „TitanOS als App“:** Chrome und Edge (Android, Desktop) bieten „Installieren“ über `beforeinstallprompt` an (`lib/pwa-install.svelte.ts`, früh im Layout geladen). Läuft TitanOS schon als App (`display-mode: standalone`), steht dort „Als App installiert“.
- **iOS/Safari:** Es gibt nur den Weg „Teilen › Zum Home-Bildschirm“, die Karte erklärt das. Ein Teilen-Ziel unterstützt iOS für Web-Apps nicht.
- **Kurzbefehle:** Langes Drücken auf das App-Symbol bietet Dateien, Docker und Systemsteuerung an (`shortcuts`).

## Teilen aufs NAS

- Das Manifest meldet ein **Teilen-Ziel** (`share_target`): POST `multipart/form-data` an `/share-target` mit `files` (alle Typen) sowie `title`, `text` und `url`.
- Der **Service Worker** nimmt diesen POST selbst an und sendet nichts. Er legt die Dateien und eine Liste im Cache Storage `titanos-share` dieses Browsers ab und leitet auf `/share` weiter. API und Oberfläche cacht er weiter nicht.
- **`/share`** (Seite ohne Programm, nur angemeldet) zeigt die Dateien mit Größe und den geteilten Text. Der Text wird ab Werk als `geteilt-<zeit>.txt` gespeichert, abschaltbar.
  - Ein Ordner wird gewählt; der Ordner-Dialog der Web-Apps zeigt im Modus `folder` nur beschreibbare Orte. Der zuletzt gewählte Ordner wird im Browser gemerkt.
  - Hochgeladen wird mit der Upload-Warteschlange des Dateimanagers (`lib/uploads.svelte.ts`): dieselben Rechte und Namensregeln, bei vorhandenem Namen ein neuer Name (`conflict: rename`).
  - Danach löscht die Seite den Cache und bietet „In Dateien zeigen“. „Verwerfen“ löscht ihn ebenfalls.
- **Ohne aktiven Service Worker** (erste Sekunden nach der Installation) erreicht der POST titand. titand liest ihn nicht und leitet auf `/share?missing=1` weiter (Hinweis „nicht angekommen“). Andere POSTs an die Oberfläche bleiben 405.

## Grenzen

- Nur über HTTPS (oder localhost), weil Service Worker sonst nicht laufen. Ohne Zertifikat (Let's Encrypt, `docs/LETSENCRYPT.md`) bietet der Browser weder Installieren noch Teilen an.
- Sehr große Dateien liegen bis zum Hochladen ganz im Speicher des Browsers; das Handy kann sie verweigern.
- Getestet in Chromium (Playwright) mit nachgebildeter API und vorbereitetem Cache; nicht auf echten Android-Geräten.
