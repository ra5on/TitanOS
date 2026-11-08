# Screenshots der Weboberfläche

`current/` enthält die aktuellen Aufnahmen für die Projekt-README und die Fachdokumente.

**Aufnahmestand:** Alle in der README eingebundenen Bilder zeigen **TitanOS 0.70.0** (Zweig `feat/0.70`), aufgenommen am **8. November 2026** mit `scripts/capture-readme-screenshots.mjs` (Chromium/Playwright gegen `vite dev`, Theme Graphit, blauer Akzent, Deutsch, Fenster maximiert). Die Docker-Apps (immich, jellyfin, paperless, home-assistant, vaultwarden) sind Beispieldaten des Skripts mit Buchstaben-Symbolen. Die Fernansicht kommt aus `scripts/test-ui-viewer.mjs` mit `VIEWER_PICTURE=/backgrounds/kueste.webp`.

Ältere Bilder in `current/` (Speicher, Freigaben, Dateien, Firewall, VM erstellen, Protokoll, Dateiaufträge, KI-Agent) zeigen die **alte Oberfläche** mit Kopfzeile (0.47.1 bzw. KI-Agent 0.51.0/0.54.0). Kopfzeile und Seitennavigation sind seit 0.62.0 entfernt; diese Bilder sind nur noch in Fachdokumenten verlinkt (VM erstellen, Protokoll) bzw. als Nachweis ihrer damaligen Prüfungen und werden in der README nicht mehr eingebunden. Die Aufnahmen des alten Dashboards und der alten Navigation sind entfernt.

Alle NAS-Antworten werden durch isolierte Demonstrationsdaten ersetzt (`scripts/test-ui-fixtures.mjs`). Hostname, Konten, Dateien, Laufwerke, Adressen und Messwerte sind Beispiele. Es werden weder echte NAS-Geräte angesprochen noch Container oder VMs gestartet. Das Bild in der **Fernansicht** ist ein nachgebildeter Strom (das mitgelieferte Foto „Küste“, im Browser selbst als Video kodiert), keine echte App. Die App-Symbole sind Buchstaben, keine Logos der Projekte ([icon-sources.json](icon-sources.json) gehört zu älteren Aufnahmen). Keine nachträgliche Bildbearbeitung und keine Design-Mockups. Die Bilder ersetzen keine Funktions- oder Hardwareprüfung.

[capture.json](current/capture.json) nennt Quellstand, Aufnahmedatum und für jedes Bild Aufnahmeskript, Originaldatei der Skriptausgabe, Größe und SHA-256.

## In der README

| Datei | Inhalt | Darstellung |
|---|---|---|
| [arbeitsplatz.png](current/arbeitsplatz.png) | Arbeitsplatz: Programmsymbole mit Abzeichen (Update, läuft/gestoppt), Widgets rechts (Zustand, CPU/RAM, Volumes, Docker-Apps), Leisten oben, Dock | Desktop 1600 × 1000 |
| [fernansicht.png](current/fernansicht.png) | Fenster der Docker-App „immich“ in der Fernansicht mit geöffnetem Menü „…“: Lautstärke, Datei öffnen, Zwischenablage, Qualität, Zustand; Bild nachgebildet | Desktop 1600 × 1000 |
| [hauptmenue.png](current/hauptmenue.png) | Hauptmenü mit Suche, Filtern, Programmen, Docker-Apps und VM-Bildschirm, unten Konto, Sperren, Ein/Aus | Desktop 1600 × 1000 |
| [docker.png](current/docker.png) | Programm Docker (Fenster maximiert): Seitenleiste, Werkzeugleiste, Container-Karten | Desktop 1600 × 1000 |
| [vms.png](current/vms.png) | Programm Virtuelle Maschinen: „Deine VMs“ in der Seitenleiste, Übersicht einer VM mit Gefahrenzone | Desktop 1600 × 1000 |
| [systemsteuerung.png](current/systemsteuerung.png) | Systemsteuerung › Übersicht: Seitenleiste mit Suche, Zustand, Dienste, Zu erledigen | Desktop 1600 × 1000 |
| [darstellung.png](current/darstellung.png) | Systemsteuerung › Darstellung: Theme, Variante, Akzent, Glas von Klar bis Getönt, Hintergründe | Desktop 1600 × 1000 |
| [dock-bearbeiten.png](current/dock-bearbeiten.png) | Dock bearbeiten: Größe, Deckkraft, Vergrößern, Position; Programme im Dock mit × | Desktop 1600 × 1000 |
| [anmeldeseite.png](current/anmeldeseite.png) | Anmeldeseite nach dem Abmelden: Uhr, Glaskarte, Sprache | Desktop 1440 × 900 |
| [handy-start.png](current/handy-start.png) | Startbildschirm am Handy: Suche, Zustand, App-Raster mit Docker-Apps | Smartphone 390 × 844 |
| [fernansicht-mobil.png](current/fernansicht-mobil.png) | Fernansicht am Handy im Vollbild mit Lautstärke und Tastatur | Smartphone 390 × 844 |
| [ki-agent-mobil.png](current/ki-agent-mobil.png) | KI-Agent als eigene App am Handy (0.54.0) | Smartphone 390 × 844 |
| [ki-agent-menue-mobil.png](current/ki-agent-menue-mobil.png) | Menü des KI-Agenten am Handy (0.54.0) | Smartphone 390 × 844 |

