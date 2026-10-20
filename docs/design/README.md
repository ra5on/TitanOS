# TitanOS – UI-Entwürfe

Diese Dateien sind die Quelltexte der Entwürfe aus dem Design-Canvas „TitanOS Web-UI“. Sie sind **Vorlage für Aufbau, Abstände, Farben und Verhalten**, kein Code zum Übernehmen:

- Sie nutzen ein Entwurfsformat (`.dc.html`) mit eigener Laufzeit und laufen außerhalb des Canvas nicht direkt im Browser.
- Alle Daten sind Beispieldaten.
- Farben sind darin als feste Werte geschrieben; im Produkt kommen sie aus den Theme-Variablen (siehe `CLAUDE.md`, Abschnitt Themes).
- Der Datenteil steht jeweils unten im `<script>`-Block (`renderVals()`), das Layout oben im Markup.

| Datei | Seite |
|---|---|
| `Main.dc.html` | Dashboard mit Konto-Menü (Neustart, Herunterfahren, Abmelden) und Bestätigungsdialog |
| `DashboardEdit.dc.html` | Dashboard im Bearbeiten-Modus (12-Spalten-Raster, Kacheln verschieben/vergrößern) |
| `Storage.dc.html` | Speicher: Volumes, RAID-Wiederaufbau, Plattentabelle |
| `Appearance.dc.html` | Einstellungen › Darstellung: Theme-Editor mit Themes, eigenen Farben, Akzent, Stil Flach/Glas/Kontur, Transparenz-, Unschärfe-, Ecken- und Schriftregler, Hintergrund (enthält alle Theme-Farbwerte) |
| `DockerList.dc.html` | Docker-Übersicht: Karten, Schnellmenü, Compose-Stack, Filter |
| `DockerEdit.dc.html` | Container bearbeiten: Pfade, Ports, Variablen, Labels mit Herkunft (Vorlage/TitanOS/Eigene), eigene Einträge hinzufügen, Live-Prüfungen, YAML live erzeugt |
| `AppStore.dc.html` | Apps: Quellen, Katalog, Installationspanel mit vorausgefüllten Feldern |
| `Terminal.dc.html` | Terminal-Panel: NAS-Shell und Container-Konsolen als Tabs |
| `VmConsole.dc.html` | VMs: VNC-Ansicht (größenveränderbar, drei Anzeigemodi, Vollbild), serielle Konsole |
| `Brand.dc.html` | Logo: Symbol und Schriftzug, Varianten, Regeln |
| `VmCreate.dc.html` | VM erstellen: Vorlagen, Kernauswahl mit P-/E-Kernen (Pinning), Platten (qcow2/img, Größen, mehrere Platten), vorhandene Abbilder vom NAS, GPU/iGPU durchreichen, NAS-Ordner per virtiofs |
| `Hardware.dc.html` | Einstellungen › Hardware: iGPU, NPU, NVIDIA, Verwendung Container/VM, USB-Geräte |
| `Plugins.dc.html` | Plugins: Rechte-Abfrage, Installationsschritte, NVIDIA-Einstellungen (Energie, Leistungslimit) |

Screenshots (optional) gehören nach `docs/design/screenshots/` mit demselben Dateinamen wie die Quelle, z. B. `Main.png`.

### Docker-Anpassung (0.5.1)

Der am 18. Oktober 2026 vom Projektinhaber bestätigte interaktive Folgeentwurf
ersetzt die feste Trennung aus `DockerList.dc.html`: Container und Stacks im
selben 12-Spalten-Raster, Breiten 3/4/8, Karten/Liste/Minimal. Minimal zeigt
Name und Zustand und öffnet Details per Namensknopf. „Seite anpassen“ bietet
Sortieren per Griff/Pfeilen, Anzeigeoptionen, Speichern/Abbrechen/Standard.
Kleine Bildschirme stapeln; Theme-Werte bleiben die aus `Appearance.dc.html`.
Die ursprüngliche Entwurfsdatei dokumentiert weiterhin die Container-Aktionen.

### Bedienung und Rückmeldungen (0.6.0)

Auf Wunsch des Projektinhabers bleibt die gesamte Docker-Oberfläche auf dem
Stand 0.5.9. Neue Aufgabenübersicht, Verbindungshinweise und Lesbarkeitsregeln
sind ausdrücklich auf die übrigen Seiten begrenzt. Die Einstellungssuche
filtert die alphabetischen Kacheln samt Zeitzone/Terminal. Die VM-Seite behält
Liste links und Konsole rechts; CPU/RAM sind getrennte Werte, Start/Herunterfahren
ist die Hauptaktion, seltene Aktionen liegen unter „Weitere Aktionen“.
Meldungen verlinken bekannte lokale Ziele; asynchron geladene Ziele werden
angesprungen. Aufgaben zeigen ausschließlich vorhandene API-Messwerte. Der separate
Administrator-Endpunkt `/api/v1/update/progress` liest nur RAM, damit die
Übersicht keine regelmäßigen Zugriffe auf den Bootstick auslöst.

Browserprüfung mit isolierten API-Daten: `scripts/test-ui-060.mjs` gegen einen
lokalen Vite-Server. Benötigt eine vorhandene Playwright-Installation (über
`PLAYWRIGHT_MODULE` wählbar) und Chromium (`BROWSER_EXECUTABLE` optional).
`UI_URL` wählt den Server, `UI_OUTPUT` den Screenshot-Ordner. `DOCKER_ONLY=1`
erzeugt Vergleichsbilder für Graphit/Hell bei 1440 und 390 Pixeln.
Die Testdaten gehören ausschließlich zum Testskript, nicht zum Produkt.
