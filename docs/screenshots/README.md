# Screenshots der Weboberfläche

`current/` enthält die aktuellen Aufnahmen für die Projekt-README und die Fachdokumente.

**Aufnahmestand:** die Bilder aus `capture.mjs` sowie Firewall, VM erstellen, Protokoll, Dateien ersetzen und Dateiaufträge zeigen die gebaute Svelte-WebUI von **TitanOS 0.41.0** (Stand vor der Veröffentlichung, Zweig `grossumbau-0.41`), aufgenommen am **31. Oktober 2026** mit Chromium (Playwright), Theme Graphit, blauer Akzent, Glasflächen, Deutsch. Die Statuszeile von Firewall, VM erstellen und Protokoll nennt die Version der jeweiligen Prüfskripte. Einstellungen, Arbeitsspeicher, App-Images, Netzwerk, Docker-LAN, API-Zugänge, VM-Medien und Plugin-Aufräumen sind noch Aufnahmen von **0.31.0**: `test-ui-firewall.mjs` liefert für die Übersicht derzeit keine brauchbare Aufnahme, `test-ui-memory-images.mjs` und `test-ui-023.mjs` scheitern an veralteten Erwartungen der Skripte (beides nachzuziehen).

**Veraltet (Prüfung 31.10.2026, Plan 9f):** `einstellungen.png` zeigt noch die Übersicht mit zweiter Liste und `docker.png` die frühere Tabelle mit Spalte „PORTS“. Die WebUI zeigt beides so nicht mehr (Übersicht nur „Braucht Aufmerksamkeit“ und „Dieses NAS“, Docker-Liste als Kacheln); die Bilder werden mit der nächsten vollständigen Aufnahme ersetzt.

Alle NAS-Antworten werden durch isolierte Demonstrationsdaten ersetzt. Hostname, Konten, Dateien, Laufwerke, Adressen und Messwerte sind Beispiele. Es werden weder echte NAS-Geräte angesprochen noch Container oder VMs gestartet. Die sichtbaren App-Icons sind die konfigurierten Originalbilder der jeweiligen Projekte. Ihre Quellen und SHA-256-Werte stehen in [icon-sources.json](icon-sources.json); Namen und Logos gehören den jeweiligen Projekten. Keine nachträgliche Bildbearbeitung oder eingebauten Design-Mockups. Die Bilder ersetzen keine Funktions- oder Hardwareprüfung.

[capture.json](current/capture.json) nennt Quellstand, Version, Aufnahmedatum und für jedes Bild Aufnahmeskript, Originaldatei der Skriptausgabe, Größe und SHA-256.

## In der README

