# Screenshots der Weboberfläche

`current/` enthält die aktuellen Aufnahmen für die Projekt-README. Die bisherigen Übersichten stammen aus der **gebauten Svelte-WebUI von TitanOS 0.20.2**, Quellstand [`6e555a3`](https://github.com/ra5on/TitanOS/commit/6e555a30dfdfd7234f5361020322f3fe9c5c1530), aufgenommen am **24. Oktober 2026** mit Chromium. Die unten ergänzten VM-Formular- und Protokollbilder zeigen den gebauten Entwicklungsstand nach 0.20.2; sie sind kein veröffentlichtes Release.

Alle NAS-Antworten werden durch isolierte Demonstrationsdaten ersetzt. Hostname, Konten, Dateien, Laufwerke, Adressen und Messwerte sind Beispiele. Es werden weder echte NAS-Geräte angesprochen noch Container oder VMs gestartet. Die sichtbaren App-Icons sind die konfigurierten Originalbilder der jeweiligen Projekte. Ihre Quellen und SHA-256-Werte stehen in [icon-sources.json](icon-sources.json); Namen und Logos gehören den jeweiligen Projekten.

## Aufnahmen

| Datei | Inhalt | Darstellung |
|---|---|---|
| [dashboard.png](current/dashboard.png) | Persönliche Startseite mit zehn Kacheln | Desktop, zwei Spalten |
| [dashboard-editor.png](current/dashboard-editor.png) | Positionsraster und globale Transparenz-/Blur-/Glow-Regler | Desktop |
| [dashboard-hintergruende.png](current/dashboard-hintergruende.png) | Galerie mit zehn lokalen Motiven und Vorschau | Desktop |
| [dashboard-bild-upload.png](current/dashboard-bild-upload.png) | Eigene Bilder hinzufügen und auswählen | Desktop |
| [dashboard-vm-details.png](current/dashboard-vm-details.png) | Details unmittelbar unter der ausgewählten VM | Desktop |
| [speicher.png](current/speicher.png) | Volume-Liste und ausgewähltes RAID5-Volume | Desktop |
| [shares.png](current/shares.png) | Kompakte Freigabenliste | Desktop |
| [dateien.png](current/dateien.png) | Dateimanager mit Ordnern und Dokumenten | Desktop |
| [docker.png](current/docker.png) | Geschlossene Stacks und einzelne Container | Desktop |
| [vms.png](current/vms.png) | Liste und Übersicht einer ausgewählten VM | Desktop |
| [vm-erstellen.png](current/vm-erstellen.png) | Bestätigtes Formular mit fünf Kacheln und Zusammenfassung | Desktop, Entwicklungsstand |
| [vm-erstellen-mobil.png](current/vm-erstellen-mobil.png) | Dasselbe Formular in Lesereihenfolge gestapelt | Smartphone, vollständige Seite, Entwicklungsstand |
| [protokoll.png](current/protokoll.png) | Systemprotokoll mit Filter und originaler historischer Versionsmeldung | Desktop, Entwicklungsstand |
| [einstellungen.png](current/einstellungen.png) | Fünf Einstellungsgruppen | Desktop |
| [dashboard-mobil.png](current/dashboard-mobil.png) | Oberer Ausschnitt der gestapelten Startseite | Smartphone |
| [arbeitsspeicher.png](current/arbeitsspeicher.png) | Verfügbarer RAM trotz hoher Cachebelegung | Desktop, Entwicklungsstand 0.21.0 |
| [app-images.png](current/app-images.png) | Eigene Image-Tags je Compose-Dienst | Desktop, Entwicklungsstand 0.21.0 |
| [app-images-mobil.png](current/app-images-mobil.png) | Image-Tags im Installationspanel | Smartphone, vollständige Seite, Entwicklungsstand 0.21.0 |
| [dateien-ersetzen.png](current/dateien-ersetzen.png) | Ersetzen-Bestätigung vor dem ersten Kopierklick | Desktop, Entwicklungsstand 0.21.0 |
| [dateien-mobil.png](current/dateien-mobil.png) | Ortsauswahl und Dateiliste | Smartphone |
| [dateien-auftraege-1440.png](current/dateien-auftraege-1440.png) | Interne Laufwerke, Restzeit und Entfernen einzelner/aller beendeten Aufträge | Desktop, Entwicklungsstand nach 0.21.0 |
| [dateien-auftraege-390.png](current/dateien-auftraege-390.png) | Dieselben Funktionen mit geöffneter Ortsauswahl | Smartphone, vollständige Seite, Entwicklungsstand nach 0.21.0 |

Desktopbreite: **1600 CSS-Pixel**, Smartphone: **390 × 844 CSS-Pixel**. Theme: Graphit, blauer Akzent, Glasflächen; Dashboard mit Nordlicht-Hintergrund und global durchscheinenden Kacheln. Die Desktopbilder zeigen die vollständige Seite, die Mobilbilder den sichtbaren Bildschirmausschnitt. Keine nachträgliche Bildbearbeitung oder eingebauten Design-Mockups.

[Die Aufnahmedaten](current/capture.json) nennen Quellstand, Bildgrößen und Prüfsummen. Bei der Aufnahme werden geladene Schriftdateien, alle 13 Docker-Logos, fehlender horizontaler Überlauf auf dem Smartphone und JavaScript-Laufzeitfehler geprüft. Das ersetzt keine vollständige Funktions- oder Hardwareprüfung.

[VM-Aufnahmedaten](current/vm-create-capture.json) dokumentieren den Arbeitsstand mit Quell-Datei- und Bild-Prüfsummen. Aufnahme durch `scripts/test-ui-vm-create.mjs` gegen das gebaute Frontend, Graphit, Deutsch, mit isolierten Beispieldaten und der sichtbaren Testversion `0.20.2-vm-ui-test`. Die vollständige Browserprüfung ergänzt Hell und Englisch. Die Dateien `graphit-1600-de.png`, `graphit-390-de.png` und `system-logs.png` aus dem Testausgabeordner werden unverändert als `vm-erstellen.png`, `vm-erstellen-mobil.png` und `protokoll.png` übernommen. Die übrigen bisherigen Bilder und ihr Nachweis bleiben beim oben genannten Release-Stand.

Die RAM-/Image-Tag-/Ersetzen-Bilder wurden am **25. Oktober 2026** aus dem
gebauten Entwicklungsstand 0.21.0 aufgenommen, mit Chromium, Graphit und
ausschließlich isolierten API-Daten. Die Desktopbreite dieser vier Ergänzungen
beträgt 1440 CSS-Pixel, mobil 390 Pixel.
[memory-images-capture.json](current/memory-images-capture.json) nennt
Quell-Dateihashes und unveränderte Bildprüfsummen. Aufnahmeskripte:
`scripts/test-ui-memory-images.mjs` und `scripts/test-ui-copy-confirmation.mjs`.
Die übrigen Bilder behalten den oben dokumentierten früheren Stand.

Die Laufwerks-/Auftragsbilder wurden am **25. Oktober 2026** aus dem gebauten
Entwicklungsstand nach 0.21.0 aufgenommen: Chromium, Graphit, Deutsch,
1440 beziehungsweise 390 CSS-Pixel, vollständige Seite. Alle Laufwerke,
Dateianzahlen, Geschwindigkeiten und Restzeiten sind isolierte API-Beispiele.
`scripts/test-ui-drive-transfers.mjs`
prüft zusätzlich acht beendete Aufträge, Einzelentfernung ohne Dialog/Kennung,
veraltete Statusantworten, Neuladen, Fehler/Wiederholen, laufende Aufträge sowie
Englisch/Hell und horizontalen Überlauf. Die Bilder wurden unverändert aus der
Testausgabe übernommen. [Aufnahmedaten](current/drive-transfers-capture.json)
nennen Quell-Datei- und Bildprüfsummen. Das ist keine Hardwareabnahme.

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
BROWSER_EXECUTABLE=/absoluter/pfad/zu/chromium \
UI_URL=http://127.0.0.1:5187 \
node docs/screenshots/capture.mjs
```

Bei lokal auflösbarem Playwright und dessen installiertem Chromium können die ersten beiden Variablen entfallen. Fehlende Original-Icons lädt das Skript aus den dokumentierten öffentlichen Quellen in den ignorierten Cache `out/readme-icons/`; hierfür ist einmalig Internetzugang nötig. `UI_ICON_DIR` wählt einen bereits vorhandenen Cache. Geänderte Icon-Bytes brechen die Aufnahme ab, bis Quelle und Prüfsumme bewusst geprüft wurden. Die Browseraufnahme selbst verwendet nur den lokalen UI-Server und abgefangene API-/Icon-Antworten; sonstige externe Browseranfragen werden blockiert.

`UI_OUTPUT` kann ein anderes Ausgabeziel setzen. `UI_SOURCE_REF` setzt den tatsächlich gebauten Git-Stand für den Nachweis, standardmäßig `HEAD`. Vor Aufnahmen nach einer Produktänderung Versionsangabe, Kernel und Beispieldaten im Skript anpassen, das Frontend aus diesem Stand neu bauen und alle Bilder visuell prüfen. Die feste Beispieluhr sorgt für zusammenpassende Datums- und Laufzeitangaben.

Neue README-Bilder ersetzen die Dateien in `current/`, damit die README keine Versionspfade benötigt. Ordner wie `v0.7.0/`, `v0.7.1/` und `v0.7.2/` enthalten historische Aufnahmen und werden dort nicht mehr verwendet.

VM-/Protokollaufnahmen mit demselben lokalen Vorschau-Server:

```sh
PLAYWRIGHT_MODULE=/absoluter/pfad/zu/playwright/index.mjs \
BROWSER_EXECUTABLE=/absoluter/pfad/zu/chromium \
UI_URL=http://127.0.0.1:5187 UI_OUTPUT=out/validation/vm-create-production \
node scripts/test-ui-vm-create.mjs
```

Nach einer Neuaufnahme die drei Dateien wie oben zuordnen und die Quell- und Bild-Prüfsummen in `vm-create-capture.json` aktualisieren. Der zugehörige Funktions- und QEMU-Prüfstand steht in [VM-CREATE-VALIDATION.md](../VM-CREATE-VALIDATION.md).

## Netzwerk und Workflows 0.23.0

Die ergänzten Aufnahmen vom **26. Oktober 2026** zeigen die gebaute Produkt-WebUI
von 0.23.0 und ausschließlich isolierte Beispiele, keine QEMU-Messwerte.
`dateien-mobil.png` wurde durch den kompakten Dateimanager ersetzt. Die bisherigen
anderen Aufnahmen behalten ihren oben genannten Stand.

| Datei | Inhalt |
|---|---|
| [netzwerk.png](current/netzwerk.png) | Mehrere NAS-Netze, Desktop 1440 Pixel |
| [netzwerk-mobil.png](current/netzwerk-mobil.png) / [Hell](current/netzwerk-mobil-hell.png) | Netzwerk in 390×844, Graphit/Hell |
| [docker-lan.png](current/docker-lan.png) | Docker-LAN-Pool, Desktop |
| [api-zugaenge.png](current/api-zugaenge.png) | Persönliche API-Zugänge, Desktop |
| [vm-medien-mobil.png](current/vm-medien-mobil.png) | Medienimport, Smartphone |
| [plugin-aufraeumen.png](current/plugin-aufraeumen.png) | Geschützte und auswählbare Plugin-Versionen |

[Aufnahmedaten](current/network-workflows-capture.json) enthalten den Quellcommit,
Quell-Dateihashes, Bildprüfsummen und die gemessenen Dateizeilenpositionen.
`scripts/test-ui-023.mjs` prüft außerdem tokenbezogene Lesestandards/Widerruf,
Dialoge, Überlauf und globale Flächen-/Schriftoptionen. Die Original-PNGs wurden
unverändert übernommen. Sie liegen einzeln vor und benötigen weder ZIP noch
lokale Bildergalerie.