## Weitere Aufnahmen (alte Oberfläche)

| Datei | Inhalt | Darstellung |
|---|---|---|
| [vm-erstellen.png](current/vm-erstellen.png) | VM-Formular mit sechs Kacheln und Zusammenfassung (0.47.1) | Desktop 1600, vollständige Seite |
| [vm-erstellen-mobil.png](current/vm-erstellen-mobil.png) | Dasselbe Formular gestapelt | Smartphone 390, vollständige Seite |
| [protokoll.png](current/protokoll.png) | Systemprotokoll mit Filter | Desktop 1600, vollständige Seite |
| [speicher.png](current/speicher.png), [shares.png](current/shares.png), [dateien.png](current/dateien.png), [dateien-mobil.png](current/dateien-mobil.png), [firewall.png](current/firewall.png) | Seiten der alten Oberfläche (0.47.1) | Desktop 1600 bzw. 390 |
| [dateien-ersetzen.png](current/dateien-ersetzen.png), [dateien-auftraege-1440.png](current/dateien-auftraege-1440.png), [dateien-auftraege-390.png](current/dateien-auftraege-390.png) | Ersetzen-Bestätigung, Dateiaufträge (0.47.1) | 1440 bzw. 390 |
| [ki-agent.png](current/ki-agent.png), [ki-agent-chat.png](current/ki-agent-chat.png) | KI-Agent am Desktop neben der alten Seitennavigation (0.51.0/0.54.0) | Desktop 1440 |

## Herkunft je Bild

| Skript | Ausgabe → Datei in `current/` |
|---|---|
| `scripts/test-ui-desktop.mjs`, Variante A | `01-arbeitsplatz.png` → `arbeitsplatz.png`, `12-handy-start.png` → `handy-start.png` |
| `scripts/test-ui-desktop.mjs`, Variante B | `06-hauptmenue.png` → `hauptmenue.png`, `11-dock-bearbeiten.png` → `dock-bearbeiten.png` |
| `scripts/test-ui-viewer.mjs`, Variante B | `01-fenster-fernansicht.png` → `fernansicht.png`, `07-handy-fernansicht.png` → `fernansicht-mobil.png` |
| `scripts/test-ui-appearance.mjs` (unverändert) | `09-abgemeldet-desktop.png` → `anmeldeseite.png` |
| Aufnahme nach Variante B, Fenster maximiert | `/settings` → `systemsteuerung.png`, `/settings/appearance` → `darstellung.png`, `/docker` → `docker.png`, `/vms` → `vms.png` |
| `scripts/test-ui-agent.mjs` (0.54.0) | `10-phone-in-titanos.png` → `ki-agent-mobil.png`, `16-phone-menu.png` → `ki-agent-menue-mobil.png` |

