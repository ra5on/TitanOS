# README-Screenshots · TitanOS 0.6.1

Am 20. Oktober 2026 aus der unveränderten Weboberfläche des Tags `v0.6.1` aufgenommen. Die API-Antworten kommen aus isolierten Beispieldaten in `scripts/test-ui-061.mjs`; Konten, Aufträge, Messwerte und Container sind keine produktiven NAS-Daten. Die Bilder sind Browseraufnahmen, keine Entwürfe.

- Desktop: Chromium, 1440 × 1000 Pixel, Deutsch, Theme Graphit.
- Smartphone: 390 Pixel Breite, Deutsch, Theme Hell, vollständige Seite.
- Version in den Beispieldaten: 0.6.1.
- Ansichten: Einstellungen, Sicherungen, Wiederherstellung, VMs, laufende Aufgaben und Docker.

Vom Repository-Stamm aus den Vite-Server starten:

```sh
npm --prefix web run dev -- --host 127.0.0.1 --port 5179
```

In einem zweiten Terminal mit installiertem Playwright und Chromium aufnehmen:

```sh
README_SCREENSHOTS=1 UI_OUTPUT=docs/screenshots/v0.6.1 \
  node scripts/test-ui-061.mjs
```

Bei separat installiertem Playwright bzw. Browser können `PLAYWRIGHT_MODULE` (absoluter Modulpfad) und `BROWSER_EXECUTABLE` (absoluter Chromium-Pfad) gesetzt werden. Für einen anderen lokalen Server `UI_URL` setzen. Die Aufnahmeroutine greift auf keine realen NAS-Datenträger zu.
