# README-Screenshots · TitanOS 0.7.2

Am 21. Oktober 2026 aus der Weboberfläche von 0.7.2 aufgenommen. Die API-Antworten kommen aus isolierten Beispieldaten in `scripts/test-ui-vms.mjs`; VMs, Adressen und Geräte sind keine produktiven NAS-Daten. Die Bilder sind Browseraufnahmen, keine Entwürfe. Der Bildschirm zeigt keine VM, weil in der Prüfung keine läuft.

- `vms.png`: Chromium, 1440 × 1000 Pixel, Deutsch, Theme Graphit, Reiter „Übersicht“ einer laufenden Windows-VM.
- `vms-bildschirm.png`: Ausschnitt mit geöffnetem Menü „Senden“ im Reiter „Bildschirm“.
- `vms-mobil.png`: 390 Pixel Breite, Deutsch, Theme Hell, Übersicht mit eingeklappter VM-Liste.

Vom Repository-Stamm aus den Vite-Server starten:

```sh
npm --prefix web run dev -- --host 127.0.0.1 --port 5179
```

In einem zweiten Terminal mit installiertem Playwright und Chromium aufnehmen (die Browserprüfung läuft dabei vollständig mit):

```sh
README_SCREENSHOTS=docs/screenshots/v0.7.2 node scripts/test-ui-vms.mjs
```

Bei separat installiertem Playwright bzw. Browser können `PLAYWRIGHT_MODULE` (absoluter Modulpfad) und `BROWSER_EXECUTABLE` (absoluter Chromium-Pfad) gesetzt werden. Die Aufnahmeroutine greift auf keine realen NAS-Datenträger oder VMs zu.
