# Screenshots der Weboberfläche

`current/` enthält die aktuellen Aufnahmen für die Projekt-README und die Fachdokumente.

**Aufnahmestand:** Alle Bilder der README kommen aus `scripts/capture-readme-screenshots.mjs` und zeigen den Stand **0.78 in Arbeit** (Zweig `docs/078-readme`, abgezweigt vom 0.78-Arbeitszweig `feat/078-nvr-ui` bei Commit `052cc095`; der Web-Teil ist bis `e3fe8443` unverändert). Aufgenommen am **13. November 2026** mit Chromium/Playwright gegen einen Vite-Entwicklungsserver dieses Stands, Theme Graphit, blauer Akzent, Deutsch; Programmfenster maximiert. Die Seiten nennen die Version 0.78 (`SHOW_VERSION`, ab Werk `0.78.0`).

Alle NAS-Antworten werden durch isolierte Demonstrationsdaten ersetzt (`scripts/test-ui-fixtures.mjs`, für den NVR `scripts/test-ui-nvr-fixtures.mjs`). Hostname, Konten, Dateien, Laufwerke, Adressen, Messwerte, Docker-Apps (immich, jellyfin, paperless, home-assistant, vaultwarden) und VMs (Home Assistant OS, Windows 11) sind Beispiele des Skripts. Es werden weder echte NAS-Geräte angesprochen noch Container oder VMs gestartet. Nachgebildet sind:

- das Bild der **Fernansicht**: das mitgelieferte Foto „Küste“ (`/backgrounds/kueste.webp`), im Browser selbst als Video kodiert und über eine nachgestellte Verbindung gesendet, keine echte App;
- die **Kamerabilder des NVR**: gezeichnete Farbverläufe der Testdaten, kein Video.

Die App-Symbole sind Buchstaben, keine Logos der Projekte. Keine nachträgliche Bildbearbeitung und keine Design-Mockups. Die Bilder ersetzen keine Funktions- oder Hardwareprüfung.

[capture.json](current/capture.json) nennt Quellstand, Aufnahmedatum und für jedes Bild Aufnahmeskript, Originaldatei der Skriptausgabe, Größe und SHA-256.

## In der README

| Datei | Inhalt | Darstellung |
|---|---|---|
| [arbeitsplatz.png](current/arbeitsplatz.png) | Arbeitsplatz: Programmsymbole mit Abzeichen (Update, läuft/gestoppt), Widgets rechts (Zustand, CPU/RAM, Volumes, Docker-Apps), obere Leiste mit „Menü“ und Suchfeld, Dock | Desktop 1600 × 1000 |
| [anmeldeseite.png](current/anmeldeseite.png) | Anmeldeseite nach dem Abmelden: Nordlicht, Reihe Kontobilder, Passwortfeld, Uhr, Sprache | Desktop 1600 × 1000 |
| [dateien.png](current/dateien.png) | Dateien im Ordner „Documents“: Seitenleiste mit Freigaben, Adresszeile, Befehlsleiste, zwei markierte Dateien, Statuszeile | Desktop 1600 × 1000 |
| [docker.png](current/docker.png) | Docker › Container: Tabelle der Apps, rechts die Einzelheiten der gewählten App „immich“, Statusleiste | Desktop 1600 × 1000 |
| [vms.png](current/vms.png) | Virtuelle Maschinen: zwei VMs in der Seitenleiste, Übersicht der laufenden VM „Home Assistant OS“ mit Auslastung, Netzwerk, Hardware und Snapshots | Desktop 1600 × 1000 |
| [nvr.png](current/nvr.png) | NVR › Live mit vier Beispielkameras (eine nicht erreichbar) und den Ereignissen daneben; Kamerabilder nachgebildet | Desktop 1600 × 1000 |
| [hauptmenue.png](current/hauptmenue.png) | Das Menü: Filter, Programme A–Z mit Docker-Apps und VM-Bildschirmen, unten Konto, Systemsteuerung, Updates, App-Center, Sperren, Abmelden, Neustart, Herunterfahren; oben das Suchfeld der Leiste | Desktop 1600 × 1000 |
| [dock-bearbeiten.png](current/dock-bearbeiten.png) | Dock bearbeiten: Größe, Deckkraft, Vergrößern, Position; Programme im Dock mit × | Desktop 1600 × 1000 |
| [fernansicht.png](current/fernansicht.png) | Fenster der Docker-App „immich“ in der Fernansicht, maximiert, mit geöffnetem ⋯-Menü: Datei öffnen, Heruntergeladen, Zwischenablage, Qualität, Neu verbinden, Zustand; Bild nachgebildet | Desktop 1600 × 1000 |
| [systemdaten.png](current/systemdaten.png) | Systemsteuerung › Daten und Sicherung › Systemdaten: Ablage-Volume und eine Zeile je Art der Systemdaten | Desktop 1600 × 1000 |
| [systemsteuerung.png](current/systemsteuerung.png) | Systemsteuerung › Übersicht: Seitenleiste mit Konto, Suche und Bereichen; Zustand, Zu erledigen, Dienste, Zuletzt geändert | Desktop 1600 × 1391 (Fenster so hoch wie die Seite) |
| [darstellung.png](current/darstellung.png) | Systemsteuerung › Darstellung, Reiter Farben: Modus, dunkle Variante, Akzent, Glas von Klar bis Getönt, Ecken, Schriftgröße, Farbstärke, Theme als Datei | Desktop 1600 × 1254 (Fenster so hoch wie die Seite) |
| [handy-start.png](current/handy-start.png) | Startbildschirm am Handy: Knopf „Menü“, Suche, Zustand, App-Raster mit Docker-Apps | Smartphone 390 × 844 |
| [fernansicht-mobil.png](current/fernansicht-mobil.png) | Fernansicht am Handy im Vollbild mit Zurück, Lautstärke und Tastatur; Bild nachgebildet | Smartphone 390 × 844 |

