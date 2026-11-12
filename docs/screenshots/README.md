# Screenshots der Weboberfläche

`current/` enthält die aktuellen Aufnahmen für die Projekt-README und die Fachdokumente.

**Aufnahmestand:** Alle Bilder der README kommen aus `scripts/capture-readme-screenshots.mjs` und zeigen den Stand **0.78 in Arbeit** (Zweig `docs/078-readme`, gleicher Stand wie der 0.78-Arbeitszweig `feat/078-nvr-ui`, Commit `052cc095`). Aufgenommen am **13. November 2026** mit Chromium/Playwright gegen einen Vite-Entwicklungsserver dieses Stands, Theme Graphit, blauer Akzent, Deutsch; Programmfenster maximiert. Die Seiten nennen die Version 0.78 (`SHOW_VERSION`, ab Werk `0.78.0`).

Alle NAS-Antworten werden durch isolierte Demonstrationsdaten ersetzt (`scripts/test-ui-fixtures.mjs`, für den NVR `scripts/test-ui-nvr-fixtures.mjs`). Hostname, Konten, Dateien, Laufwerke, Adressen, Messwerte, Docker-Apps (immich, jellyfin, paperless, home-assistant, vaultwarden) und VMs (Home Assistant OS, Windows 11) sind Beispiele des Skripts. Es werden weder echte NAS-Geräte angesprochen noch Container oder VMs gestartet. Nachgebildet sind:

- das Bild der **Fernansicht**: das mitgelieferte Foto „Küste“ (`/backgrounds/kueste.webp`), im Browser selbst als Video kodiert und über eine nachgestellte Verbindung gesendet, keine echte App;
- die **Kamerabilder des NVR**: gezeichnete Farbverläufe der Testdaten, kein Video.

Die App-Symbole sind Buchstaben, keine Logos der Projekte ([icon-sources.json](icon-sources.json) gehört zu älteren Aufnahmen). Keine nachträgliche Bildbearbeitung und keine Design-Mockups. Die Bilder ersetzen keine Funktions- oder Hardwareprüfung.

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
| [vm-erstellen.png](current/vm-erstellen.png) | VM-Formular mit sechs Kacheln und Zusammenfassung, alte Oberfläche (0.47.1) | Desktop 1600, vollständige Seite |
| [vm-erstellen-mobil.png](current/vm-erstellen-mobil.png) | Dasselbe Formular gestapelt, alte Oberfläche (0.47.1) | Smartphone 390, vollständige Seite |
| [protokoll.png](current/protokoll.png) | Systemprotokoll mit Filter, alte Oberfläche (0.47.1) | Desktop 1600, vollständige Seite |
| [shares.png](current/shares.png), [dateien-mobil.png](current/dateien-mobil.png), [firewall.png](current/firewall.png) | Seiten der alten Oberfläche (0.47.1) | Desktop 1600 bzw. 1440, Smartphone 390 |
| [dateien-ersetzen.png](current/dateien-ersetzen.png), [dateien-auftraege-1440.png](current/dateien-auftraege-1440.png), [dateien-auftraege-390.png](current/dateien-auftraege-390.png) | Ersetzen-Bestätigung, Dateiaufträge, alte Oberfläche (0.47.1) | 1440 bzw. 390 |
| [ki-agent.png](current/ki-agent.png), [ki-agent-chat.png](current/ki-agent-chat.png), [ki-agent-mobil.png](current/ki-agent-mobil.png), [ki-agent-menue-mobil.png](current/ki-agent-menue-mobil.png) | KI-Agent mit eigenem Auftritt (0.51.0/0.54.0); dieser Auftritt ist seit 0.64.0 entfernt | Desktop 1440, Smartphone 390 |

Die Bilder der alten Oberfläche (Kopfzeile und Seitennavigation, seit 0.62.0 entfernt) dienen nur noch als Nachweis ihrer damaligen Prüfungen; `vm-erstellen*.png` und `protokoll.png` verlinkt `docs/VM-CREATE-VALIDATION.md`.

## Herkunft je Bild

| Skript | Name im Skript (`ONLY=`) → Datei in `current/` |
|---|---|
| `scripts/capture-readme-screenshots.mjs` | `arbeitsplatz`, `anmeldeseite`, `dateien`, `docker`, `vms`, `nvr`, `hauptmenue`, `dock-bearbeiten`, `fernansicht`, `fernansicht-mobil`, `systemdaten`, `systemsteuerung`, `darstellung`, `handy-start` → jeweils `<Name>.png` (13.11.2026) |
| `scripts/capture-readme-screenshots.mjs` | `freigaben`, `speicher` → `freigaben.png`, `speicher.png` (12.11.2026, bei dieser Aufnahme nicht wiederholt) |
| `scripts/test-ui-vm-create.mjs`, `scripts/test-ui-drive-transfers.mjs`, `scripts/test-ui-copy-confirmation.mjs`, `scripts/test-ui-firewall.mjs`, `scripts/test-ui-agent.mjs`, `docs/screenshots/capture.mjs` | die Bilder der alten Oberfläche, siehe `capture.json` |

Was das Skript je Bild tut:

- Jedes Bild startet einen frischen Browser-Kontext mit den Testdaten, löscht die gespeicherte Fensteranordnung, öffnet den Pfad, wartet 4 s und maximiert das Fenster per Doppelklick auf die Titelleiste.
- `docker` klickt die Zeile „immich“ an (Einzelheiten rechts), `vms` wechselt auf den Reiter „Übersicht“, `dateien` markiert zwei Dateien, `nvr` schaltet die Ereignisse neben den Kacheln ein, `hauptmenue` öffnet das Menü, `dock-bearbeiten` öffnet „Dock bearbeiten“ per Rechtsklick auf das Dock.
- `systemsteuerung`, `darstellung` und `systemdaten` vergrößern das Browserfenster, bis die Seite im Fenster nichts mehr zu scrollen hat. So endet das Bild unter der letzten Zeile statt mitten in einer.
- `fernansicht` und `fernansicht-mobil` kodieren das Foto „Küste“ im Browser als Video (wie `scripts/test-ui-viewer.mjs`) und beantworten die WebSocket-Verbindung der Fernansicht selbst; das Fenster bzw. die Vollbildansicht zeigt dann diese Bilder.
- `anmeldeseite` meldet die Sitzung als abgemeldet und liefert vier Konten mit mitgelieferten Kontobildern.

`docs/screenshots/capture.mjs` nimmt noch die alte Oberfläche (Dashboard, Kopfzeile) auf und ist **veraltet**; nicht mehr für `current/` verwenden.

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

Neue Bilder ersetzen die Dateien in `current/`, damit die README keine Versionspfade benötigt. Danach `capture.json` mit Größe und SHA-256 jedes neuen Bildes nachführen (von Hand bzw. per kleinem Skript), den Aufnahmestand oben anpassen und jedes Bild ansehen: kein abgeschnittener oder überlappender Text, nichts hinter dem Dock, keine leeren Zustände, keine unpassenden Beispieldaten. Ordner wie `v0.6.1/` bis `v0.7.2/` und die Bilder direkt in `docs/screenshots/` sind historische Aufnahmen und werden nirgends mehr verlinkt.
