# README-Screenshots · TitanOS 0.7.1

Am 21. Oktober 2026 aus der Weboberfläche von 0.7.1 aufgenommen. Die API-Antworten kommen aus isolierten Beispieldaten in `scripts/test-files-ui.mjs`; Konten, Dateien und Aufträge sind keine produktiven NAS-Daten. Die Bilder sind Browseraufnahmen, keine Entwürfe.

- `dateien.png`: Chromium, 1440 × 1000 Pixel, Deutsch, Theme Graphit, zwei ausgewählte Dateien mit offenem Kontextmenü.
- `werkzeuge.png`: Ausschnitt mit geöffnetem Menü „Werkzeuge“.
- `dateien-mobil.png`: 390 Pixel Breite, Deutsch, Theme Hell, eine ausgewählte Datei.

Vom Repository-Stamm aus den Vite-Server starten:

```sh
npm --prefix web run dev -- --host 127.0.0.1 --port 5179
```

In einem zweiten Terminal mit installiertem Playwright und Chromium aufnehmen (die Browserprüfung läuft dabei vollständig mit):

```sh
README_SCREENSHOTS=docs/screenshots/v0.7.1 node scripts/test-files-ui.mjs
```

Bei separat installiertem Playwright bzw. Browser können `PLAYWRIGHT_MODULE` (absoluter Modulpfad) und `BROWSER_EXECUTABLE` (absoluter Chromium-Pfad) gesetzt werden. Die Aufnahmeroutine greift auf keine realen NAS-Datenträger zu.