| Datei | Inhalt | Darstellung |
|---|---|---|
| [dashboard.png](current/dashboard.png) | Persönliche Startseite mit zehn Kacheln auf dem Motiv Nordlicht | Desktop 1600, vollständige Seite |
| [dashboard-editor.png](current/dashboard-editor.png) | Editor mit haftender Editorleiste (Kachel hinzufügen, Darstellung, Spalten, Mindestbreite, Rückgängig/Wiederholen, Standard, Abbrechen, Speichern) und den Inhaltsoptionen der ausgewählten Docker-Kachel | Desktop 1600, vollständige Seite |
| [dashboard-hintergruende.png](current/dashboard-hintergruende.png) | Einstellungen › Darstellung › Hintergrund des Dashboards: Galerie mit zehn lokalen Motiven und Vorschau | Desktop 1600 × 1000 |
| [dashboard-bild-upload.png](current/dashboard-bild-upload.png) | Dieselbe Galerie, Reiter „Eigene Bilder“ mit einem hinzugefügten Bild | Desktop 1600 × 1000 |
| [darstellung.png](current/darstellung.png) | Einstellungen › Darstellung in der Seitenleiste: Theme, Akzent, Stil der Flächen, Vorschau | Desktop 1600 × 1000 |
| [dashboard-vm-details.png](current/dashboard-vm-details.png) | Details unmittelbar unter der ausgewählten VM | Desktop 1600, vollständige Seite |
| [speicher.png](current/speicher.png) | Volume-Liste und ausgewähltes RAID5-Volume | Desktop 1600 × 1000 |
| [shares.png](current/shares.png) | Kompakte Freigabenliste | Desktop 1600, vollständige Seite |
| [dateien.png](current/dateien.png) | Dateimanager mit Ordnern und Dokumenten | Desktop 1600, vollständige Seite |
| [docker.png](current/docker.png) | Geschlossene Stacks und einzelne Container | Desktop 1600, vollständige Seite |
| [vms.png](current/vms.png) | Liste und Übersicht einer ausgewählten VM | Desktop 1600, vollständige Seite |
| [einstellungen.png](current/einstellungen.png) | Einstellungsübersicht mit Seitenleiste, sechs Gruppen und „neu“-Abzeichen | Desktop 1440, vollständige Seite |
| [firewall.png](current/firewall.png) | Firewall eingeschaltet: Zonen und Dienste, Docker-Apps, eigene Regeln, automatisches Sperren, zuletzt verworfene Pakete | Desktop 1440, vollständige Seite |
| [anmeldeseite.png](current/anmeldeseite.png) | Gestaltete Anmeldeseite (abgemeldet): Theme Mitternacht, Akzent Türkis, Glaskarte über dem Motiv Nordlicht, Uhr, Überschrift „Willkommen zu Hause“, Hinweis „Nur für die Familie.“ | Desktop 1600 × 900 |
| [dashboard-mobil.png](current/dashboard-mobil.png) | Oberer Ausschnitt der gestapelten Startseite | Smartphone 390 × 844 |
| [dateien-mobil.png](current/dateien-mobil.png) | Ortsauswahl und Dateiliste | Smartphone 390 × 844 |

## Weitere Aufnahmen

Nicht in der README eingebunden; die VM- und Protokollbilder verlinkt [VM-CREATE-VALIDATION.md](../VM-CREATE-VALIDATION.md).

| Datei | Inhalt | Darstellung |
|---|---|---|
| [vm-erstellen.png](current/vm-erstellen.png) | VM-Formular mit sechs Kacheln (einschließlich Zeitplan) und Zusammenfassung | Desktop 1600, vollständige Seite |
| [vm-erstellen-mobil.png](current/vm-erstellen-mobil.png) | Dasselbe Formular in Lesereihenfolge gestapelt | Smartphone 390, vollständige Seite |
| [protokoll.png](current/protokoll.png) | Systemprotokoll mit Filter und originaler historischer Versionsmeldung „TitanOS 0.19.0 started“ als Journalbeispiel | Desktop 1600, vollständige Seite |
| [arbeitsspeicher.png](current/arbeitsspeicher.png) | Verfügbarer RAM trotz hoher Cachebelegung | Desktop 1440, vollständige Seite |
| [app-images.png](current/app-images.png) | Eigene Image-Tags je Compose-Dienst | Desktop 1440, vollständige Seite |
| [app-images-mobil.png](current/app-images-mobil.png) | Image-Tags im Installationspanel | Smartphone 390, vollständige Seite |
| [dateien-ersetzen.png](current/dateien-ersetzen.png) | Ersetzen-Bestätigung vor dem ersten Kopierklick | Desktop 1440 × 1000 |
| [dateien-auftraege-1440.png](current/dateien-auftraege-1440.png) | Interne Laufwerke, Restzeit und Entfernen einzelner/aller beendeten Aufträge | Desktop 1440, vollständige Seite |
| [dateien-auftraege-390.png](current/dateien-auftraege-390.png) | Dieselben Funktionen mit geöffneter Ortsauswahl | Smartphone 390, vollständige Seite |
| [netzwerk.png](current/netzwerk.png) | Mehrere NAS-Netze in der Einstellungsseitenleiste | Desktop 1440, vollständige Seite |
| [netzwerk-mobil.png](current/netzwerk-mobil.png) / [Hell](current/netzwerk-mobil-hell.png) | Netzwerk, Graphit/Hell | Smartphone 390 |
| [docker-lan.png](current/docker-lan.png) | Docker-LAN-Pool | Desktop 1440 × 1000 |
| [api-zugaenge.png](current/api-zugaenge.png) | Persönliche API-Zugänge in der Einstellungsseitenleiste | Desktop 1440, vollständige Seite |
| [vm-medien-mobil.png](current/vm-medien-mobil.png) | Medienimport | Smartphone 390 × 844 |
| [plugin-aufraeumen.png](current/plugin-aufraeumen.png) | Geschützte und auswählbare Plugin-Versionen | Desktop 1440 × 1000 |

