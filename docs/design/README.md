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