**Varianten** (Kopien der Skripte außerhalb des Repositorys, nur für die Aufnahme): **A** setzt in den Testcontainern `icon` auf `https://icons.example.test/<name>.svg` und beantwortet diese Adressen aus `out/readme-icons/` (sonst zeigen die Docker-Apps Buchstaben). **B** setzt zusätzlich `prefs.desktop = { widgetsHidden: true }`, damit keine Widgets unter halbtransparenten Fenstern durchscheinen. Die Aufnahmen „Fenster maximiert“ öffnen den Pfad wie `open()` in `test-ui-desktop.mjs` (Variante B), doppelklicken auf die Titelleiste des Fensters und nehmen nach 1,8 s auf. Die unveränderten Skripte `test-ui-desktop.mjs`, `test-ui-viewer.mjs`, `test-ui-appearance.mjs` und `test-ui-phone.mjs` liefen am selben Tag kurz vorher ohne Befund (der Arbeitsstand änderte sich dazwischen durch andere Teilarbeiten); die Variante A von `test-ui-desktop.mjs` brach erst nach den verwendeten Bildern beim Schritt „Dock bearbeiten › Fertig“ ab, Variante B beim Widget-Schritt (es gibt dort absichtlich keine Widgets).

`docs/screenshots/capture.mjs` nimmt noch die alte Oberfläche (Dashboard, Kopfzeile) auf und ist **veraltet**; nicht mehr für `current/` verwenden, bis es auf den Desktop-Modus umgestellt ist.

## Neu aufnehmen

Voraussetzungen: Node.js 24, installierte Web-Abhängigkeiten, Playwright mit Chromium und ein gebautes Frontend. Aus dem Repository-Stamm:

```sh
npm --prefix web ci
npm --prefix web run build
npm --prefix web run preview -- --host 127.0.0.1 --port 5187
```

In einem zweiten Terminal:

```sh
PLAYWRIGHT_MODULE=/absoluter/pfad/zu/playwright/index.mjs \
UI_URL=http://127.0.0.1:5187 UI_OUTPUT=out/validation/desktop \
node scripts/test-ui-desktop.mjs
```

Bei lokal auflösbarem Playwright und dessen installiertem Chromium können die ersten beiden Variablen entfallen. Fehlende Original-Icons lädt das Skript aus den dokumentierten öffentlichen Quellen in den ignorierten Cache `out/readme-icons/`; hierfür ist einmalig Internetzugang nötig. `UI_ICON_DIR` wählt einen bereits vorhandenen Cache. Geänderte Icon-Bytes brechen die Aufnahme ab, bis Quelle und Prüfsumme bewusst geprüft wurden. Die Browseraufnahme selbst verwendet nur den lokalen UI-Server und abgefangene API-/Icon-Antworten; sonstige externe Browseranfragen werden blockiert.

`UI_OUTPUT` kann ein anderes Ausgabeziel setzen. `UI_SOURCE_REF` setzt den tatsächlich gebauten Git-Stand für den Nachweis, standardmäßig `HEAD`. Vor Aufnahmen nach einer Produktänderung Versionsangabe, Kernel und Beispieldaten im Skript anpassen, das Frontend aus diesem Stand neu bauen und alle Bilder visuell prüfen. Die feste Beispieluhr sorgt für zusammenpassende Datums- und Laufzeitangaben.

Neue Bilder ersetzen die Dateien in `current/`, damit die README keine Versionspfade benötigt. Reihenfolge: zuerst die Skripte aus der Tabelle oben mit eigenem `UI_OUTPUT` ausführen und ihre Ausgaben wie angegeben nach `current/` kopieren, danach `capture.json` mit Größe und SHA-256 jedes Bildes in `current/` neu schreiben (bis zur Umstellung von `capture.mjs` von Hand bzw. per kleinem Skript; `capture.mjs` selbst nicht mehr ausführen). Danach Aufnahmestand oben anpassen und jedes Bild ansehen. Ordner wie `v0.6.1/` bis `v0.7.2/` und die Bilder direkt in `docs/screenshots/` sind historische Aufnahmen und werden nirgends mehr verlinkt.

Die Skripte aus der Tabelle oben laufen gegen denselben Vorschau-Server, zum Beispiel:

```sh
PLAYWRIGHT_MODULE=/absoluter/pfad/zu/playwright/index.mjs \
UI_URL=http://127.0.0.1:5187 UI_OUTPUT=out/validation/vm-create-production \
node scripts/test-ui-vm-create.mjs
```
