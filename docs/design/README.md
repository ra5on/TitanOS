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

### Einstellungen und Sicherungen (0.6.1)

Vom Projektinhaber am 20. Oktober 2026 bestätigter Folgeentwurf: Die
Einstellungsübersicht enthält ausschließlich kompakte, alphabetische
Navigationskacheln. Datum und Uhrzeit erhalten eine eigene Seite, Terminal
und Konsolen stehen unter Zugang. Suchtreffer führen gezielt zu SSH,
Zertifikat, Terminal, DNS und den Sicherungsbereichen. Farben, Abstände,
Schrift und Radien folgen weiterhin dem bestehenden Theme-System.

„Sicherungen“ ist eine eigene Unterseite mit drei Bereichen: Datensicherung,
Systemkonfiguration und Wiederherstellung. Darüber stehen die letzten
Sicherungserfolge und der nächste geplante Lauf aus den vorhandenen APIs;
Fehler, fehlende Werte und ausgeschaltete Konfigurationssicherung werden
sichtbar angezeigt. Die Übersicht liest beim Öffnen und auf ausdrückliches
Aktualisieren, nicht zyklisch aus Sicherungsarchiven. Datenaufträge und
Konfigurationssicherungen behalten ihre bisherigen Funktionen. Wiederherstellen
verwendet die bestehende Dateiwiederherstellung bzw. erklärt die
Konfigurationswiederherstellung während der Ersteinrichtung eines Ersatzsticks.
Es gibt keinen neuen Live-Import der Systemkonfiguration.

Speicherbestätigungen bei Datum/Uhrzeit und Zugang stehen an der betreffenden
Einstellung und verschwinden nach fünf Sekunden. Ladefehler bieten einen
erneuten Abruf. Alle Texte liegen auf Deutsch und Englisch vor; mobile
Ansichten stapeln die drei Bereiche. Alte Stick- und Backup-Auftragslinks
werden weitergeleitet. Neue Links aus Meldungen und Aufgaben zeigen direkt
auf die Unterseiten. Direkte Einstellungsaufrufe beachten die Kontorolle.

`scripts/test-ui-061.mjs` prüft die Navigation, Fehler/Wiederholung, lokale
Rückmeldungen, Wiederherstellungsdialog, alte Links, Rollen und ausbleibendes
Archiv-Polling mit isolierten API-Testdaten. Screenshots entstehen bei 1440
und 390 Pixeln in Graphit und Hell. Aufruf wie beim 0.6.0-Skript oben.