## Weitere Aufnahmen (nicht in der README)

| Datei | Inhalt | Darstellung |
|---|---|---|
| [freigaben.png](current/freigaben.png) | Freigaben im neuen Aufbau (0.77 in Arbeit, 12.11.2026) | Desktop 1600 × 1000 |
| [speicher.png](current/speicher.png) | Speicher › Übersicht (0.77 in Arbeit, 12.11.2026). Die Spalte „Platten“ zeigt 0, weil die Testdaten keine Mitgliedsplatten nennen; deshalb nicht in der README | Desktop 1600 × 1000 |

## Herkunft je Bild

| Skript | Name im Skript (`ONLY=`) → Datei in `current/` |
|---|---|
| `scripts/capture-readme-screenshots.mjs` | `arbeitsplatz`, `anmeldeseite`, `dateien`, `docker`, `vms`, `nvr`, `hauptmenue`, `dock-bearbeiten`, `fernansicht`, `fernansicht-mobil`, `systemdaten`, `systemsteuerung`, `darstellung`, `handy-start` → jeweils `<Name>.png` (13.11.2026) |
| `scripts/capture-readme-screenshots.mjs` | `freigaben`, `speicher` → `freigaben.png`, `speicher.png` (12.11.2026, bei dieser Aufnahme nicht wiederholt) |

Was das Skript je Bild tut:

- Jedes Bild startet einen frischen Browser-Kontext mit den Testdaten, löscht die gespeicherte Fensteranordnung, öffnet den Pfad, wartet 4 s und maximiert das Fenster per Doppelklick auf die Titelleiste.
- `docker` klickt die Zeile „immich“ an (Einzelheiten rechts), `vms` wechselt auf den Reiter „Übersicht“, `dateien` markiert zwei Dateien, `nvr` schaltet die Ereignisse neben den Kacheln ein, `hauptmenue` öffnet das Menü, `dock-bearbeiten` öffnet „Dock bearbeiten“ per Rechtsklick auf das Dock.
- `systemsteuerung`, `darstellung` und `systemdaten` vergrößern das Browserfenster, bis die Seite im Fenster nichts mehr zu scrollen hat. So endet das Bild unter der letzten Zeile statt mitten in einer.
- `fernansicht` und `fernansicht-mobil` kodieren das Foto „Küste“ im Browser als Video (wie `scripts/test-ui-viewer.mjs`) und beantworten die WebSocket-Verbindung der Fernansicht selbst; das Fenster bzw. die Vollbildansicht zeigt dann diese Bilder.
- `anmeldeseite` meldet die Sitzung als abgemeldet und liefert vier Konten mit mitgelieferten Kontobildern.

## Neu aufnehmen

Voraussetzungen: Node.js 24, installierte Web-Abhängigkeiten und Playwright mit Chromium. Aus dem Repository-Stamm einen UI-Server starten, zum Beispiel:

```sh
npm --prefix web ci
npm --prefix web run dev -- --host 127.0.0.1 --port 5262
```

(oder `npm --prefix web run build` und `npm --prefix web run preview -- --host 127.0.0.1 --port 5262`). In einem zweiten Terminal:

```sh
PW=/absoluter/pfad/zu/playwright/index.mjs \
UI_URL=http://127.0.0.1:5262 OUT=docs/screenshots/current \
node scripts/capture-readme-screenshots.mjs
```

`ONLY=name,name` nimmt nur diese Bilder auf, `SHOW_VERSION` setzt die Version, die die Seiten nennen. Ohne `OUT` landen die Bilder in `out/readme-shots/`. Die Browseraufnahme verwendet nur den lokalen UI-Server und abgefangene API-Antworten.

Neue Bilder ersetzen die Dateien in `current/`, damit die README keine Versionspfade benötigt. Danach `capture.json` mit Größe und SHA-256 jedes neuen Bildes nachführen (von Hand bzw. per kleinem Skript), den Aufnahmestand oben anpassen und jedes Bild ansehen: kein abgeschnittener oder überlappender Text, nichts hinter dem Dock, keine leeren Zustände, keine unpassenden Beispieldaten. Aufnahmen der alten Oberfläche (bis 0.62) und des früheren Agent-Auftritts sind seit dem 14.11.2026 entfernt; sie stehen nur noch in der Git-Historie.