## Herkunft je Bild

`docs/screenshots/capture.mjs` nimmt die README-Bilder außer Einstellungen und Firewall auf. Die übrigen Bilder stammen unverändert aus der Ausgabe der Browserprüfungen, die dabei zugleich ihre Funktionsprüfungen bestehen müssen:

| Skript | Ausgabe → Datei in `current/` |
|---|---|
| `scripts/test-ui-firewall.mjs` | `settings-overview-desktop.png` → `einstellungen.png`, `firewall-desktop-on.png` → `firewall.png` |
| `scripts/test-ui-vm-create.mjs` | `graphit-1600-de.png` → `vm-erstellen.png`, `graphit-390-de.png` → `vm-erstellen-mobil.png`, `system-logs.png` → `protokoll.png` |
| `scripts/test-ui-memory-images.mjs` | `ram-cache.png` → `arbeitsspeicher.png`, `stack-images.png` → `app-images.png`, `stack-images-mobile.png` → `app-images-mobil.png` |
| `scripts/test-ui-copy-confirmation.mjs` | `replace-before-copy.png` → `dateien-ersetzen.png` |
| `scripts/test-ui-drive-transfers.mjs` | `dateien-auftraege-1440.png`, `dateien-auftraege-390.png` (gleiche Namen) |
| `scripts/test-ui-023.mjs` | `settings-network-1440-graphit.png` → `netzwerk.png`, `settings-network-390-graphit.png` → `netzwerk-mobil.png`, `settings-network-390-hell.png` → `netzwerk-mobil-hell.png`, `docker-networks-1440-graphit.png` → `docker-lan.png`, `settings-api-1440-graphit.png` → `api-zugaenge.png`, `vms-media-390-graphit.png` → `vm-medien-mobil.png`, `plugins-cleanup-1440-graphit.png` → `plugin-aufraeumen.png` |

Die Anmeldeseite nimmt `capture.mjs` in einem eigenen, abgemeldeten Browserkontext mit isolierten Werten für `/api/v1/login-appearance` auf (wie `scripts/test-ui-login-appearance.mjs`); Texte werden nur als Text angezeigt. Bei der Aufnahme werden geladene Schriftdateien, alle 13 Docker-Logos, fehlender horizontaler Überlauf auf dem Smartphone und JavaScript-Laufzeitfehler geprüft.

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

Neue Bilder ersetzen die Dateien in `current/`, damit die README keine Versionspfade benötigt. Reihenfolge: zuerst die Skripte aus der Tabelle oben mit eigenem `UI_OUTPUT` ausführen und ihre Ausgaben wie angegeben nach `current/` kopieren, zuletzt `capture.mjs` (schreibt seine Bilder und `capture.json` mit den Prüfsummen aller Bilder in `current/`). Danach Aufnahmestand oben anpassen und jedes Bild ansehen. Ordner wie `v0.6.1/` bis `v0.7.2/` und die Bilder direkt in `docs/screenshots/` sind historische Aufnahmen und werden nirgends mehr verlinkt.

Die Skripte aus der Tabelle oben laufen gegen denselben Vorschau-Server, zum Beispiel:

```sh
PLAYWRIGHT_MODULE=/absoluter/pfad/zu/playwright/index.mjs \
UI_URL=http://127.0.0.1:5187 UI_OUTPUT=out/validation/vm-create-production \
node scripts/test-ui-vm-create.mjs
```
