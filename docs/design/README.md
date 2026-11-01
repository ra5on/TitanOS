# TitanOS – UI-Entwürfe

Diese Dateien sind die Quelltexte der Entwürfe aus dem Design-Canvas „TitanOS Web-UI“. Sie sind **Vorlage für Aufbau, Abstände, Farben und Verhalten**, kein Code zum Übernehmen:

- Sie nutzen ein Entwurfsformat (`.dc.html`) mit eigener Laufzeit und laufen außerhalb des Canvas nicht direkt im Browser.
- Alle Daten sind Beispieldaten.
- Farben sind darin als feste Werte geschrieben; im Produkt kommen sie aus den Theme-Variablen (siehe `docs/rules/WEB-UI.md`, Abschnitt Themes).
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
| `VmPage.html` | VMs: Liste, Kopfzeile, Reiter Übersicht/Bildschirm/Snapshots, Übersichtskacheln, Bildschirm-Werkzeugleiste (statische Skizze, ohne Entwurfslaufzeit lesbar) |
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

### Cluster: Wege, Versionen, Alle aktualisieren (0.44.0)

Vom Projektinhaber am 1. November 2026 nach Screenshots bestätigt („Los“,
Leitlinie „freie Wahl, wie bei allem“). Einstellungen › Cluster: oben die
Knotenliste (Punkt für den Zustand, Rolle und Versions-Chips, „über <Weg> ·
n ms“, Warnung bei Ausweichweg und Uhrzeit, rechts „Verwalten“,
„Aktualisieren“ bei älterer Version und „⋯“ mit Weg je Knoten, Verwalten
erlauben, Admin-Rolle, Entfernen; im Kopf „Wege prüfen“ und „Alle
aktualisieren“), darunter ein einspaltiges Formular wie das App-Formular
(`FormRow`/`FormSection`/`Toggle`) mit den Abschnitten Einstellungen,
Verbindung zwischen den Knoten, Wege, Meldungen; Beitrittsinformation und
Verlassen unverändert darunter. „Wege prüfen“, „Weg zu <Knoten>“ und „Alle
Knoten aktualisieren“ (nacheinander vorausgewählt) sind einfache Dialoge.
Ein Knoten mit anderer Version ist verwaltbar; der Balken oben nennt beide
Versionen. Updates zeigt die Knoten des Clusters als Tabelle; das Dashboard
hat die Kachel „Cluster“. Prüfung `scripts/test-ui-cluster.mjs`, Regeln
`docs/rules/CLUSTER.md`.

### App-Formular und Docker-Kopf (0.43.0)

Vom Projektinhaber am 1. November 2026 nach drei Screenshot-Runden bestätigt
(„Ja, jetzt haben wir es. Das ist top.“ bzw. „Passt“). Installieren und
Bearbeiten einer App zeigen dasselbe einspaltige Formular: Bezeichnung links,
Feld rechts, grauer Hinweis darunter, Abschnitte „Netzwerk und Zugriff“,
„Grafikkarte und Geräte“, „Pfade, Ports und Variablen“, Schalter
Basis/Erweitert oben rechts, nach „Installieren“ eine Protokollseite. Die
Docker-Seite hat eine Kopfzeile (Suche, Ansicht, Zahnrad, „App installieren“
mit Pfeil), Filter in der Statuszeile und Sammelaktionen unter der Liste; die
Liste selbst bleibt unverändert. Verworfen: Assistent mit Schritten, Reiter
mit langen Schiebereglern, Kacheln unterschiedlicher Höhe. Regeln und Umfang:
`docs/rules/DOCKER.md`, Prüfung `scripts/test-ui-appform.mjs`. Die älteren
Entwürfe `DockerEdit.dc.html` und `AppStore.dc.html` gelten für Aufbau und
Anordnung nicht mehr, nur noch für Herkunft je Feld und Live-Prüfungen.

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

### Einstellungen mit Seitenleiste (28. Oktober 2026)

Vom Projektinhaber am 28. Oktober 2026 bestätigter Entwurf „A – Seitenleiste“.
Er ersetzt die Kachelübersicht der Abschnitte 0.6.1 und 0.30.0 (Gerüst der
Seiten bleibt). Desktop: links eine Seitenleiste von etwa 250 px auf eigener
Kachelfläche, unter der 60-px-Kopfzeile haftend und eigenständig scrollend.
Oben die Einstellungssuche (Bereiche und Abschnitte wie bisher; Pfeiltasten
wählen, Enter öffnet, Treffer springen zum Abschnittsanker, Escape leert),
darunter die Gruppen mit kleiner Großbuchstaben-Überschrift und Zeilen von
etwa 32 px mit 16-px-Symbol und Titel. Die aktuelle Seite trägt eine
Akzenttönung (Text aus Akzent und Textfarbe gemischt, damit auch Hell 4,5:1
erreicht) und `aria-current="page"`; die Leiste ist ein `nav` mit
`aria-label`. Gruppen in fester Reihenfolge (`settingsAreas`, Stand 30.10.2026):
System, Netzwerk, Sicherheit und Fernzugriff, Hardware und Energie, Daten und
Speicher, Integrationen, Persönlich; oben „Übersicht“ (Aufmerksamkeit, dieses NAS);
innerhalb der Gruppen nicht alphabetisch. Bereiche mit Zustand bekommen einen
Punkt mit Tooltip und Screenreader-Text, optional ein „neu“-Abzeichen
(`isNew`). Rechts die bisherigen Seiten unverändert (`.settings-page.column`,
höchstens 64rem); der Brotkrumen zu „Einstellungen“ entfällt.

`/settings` zeigt rechts alle Bereiche als kompakte Zeilen je Gruppe
(Symbol in Akzenttönung, Titel, Zustand als Chip mit Text, Pfeil), eine
Spalte. Mobil bis 760 px: keine Seitenleiste; `/settings` ist dieselbe Liste
in voller Breite mit Suchfeld darüber, Unterseiten haben oben den Rückweg
„‹ Einstellungen“ (44 px hoch). Kein seitliches Scrollen. Knöpfe und Felder
sind innerhalb der Einstellungen auf dem Desktop kompakter (etwa 30 px,
13 px Text, über `--control-height` und die Dichte); Touch bleibt bei 44 px.
Zustände werden einmal beim Öffnen der Einstellungen gelesen, nicht je Seite
und nicht zyklisch. Browserprüfung: `scripts/test-ui-settings-sidebar.mjs`
(Bilder unter `out/validation/ui-settings-sidebar/`).

### Einstellungsseiten aufgeräumt (0.30.0)

Auftrag des Projektinhabers vom 27. Oktober 2026: alle Seiten hinter der
Einstellungsübersicht einheitlich. Eine Spalte von höchstens 64rem; jeder
Abschnitt ist eine Kachel mit Innenabstand (`.tile.panel`) und der kleinen
Großbuchstaben-Überschrift `.tile-title`; Felder mit Beschriftung darüber und
lesbarer Breite (`label.stack`), Häkchen mit Hinweis eingerückt darunter,
Knopfleisten `.row-actions`, Unterteilungen mit Linie (`.divided`). Neu:
„HTTPS-Namen für Apps“ unter Netzwerk und Dienste (siehe `docs/APP-PROXY.md`).
Ebenso „Firewall“ (Symbol Schild): Hauptschalter in einer Kachel, darunter
Reiter (`Tabs.svelte`, `?tab=`) Zonen, Netze, Richtlinien, Auto-Sperre und
Protokoll. **Zonen** (nach UniFi): Matrix Quell-Zone × Ziel (NAS, Docker-Apps,
Zonen) in `.table-wrap`/`.data-table`, Zellen als Knöpfe in den Statusfarben
„Erlaubt“ (grün), „Blockiert“ (rot), „Teilweise“ (gelb) mit Kurztext „n
erlaubt/gesperrt“, nicht über das NAS laufende Zellen grau gestrichelt mit „—“
und Tooltip; ein Klick öffnet darunter die Richtlinien der Zelle als Karten mit
↑/↓ („erste passende entscheidet“) und der Vorgabe als `Segments`; darunter die
Zonen mit ihren Netzen, Reihenfolge eigener Zonen per ↑/↓, „Zone anlegen“
(Dialog: Name, Netze als Häkchen gruppiert nach Netzwerk/VLAN, Docker-Netze,
VM/LXC-Netze und Docker im LAN – letztere nicht wählbar –, Quellnetze).
**Netze**: Tabelle aller erkannten Netze mit Art, Gerät, Subnetz und
Zonenauswahl („nach Adresse“ bzw. „Standard-Zone Docker“ oder eine eigene
Zone). **Richtlinien**: alle als Karten (auch mobil), filterbar nach Von/Nach.
`TrialBanner` und die haftende Leiste „Auf Probe übernehmen“ nur auf Zonen,
Netze und Richtlinien; die Auto-Sperre speichert getrennt sofort (siehe
`docs/FIREWALL.md`).

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

### Dateimanager (Entwicklungsstand nach 0.6.1)

Vom Projektinhaber bestätigte Ergänzungen vom 20. Oktober 2026: Details,
kleine Symbole und große Symbole als umschaltbare Ansichten. Farben, Flächen
und Radien folgen `Appearance.dc.html`; die Docker-Oberfläche bleibt unverändert.
Rasterbild-Miniaturen bis 2 MiB laden erst bei Sichtbarkeit. Größere Dateien
behalten das Dateisymbol und lassen sich über die vorhandene Vorschau öffnen.

Rechtsklick auf einen ausgewählten Eintrag erhält die Mehrfachauswahl und
bietet gemeinsame Aktionen; Rechtsklick auf freie Fläche bietet Ordneranlage,
Upload, Einfügen und Ansichten. Tastenkürzel Strg/Cmd+A/C/X/V, F2 und Entf
beachten Eingabefelder, modale Dialoge und Schreibrechte. Einfügen öffnet den
vorhandenen Kopier-/Verschiebedialog am Zielordner; Löschen bleibt bestätigt.
Die Pfadzeile navigiert innerhalb des gewählten Speicherorts und kopiert
relative Ordnerpfade; sie gewährt keinen zusätzlichen Zugriff auf Host-Pfade.

Ansicht, Sortierung, Favoriten und letzter Freigabe-/Volume-Speicherort werden
im Benutzerkonto auf dem NAS gespeichert (`files.json`, Preferences-API).
Neue Browser und neue Anmeldungen laden diese Einstellungen. Schreibfehler
werden mit einer Wiederholungsaktion angezeigt. Verlauf, Suche, Auswahl und
zum Einfügen vorgemerkte Dateien gelten für den aktuellen Seitenbesuch.

`scripts/test-files-ui.mjs` prüft Ansichten, neue Browserkontexte, Speicherfehler,
Mehrfachauswahl, Zwischenablage-Aktionen, Tastenkürzel, Pfade und Rollen mit
isolierten API-Testdaten. Backend-Tests prüfen persistente, kontogetrennte
Einstellungen. Screenshots liegen unter `out/validation/files-names-trash/`.

USB-Backups stehen als feste Einträge mit Zielpfad, letztem Sicherungserfolg,
Laufstatus und freiem Speicher links im Dateimanager. Der Fortschrittsabruf
liest nur RAM; unbekannte Gesamtgrößen erscheinen als unbestimmter Fortschritt.
„Backup wiederherstellen“ kopiert den ganzen Backup-Ordner, „Auswahl
wiederherstellen“ die markierten Einträge. Beide öffnen den vorhandenen
Zielordnerdialog mit einem neuen, separaten Wiederherstellungsordner auf einer
beschreibbaren Freigabe oder einem Volume. Bestehende Zielordner werden
abgelehnt, Dateien nicht überschrieben und Sicherungsdateien nicht verschoben.

Abschlussmeldungen verlinken den Zielordner; Fehler behalten den betroffenen
Pfad. Die Auswahl zeigt ihre Dateigröße, ausgewählte Ordner werden nur auf
Knopfdruck berechnet. Punktdateien lassen sich über eine im Konto gespeicherte
Option anzeigen. Vor Kopierkonflikten zeigt der Dialog Quelle und Ziel mit
Größe und Änderungszeit; Ersetzen benötigt eine zusätzliche Bestätigung. Bei
Auswahl von Ersetzen oder Einzelentscheidungen startet die begrenzte Zielprüfung
automatisch, ohne vorgeschalteten Kopierklick. Zielwechsel verwerfen Vorschau
und Bestätigung, verspätete Antworten werden verworfen. Fehler bieten erneutes
Prüfen; bei gekürztem Vergleich bleibt Ersetzen gesperrt.
USB-Backup-Laufwerke sind in der Dateiseite zunächst gegen Änderungen
geschützt. Ein Dialog schaltet Änderungen für den aktuellen Speicherort frei;
Speicherortwechsel und Verlassen der Seite beenden diese Freischaltung.

Fehlgeschlagene und abgebrochene Datei-Aufträge bieten „Offene Dateien erneut
versuchen“. Bereits abgeschlossene Dateien und verschobene Teilbäume werden
übersprungen; zuvor gewählte neue Zielordnernamen bleiben erhalten. Seit 0.16.0 liegen Aufträge mit Ziel auf einem Daten-Volume zusätzlich als Plan und Ereignisjournal dort und können nach titand-Neustart geprüft fortgesetzt werden. Andere Ziele bleiben im RAM. Aktuelle Rechte werden erneut geprüft; beim Verschieben werden nach
Unterbrechung geänderte Quelldateien erhalten und als Fehler gemeldet.

Der Papierkorb bietet zusätzlich eine flache Dateiliste und Sortierung nach
Name, Größe, ursprünglichem Pfad oder Löschzeit. Vor der Wiederherstellung
zeigt ein Dialog die Zielpfade und Namenskonflikte; vorhandene Dateien bleiben
erhalten. Neue Löschungen speichern ursprünglichen Pfad und Zeitpunkt als
Dateisystemattribut, auch für gelöschte Ordner. Bei älteren, über Samba
abgelegten oder auf Dateisystemen ohne Attributunterstützung liegenden
Einträgen dienen die vorhandenen Papierkorbpfade und Zeitstempel als Rückfall.

„Zwei Ordner anzeigen“ öffnet eine zweite Ordnerliste neben der bisherigen;
auf schmalen Bildschirmen stehen beide untereinander. Richtungsknöpfe öffnen
den Kopier-/Verschiebedialog mit vorausgewähltem Ziel. Die Ansicht ist im
Benutzerkonto gespeichert. Der Schutz für USB-Backups gilt auch für die
zweite Liste und für wiederholte Aufträge.

Uploads durchlaufen vor dem Anlegen von Ordnern und der Übertragung eine
Prüfung von Ziel, Anzahl, Gesamtgröße, freiem Speicher und Namenskonflikten.
Konflikte lassen sich einzeln überspringen, umbenennen oder nach zusätzlicher
Bestätigung ersetzen. Später entstehende Konflikte prüft weiterhin die
Upload-API. Eigenschaften zeigen den vollständigen Pfad innerhalb des
Speicherorts, Dateityp, Größe, Dateisystemzeiten und Rechte. SHA-256 wird nur
auf Anforderung berechnet; ein Vergleichswert lässt sich direkt einfügen.

Die ausdrücklich gestartete Unterordnersuche ergänzt den lokalen Dateifilter.
Sie filtert Namen, Endungen, Größe und Änderungszeit, zeigt vollständige
relative Pfade und öffnet den zugehörigen Ordner. Sie verfolgt keine Symlinks
und blendet interne, geheime und VM-geschützte Pfade aus. Pro Suche gelten
30 Sekunden, 50.000 geprüfte Einträge und 1.000 Treffer; eine begrenzte
Ergebnisliste wird ausdrücklich gekennzeichnet. Die Namenssuche bleibt ohne
Index. „Im Inhalt suchen“ nutzt den optionalen, ab Werk ausgeschalteten
Volltextindex (Einstellungen › Dateisuche, siehe `docs/FILE-SEARCH.md`) und ist
nur auswählbar, wenn der Index den Speicherort abdeckt.

„Mehrere umbenennen“ zeigt eine Vorschau für Textersetzung, Präfix, Suffix und
Nummerierung. Dateiendungen und die nicht ausdrücklich geänderte Schreibweise
bleiben erhalten. Ungültige, doppelte oder vorhandene Zielnamen blockieren die
Anwendung. Die API prüft die Dateiversionen vor und während der Anwendung und
veröffentlicht neue Namen ohne Überschreiben. Bei Teilfehlern bleiben bereits
abgeschlossene Änderungen einzeln rückgängig machbar.

„ZIP öffnen“ zeigt den Archivinhalt und erlaubt die Auswahl einzelner Dateien
oder aller Inhalte. Die Zielprüfung nennt den neuen Zielordner, Umfang und
freien Speicher. Vorhandene Zielnamen blockieren das Entpacken; Zusammenführen
und Ersetzen sind hier nicht vorgesehen. Die Extraktion läuft als abbrechbarer
Dateiauftrag, schreibt temporär auf das Zielvolume und veröffentlicht den neuen
Ordner erst nach vollständiger Extraktion und CRC-Prüfung. Interne/absolute
Pfade, Traversal, Symlinks, verschlüsselte Einträge, doppelte Namen und
Datei-/Ordner-Kollisionen werden abgelehnt. Grenze: 20.000 Archiveinträge und
1 TiB entpackter Gesamtumfang. Das Archiv wird nicht ausgeführt.

Der Vergleich der beiden geöffneten Ordner zeigt nur links/rechts vorhandene
und unterschiedliche Einträge sowie die Anzahl der Übereinstimmungen. Standard
sind Größe und Änderungszeit; optional vergleicht SHA-256 gleich große Dateien
inhaltlich. Keine automatische Synchronisation oder Löschung. Beide Scans sind
auf je 50.000 Einträge und gemeinsam zwei Minuten begrenzt; unvollständige
Scans liefern keine vermeintlich fehlenden Dateien.

„Letzte Änderungen“ bietet kontogetrennt Rückgängig für Umbenennungen,
Papierkorbaktionen und sicher umkehrbare Verschiebungen. Höchstens 20 Aktionen
pro Konto bleiben 30 Minuten im RAM. Neustart beendet diesen Verlauf. Aktuelle
Root-Rechte und -Zuordnung, Eintragsidentität, Größe, Änderungs-/Metadatenzeiten
sowie der vollständige Verzeichnisbaum werden vor der Rücknahme geprüft.
Neu belegte Originalnamen blockieren die Rücknahme. Dateien werden niemals
überschrieben; über Dateisystemgrenzen wird erst vollständig in einen temporären
Zielordner kopiert. Ersetzen, Zusammenführen, endgültiges Löschen und Bäume,
deren Zustand nicht innerhalb der Prüfgrenzen erfasst werden kann, werden
nicht als umkehrbar angeboten. Der USB-Bedienungsschutz gilt auch hier.

### Dateimanager: Übertragungen, Download, Verbrauch und Aktualisierung

Kopier- und Verschiebeaufträge laufen je Konto in einer Warteschlange. Bei gleicher
Priorität gilt die Eingangsreihenfolge; höhere Priorität erhält am nächsten
Dateiblock Vorrang. Pause hält temporäre Dateien offen und gibt die Warteschlange
frei. Fortsetzen prüft den aktuellen Root-Zugriff. Ein optionales Limit von
0–1024 MiB/s gilt je Auftrag (0 = unbegrenzt), mit höchstens einem 256-KiB-Block
Vorlauf. Innerhalb derselben Dateiwurzel ohne Einzelentscheidungen erfolgt Verschieben als Umbenennung;
ein bereits laufender einzelner Dateisystemaufruf lässt sich nicht pausieren.
Extraktion und Browser-Uploads behalten ihre eigenen Abläufe. Seit 0.16.0 erhalten Kopier-/Verschiebeaufträge mit Ziel auf einem Daten-Volume dauerhafte Checkpoints; andere Ziele bleiben im RAM. Siehe `../WORKFLOWS.md`.

Kopieraufträge haben keine feste Datei-/GB-Obergrenze. Die Dateimengen-
Vorschau kann nach 15 Sekunden auf die vollständige Berechnung beim Start
verweisen und normales Kopieren freigeben; Speicherplatz und Rechte prüft
das Backend vor dem ersten Schreibzugriff auf Nutzdateien. Bestehende
Schutzgrenzen der Ersetzen-/Einzelkonfliktprüfung bleiben erhalten.

Beendete Aufträge erscheinen vollständig statt auf fünf Einträge gekürzt.
„Auftragseintrag entfernen“ entfernt den eigenen Eintrag sofort ohne Dialog oder
Kennungseingabe; „Alle beendeten Aufträge entfernen“ räumt sämtliche eigenen
beendeten RAM-/Volume-Einträge auf. Laufende/abschließende Aufträge bleiben,
Nutzdateien und Teilkopien ebenso. Fehler bleiben sichtbar und wiederholbar.
Unter „Angeschlossene Laufwerke“ erscheinen auch interne SATA-/SAS-/NVMe-
Dateisysteme mit Name, Dateisystem und Anschluss; in Benutzung befindliche
Dateisysteme erhalten keinen Einbinden-Knopf. Ein lesender NTFS-Ausweichweg
wird beim eingebundenen Laufwerk kenntlich gemacht.

Mehrfachauswahl und Ordner öffnen vor dem ZIP-Download einen Prüfdialog mit
Dateianzahl, unkomprimierter Größe und Archivnamen. Der Download bleibt ein
Stream ohne ZIP-Zwischenspeicherung. Leere Ordner sind enthalten, Verknüpfungen,
interne und geschützte Dateien ausgenommen. Die Vorschau ist keine Momentaufnahme:
zwischen Prüfung und Download können sich Dateien ändern.

„Speicherverbrauch analysieren“ prüft nur nach ausdrücklichem Start. Anzeige:
Datei-/Ordneranzahl, logische Gesamtgröße, direkte Kinder nach Verbrauch und
höchstens 50 größte Dateien aus allen Unterordnern. Keine Inhaltsabfragen,
Symlink-Verfolgung oder Hintergrundindexierung. Die Sichtbarkeit versteckter
Dateien gilt auch hier. Maximal 50.000 Einträge/30 Sekunden; bei Fehlern oder
Erreichen der Grenze keine irreführenden Teilsummen.

Eigenschaften zeigen die TitanOS-Lese-, Schreib- und Löschrechte des angemeldeten
Kontos zusätzlich zu Unix-Modus/UID/GID. Ursachen unterscheiden Freigabe mit
Leserechten, Systemstick, VM-Schutz und den USB-Backup-Bedienungsschutz. Die
Anzeige ersetzt keine Prüfung beim tatsächlichen Zugriff und verspricht keine
Schreibbarkeit eines möglicherweise schreibgeschützt eingebundenen Datenträgers.

Beide geöffneten Ordner aktualisieren ihre unmittelbare Dateiliste alle zehn
Sekunden, nur bei sichtbarer Seite und außerhalb geöffneter Dialoge oder
Eingabefelder. Keine rekursiven Scans; der Systemstick bleibt ausgenommen.
Vorhandene Auswahl, Suche und Scrollposition bleiben erhalten; weggefallene
Namen werden aus der Auswahl entfernt. Veraltete Antworten nach Navigation oder
zwischenzeitlicher Aktualisierung werden verworfen. Fehler behalten die letzte
Liste sichtbar und werden angezeigt. Das ist kein Dateisystem-Snapshot.

### Textfassungen, Upload-Warteschlange, Duplikate und gezieltes Kopieren

Der Texteditor liest die vorhandene `.prev`-Fassung nur auf Anforderung, stellt
sie der aktuellen gespeicherten Fassung gegenüber und stellt sie nach Bestätigung
über den bestehenden versionierten Speichervorgang wieder her. Dadurch bleibt die
ersetzte aktuelle Fassung als `.prev` erhalten. Keine unbegrenzte Versionshistorie.
Ungespeicherte Änderungen müssen vorher gespeichert oder verworfen werden.
Typ-/Größenprüfung, Root-Rechte, VM-/Stick-Schutz und Symlink-Ausschluss gelten
auch für vorherige Fassungen; Änderungen an der aktuellen Datei blockieren eine
Wiederherstellung aus einem veralteten Editor.

Die Upload-Warteschlange bietet gemeinsame Pause/Fortsetzen-Knöpfe, Wiederholen
nur fehlgeschlagener Dateien und einen Gesamtfortschritt. Die vorhandene
Geschwindigkeit/Restzeit gilt für aktive und wartende Transfers. Pause wartet
auf den laufenden Datenblock. Dateien und Entscheidungen bleiben beim Wechsel
innerhalb der Web-UI erhalten, aber nicht nach Schließen des Tabs. Persistente
Teiluploads können nach erneuter lokaler Dateiauswahl weiterhin geprüft und
fortgesetzt werden. Fehlgeschlagene Einträge verschwinden nicht beim Entfernen
abgeschlossener Uploads. Neue Uploads bleiben bei pausierter Gesamtwarteschlange
zunächst pausiert.

„Doppelte Dateien finden“ startet ausdrücklich einen Scan des geöffneten Ordners,
begrenzt auf 50.000 Einträge und zwei Minuten. Erst nach Größenvergleich werden
SHA-256-Prüfsummen gleicher Größen berechnet. Keine Symlinks oder geschützten
Pfade; Punktdateien folgen der bestehenden Ansichtseinstellung. Ergebnisse zeigen
vollständige relative Fundstellen und erlauben das Öffnen des jeweiligen Ordners.
Keine automatische Auswahl. Löschen benötigt eine eigene Vorschau/Bestätigung,
nutzt nach Möglichkeit den Papierkorb und prüft die Eintragsversionen erneut.
Der Benutzer entscheidet, welche Kopie erhalten bleibt. Kein Hintergrundindex.

Im Kopierdialog bietet „Für jede Datei entscheiden“ für die aufgelisteten
Konflikte Überspringen, Ersetzen oder einen ausdrücklich eingegebenen anderen
Namen. Größe/Datum beider Seiten bleiben sichtbar. Ersetzen braucht zusätzlich
eine Bestätigung; bei abgeschnittener Vorschau sind Einzelentscheidungen und
Ersetzen gesperrt. Ungeprüfte zusätzliche Konflikte werden übersprungen.
Benannte neue Ziele werden ohne Überschreiben veröffentlicht. Versionen von
Quelle und vorhandenem Ziel werden erneut geprüft; ein zwischenzeitlicher
Zielwechsel stoppt die betroffene Kopie. Bei übersprungenen Untereinträgen einer
Verschiebung bleibt der Quellordner erhalten.

Aus dem Ordnervergleich können einzelne Dateien links → rechts oder rechts →
links übernommen werden. Eine Vorschau zeigt Richtung und relative Pfade;
Ersetzen verlangt Bestätigung. Fehlende Elternordner werden innerhalb des
berechtigten Ziel-Roots angelegt, vorhandene Verzeichnisse behalten ihre Rechte.
Datei-/Ordner-Typkonflikte sind nicht auswählbar. Dateiversionen aus dem Vergleich
werden vor der Kopie und vor Veröffentlichung erneut geprüft. Die Auswahl läuft
als normaler Kopierauftrag mit Fortschritt/Pause/Abbruch; keine automatische
Synchronisation oder Löschung. Teilweise abgeschlossene Kopien bleiben bei einem
späteren Fehler erhalten. Der USB-Bedienungsschutz gilt auch für Vergleichsziele
und das Löschen von Duplikaten.

### Ordnerbaum, Werkzeuge, Ziehen und große Ordner

Die Werkzeugleiste bündelt Unterordnersuche, Speicheranalyse, Duplikatsuche und
Ordnervergleich unter „Werkzeuge“. Richtungsknöpfe zwischen zwei Ansichten
erscheinen bei vorhandener Auswahl. Häufige Aktionen bleiben direkt erreichbar.
Der linke Ordnerbaum lädt unmittelbare Unterordner erst beim Aufklappen über die
bestehende autorisierte Dateiliste. Aufklappen navigiert nicht; der Namensknopf
öffnet den Ordner. Fehler lassen sich erneut versuchen, Einklappen bricht laufende
Anfragen ab. Versteckte Ordner folgen der Kontoeinstellung. Große Unterordnerlisten
zeigen zunächst 100 Namen und bieten weitere auf Knopfdruck. Volumes bleiben unter
„Erweiterter Zugriff“; keine rekursiven Hintergrundabfragen.

Interne Dateiauswahl lässt sich auf einen Ordner oder die andere Ansicht ziehen.
Die Quelle wird ausschließlich im Speicher der geöffneten Seite gehalten, nicht
als vertrauenswürdiger externer Drag-Payload eingelesen. Vor jeder Übertragung
zeigt ein Dialog Quellpfade und Ziel, mit Kopieren/Verschieben/Abbrechen. Danach
folgt der vorhandene Kopierdialog mit Konfliktprüfung. Schreibrechte, USB-Sperre
und Systemstick-Ausschluss gelten weiterhin; externe Datei-Drops bleiben Uploads.

Ab mehr als 200 Treffern rendern beide Ansichten nur den sichtbaren Ausschnitt
plus einige Reservezeilen. Details und beide Symbolgrößen nutzen einen eigenen,
per Tastatur scrollbar erreichbaren Bereich. Suche, Sortierung, Bereichsauswahl
und „Alle auswählen“ arbeiten auf der vollständigen geladenen Liste. Namen bleiben
unverändert; Auswahlprüfungen verwenden Mengen statt quadratischer Suchschleifen.
Zurück/Vorwärts bewahrt auch die innere Scrollposition. Die API lädt weiterhin die
vollständige unmittelbare Ordnerliste, es gibt keine serverseitige Seitennavigation.

Der Textvergleich stellt vorherige und aktuelle Fassung mit Zeilennummern und
Markierungen für hinzugefügte, entfernte und geänderte Zeilen gegenüber. Vertikales
und horizontales Scrollen sind synchronisiert; Zeilennummern bleiben horizontal
sichtbar. Große Vergleiche rendern nur einen Ausschnitt. Der Zeilenabgleich verwendet
höchstens eine Million Vergleichszellen; größere zusammenhängende Änderungsblöcke
werden ausdrücklich als positionsweiser Vergleich gekennzeichnet. Wiederherstellung
und Versionsprüfung verwenden unverändert den vorhandenen sicheren Speicherweg.

### Medien, Papierkorbfilter, Fortschritt und Tastatur

Die Vorschau unterstützt PDF bis 20 MiB über den nativen PDF-Betrachter des
Browsers sowie Audio (MP3, WAV, OGG, M4A, FLAC) und Video (MP4, WebM, OGV).
Audio/Video verwenden native Bedienelemente ohne Autoplay und streamen über den
berechtigten Download mit HTTP-Range. Der Server erlaubt dafür ausschließlich
festgelegte Medien-MIME-Typen, setzt `nosniff`, `no-store` und eine Sandbox-CSP;
HTML und andere Dateitypen bleiben Downloads. Bild-, Text- und PDF-Downloads
in den Browser-RAM werden während des Lesens begrenzt und beim Schließen
abgebrochen; Blob-URLs werden freigegeben. Die tatsächliche Medienwiedergabe
hängt von den Browser-Codecs ab. Bei Fehlern bleibt ein Download möglich.
Die Bildvorschau bietet Vor/Zurück und linke/rechte Pfeiltaste in der sichtbaren
Sortierreihenfolge, ohne versteckte Dateien hinzuzufügen.

Die flache Papierkorbansicht filtert nach ursprünglichem Ordner und inklusivem
Löschdatumsbereich. Filter begrenzen auch Gesamtauswahl und Löschvorschau; keine
automatische Löschung älterer Dateien. Sie zeigt Dateigrößen und belegte
Datenblöcke der zugänglichen Dateien, Hardlinks je Geräte-/Inode-Paar einmal
gezählt. Ordner-Metadaten sind ausgenommen; belegte Blöcke sind keine Zusage über
den durch Löschen frei werdenden Speicher, etwa bei weiteren Hardlinks oder
Snapshots. Ältere Papierkorbeinträge können weiterhin Änderungsdaten als Ersatz
für fehlende Löschmetadaten verwenden. Die bisherigen Scan- und Rechtegrenzen
gelten unverändert.

Dateiaufträge zeigen Gesamtfortschritt, aktuellen Pfad und je aktivem Auftrag
eine Geschwindigkeit aus den zuletzt beobachteten Zählerdifferenzen. Die Restzeit
ist eine ausdrücklich ungefähre Minutenschätzung. Pause, Warteschlange, Neustart
der Messung, rückläufige Zähler, Stillstand und lange Beobachtungslücken zeigen
keine veraltete Schätzung; unbekannte Gesamtgrößen bleiben unbestimmt. Ausgefallene
Statusabfragen werden angezeigt. Bekannte Fehler werden verständlich erklärt,
der betroffene Pfad und technische Details bleiben verfügbar. Ein Auftrag stoppt
weiterhin am ersten Fehler; die Anzeige behauptet keine vollständige Fehlerliste
aller noch nicht verarbeiteten Dateien.

Beide Dateilisten unterstützen Pfeiltasten, Home/End, Umschalt-Auswahl, Enter zum
Öffnen und Leertaste zur Vorschau. Virtuelle Listen scrollen den Tastaturfokus in
den gerenderten Bereich. Textfelder und native Bedienelemente behalten ihre
Tasten; die rechte Ansicht löst keine Aktionen auf der linken Auswahl aus. F1
und „Tastenkürzel“ erklären zusätzlich die vorhandenen Aktionen der linken Ansicht
(Strg/Cmd+A/C/X/V, F2, Entf). Löschen öffnet weiterhin nur den Bestätigungsdialog.

### Favoriten, Ordneransichten, neue Dateien und Auswahlhilfen

Favoriten lassen sich über einen eigenen Dialog benennen, per Pfeilen sortieren
und entfernen. Speichern übernimmt die geordnete Liste in die Kontoeinstellungen;
Abbrechen lässt sie unverändert. Bis zu 50 Favoriten, Bezeichnungen bis 100 Zeichen.
Die tatsächlichen Speicher- und Ordnernamen werden dadurch nicht verändert.

„Ansicht für diesen Ordner merken“ speichert Ansicht, Sortierkriterium und Richtung
je Root/Pfad im Benutzerkonto (höchstens 200 Ordner). Nicht individuell gespeicherte
Ordner verwenden die gemeinsame Vorgabe. Entfernen einer Ordnerausnahme stellt
diese Vorgabe wieder her. Kein Local-/Session-Storage und keine Schreibzugriffe
beim bloßen Navigieren innerhalb derselben Freigabe.

Neue Text-, Markdown- und JSON-Dateien erhalten den gewählten Namen mit passender
Endung und öffnen den vorhandenen Editor. JSON beginnt mit einem leeren Objekt.
Die Datei wird über ein temporäres Geschwister und RENAME_NOREPLACE veröffentlicht;
vorhandene Namen und Symlinks werden nicht überschrieben. Zielverzeichnis pinnen,
aufgelöste geschützte Pfade/Geheimnisse und aktuelle Root-Rechte prüfen. Kein
Anlegen auf dem Systemstick. USB-Bedienungsschutz gilt weiterhin.

Unter Werkzeuge stehen „Auswahl umkehren“ und eine Auswahl nach Bildern, Videos,
Audio, Dokumenten, Ordnern oder sonstigen Dateien. Grundlage ist die vollständige
gefilterte Liste, nicht nur der gerenderte Ausschnitt. Typgruppen basieren auf
Endungen, nicht auf einer Inhaltsprüfung. Anzahl, Dateigrößen und ausdrücklich
angeforderte Ordnergrößen verwenden die bestehende Auswahlzusammenfassung.

Der Kopierdialog zeigt Quelle/Pfade, Ziel, Dateianzahl, logischen Umfang und den
zuletzt ermittelten freien Zielspeicher schon vor dem Start. Die Umfangsprüfung
läuft nur beim Öffnen oder geänderter Quelle und wird beim Schließen abgebrochen.
Gleicher Root/Zielordner beim Verschieben und eigene Unterordner werden frühzeitig
gesperrt; Kopien im gleichen Ordner weisen auf die benötigten neuen Namen hin.
Speicherwarnungen sind Schätzungen, weil Umbenennen innerhalb eines Dateisystems,
Ersetzen und gleichzeitige Zugriffe den tatsächlichen Bedarf verändern können.
Konfliktprüfung und serverseitige Rechte-/Pfadprüfung bleiben maßgeblich.

### Von Link herunterladen (nach 0.24.0)

Letzter Eintrag im Menü „Neu“ (Globus-Symbol) und im Kontextmenü der freien
Fläche, nur bei Schreibrecht und ohne aktive USB-Backup-Sperre. Ein schmaler
`Dialog.svelte` mit Link, „Name und Größe abfragen“ (Größe und Typ in einer Zeile
daneben), Name und optionaler SHA-256; Hinweise unter den Feldern, ungültige
Eingaben sperren „Herunterladen“. Bei vorhandenem Namen bleibt der Dialog offen,
erklärt in einem Warnkasten, dass nichts überschrieben wird, und bietet
„Beide behalten“; einen Ersetzen-Knopf gibt es nicht. Konten mit Rolle Benutzer
sehen den Hinweis, dass nur Internetadressen erreichbar sind. Der Auftrag
erscheint in der Liste „Dateiaufträge“ mit Name, Größe (oder „Größe unbekannt“),
Tempo, Restzeit, Abbrechen und Wiederholen. Verhalten und Grenzen:
`docs/FILE-DOWNLOADS.md`; Browserprüfung `scripts/test-ui-url-download.mjs`.

### VM anlegen und bearbeiten (bestätigter Folgeentwurf, 24. Oktober 2026)

Variante 1 des vom Projektinhaber gewählten Vergleichs ersetzt den langen
Einspaltenaufbau aus `VmCreate.dc.html`. Fünf kompakte Kacheln im Dashboard-Stil:
Allgemein über beide Formularspalten, darunter CPU/RAM und Datenträger sowie
Installation/Start und Netzwerk/Erweiterungen. Rechts eine haftende Zusammenfassung
mit den beiden Erstellaktionen; auf schmalen Bildschirmen eine Lesespalte und
die Zusammenfassung darunter. Schmale Symbol-/Abschnittsköpfe, vorhandene
Theme-Werte und lokale IBM-Plex-Schriften; keine Beispieldaten im Produkt.

VM-Ordner, Firmware, Größenvorgaben und Datenträgerhinweise sind aufklappbar;
PCI/USB und NAS-Ordner liegen unter Netzwerk/Erweiterungen. Vorhandene Optionen
und Einschränkungen beim Bearbeiten laufender VMs bleiben erhalten.
Die Heimnetz-Aktivierung benutzt denselben Netzwerk-Probelauf wie die
Einstellungen. Netzwerkbestätigung und Rückfall sind im Formular möglich;
keine Navigation oder Neuladung während der Umstellung, daher bleibt der Entwurf
erhalten. Neue VMs bevorzugen eine bereits bestätigte `br0`.

VM-Menüaktionen halten ihr Ziel vor dem Schließen fest. Eine reaktive
`@const` aus einem gerade geschlossenen Menü ist kein stabiles Aktionsargument.
`scripts/test-ui-vm-create.mjs` prüft echte Klicks, Rechtsklick, Tastatur und Touch,
Formularoptionen, DE/EN, Graphit/Hell, Desktop/Mobil sowie Bridge und Protokoll.
Prüfstand und Grenzen: [VM-Einrichtung](../VM-CREATE-VALIDATION.md).

### VM-Seite (0.20.1)

Der am 24. Oktober 2026 bestätigte kompakte Folgeentwurf baut auf `VmPage.html`
auf und ersetzt dessen Anordnung von Kopfzeile, Reitern und Konsolenwerkzeugen.
TitanOS-Glasflächen, Schrift, Farben und Funktionen bleiben erhalten. Ziehbare
Breite/Höhe, drei Anzeigemodi und dunkle Gastfläche gelten weiterhin; TitanOS
zeigt keinerlei Einblendungen innerhalb der VNC-Bildfläche, auch im Vollbild
und im eigenen Fenster. Verbindungszustände stehen außerhalb; Escape verlässt
das Vollbild. Der VNC-Fokus verschiebt die Seite nicht.

- Liste links: Suche, Filter Alle/Laufen/Aus mit Anzahl, je Zeile Zustand,
  Autostart als Symbol und „⋯“ (auch Rechtsklick) mit Start/Herunterfahren,
  Neustart, Bearbeiten, Snapshots, Ausschalten erzwingen bzw.
  Entfernen. Auf dem Smartphone eingeklappt hinter dem Namen der gewählten VM.
- Kopfzeile über beiden Spalten: Name, Zustand, Autostart, kompakter
  Aufgabenzugang und eine Hauptaktion (Starten bzw.
  Herunterfahren), Bearbeiten als Symbol, „⋯“ mit Neustart, Ausschalten
  erzwingen, serieller Konsole, XML und Entfernen (nur ausgeschaltet).
- Meldungen: eine Zeile, die ernsteste zuerst, der Rest hinter „+N weitere“.
  Die Rückmeldung „Aktion angenommen“ verschwindet nach fünf Sekunden.
- Gemeinsame Leiste darunter: Übersicht, Bildschirm und Snapshots (`?tab=`);
  im Bildschirm-Reiter schließen Senden und Skalieren direkt an. Die übrigen
  Konsolensymbole stehen rechts in derselben Leiste. VM-Liste und Reiterinhalt
  beginnen bündig unter dieser Leiste. Schmale Flächen umbrechen geordnet.
  Ohne Angabe öffnet eine
  laufende VM den Bildschirm, eine ausgeschaltete die Übersicht.
- Übersicht mit zwei Kachelspalten (schmal eine) und breiter Snapshot-Zeile:
  Auslastung (CPU-Verlauf der letzten zwei Minuten aus
  dem Abruf der Seite, nur im Browser), Hardware, Datenträger mit Belegung des
  Volumes und CD-Laufwerk (Einlegen, Wechseln, Auswerfen, auch bei laufender
  VM), Netzwerk mit Adresse (Gastagent oder Nachbartabelle des NAS), Geräte und
  Ordner (Zusatzfunktionen einer Karte in derselben Zeile, Hinweis auf den
  Monitor bei durchgereichter Grafikkarte), Snapshots.
- Bildschirm: „Senden ▾“ (Strg+Alt+Entf, Strg+Alt+⌫, Strg+Alt+F1/F2/F3/F7,
  Alt+Tab, Windows-Taste, Druck, Text einfügen), „Anzeige ▾“ mit den drei
  Modi, rechts Screenshot, serielle Konsole, eigenes Fenster, Vollbild als
  Symbole. Die Liste lässt sich nur hier ausblenden.
- Snapshots: Liste und Zeitplan direkt im Reiter, Dialoge für Erstellen,
  Planen, Wiederherstellen, Löschen und Abschließen; Wiederherstellen und
  Löschen als gefährliche Aktion links.

`scripts/test-ui-vms.mjs` prüft Reiter, Filter, Zeilen- und Kopfmenü,
Medienwechsel, Adressanzeige, Meldungen, Snapshots, Englisch und die gefaltete
Liste bei 390 Pixeln mit isolierten API-Testdaten; Screenshots in Graphit und
Hell unter `out/validation/vms/`. Aufruf wie beim 0.6.0-Skript oben.
`scripts/test-ui-vm-layout.mjs` prüft zusätzlich bündige Spalten, gemeinsame
Bedienleiste, einmaligen Aufgabenzugang, DE/EN, fünf Breiten und echtes noVNC
gegen einen simulierten RFB-Gast einschließlich Größenwechsel, Wiederverbindung,
Tastenkombinationen, Vollbild und separatem Fenster. Kein Test realer Gastgrafik.

### Benachrichtigungen, Home Assistant, Energie (0.14.0)

Vom Projektinhaber freigegeben, ohne eigene Entwurfsseite. Alle Teile nutzen die vorhandenen Kachel-, Formular- und Dialogmuster.

- **Mein Konto** bekommt die Kacheln „Meine Benachrichtigungen“ und „Glocke“ (nur für Besitzer und Administratoren). Der Aufbau folgt *Einstellungen › Benachrichtigungen*:
  - Wege untereinander, getrennt durch eine Linie, jeweils mit „Testnachricht“.
  - Meldungsarten als Häkchen (Speicher, Sicherungen, Updates, System).
  - Geheimnisse nur als „gespeichert“.
- Die **Glocke** zeigt „N ausgeblendet – Filter ändern“ als Link auf `/account#bell`.
- **Einstellungen › Home Assistant** hat die Kacheln in dieser Reihenfolge: Verbindung, Speicher für Home Assistant, Sensoren und Knöpfe (MQTT, mit „Handeln als“-Konto), Geräte für MCP freigeben:
  - Domänen als Raster mit einem Chip für die Stufe.
  - Geräte erst nach „Geräte laden“, mit Suche und höchstens 200 Zeilen.
  - Warnungen zu Knöpfen stehen direkt bei den Häkchen.
- **Energie:** Kachel „Zeitplan und Wake-on-LAN“ mit Wochentagsknöpfen (Montag zuerst), dem nächsten Termin und den Anschlüssen samt MAC-Adresse.
- **Sicherungen:**
  - Knopf „Prüfen“ mit einem kleinen Dialog für den Umfang.
  - Im Wiederherstellen-Dialog die Wahl „Ganzer Stand“ oder „Einzelne Dateien“; Letzteres öffnet einen Ordnerbrowser mit Häkchen.

### Shares und Docker-Abläufe (0.16.0)

Am 23. Oktober 2026 vom Projektinhaber beauftragter Ausbau aus dem interaktiven
Gesamtentwurf: kompakte Freigabentabelle mit Suche, Volume-/Protokollfilter und
Dichte; Details auf Klick, Verbindungshilfe separat, „In Dateien öffnen“.
Pfad und vollständige Kontorechte stehen in den Details. Fehlendes Volume,
Gastzugriff und fehlende SMB-Kontorechte bleiben sichtbar. Formularreiter erhalten
den Entwurf; bei Schließen mit Änderungen Speichern/Verwerfen/Weiterbearbeiten.

Docker behält seine bestehende Listen-/Kartenansicht, Stacks starten geschlossen.
Direkte Links übernehmen die vorhandenen WebUI-/Tailscale-/öffentlichen Adressen
des automatisch oder ausdrücklich zugeordneten Hauptdienstes. Auswahl je Konto
und Compose-Dienstname; kein stiller Ersatz bei fehlendem Dienst. Suche und
Statusfilter finden Dienste im Stack, Problemhinweis öffnet die betroffenen Dienste.
Fehlgeschlagene Aktualisierung zeigt den letzten Stand als veraltet.

Die Befehlspalette führt ausschließlich zu autorisierten Seiten/Objekten.
Der beauftragte Gesamtentwurf umfasst außerdem die implementierten Speicher-, Wartungs-, Backup-, Monitoring-, Klon- und Wiederanlaufseiten; genaue Grenzen in `../WORKFLOWS.md`.
Prüfung: `scripts/test-ui-workflows.mjs`, wie die übrigen Browserprüfungen mit
`PLAYWRIGHT_MODULE`, `BROWSER_EXECUTABLE`, `UI_URL` und `UI_OUTPUT`.

### Speicher, Wartung und Aufträge (0.16.0)

Der bestätigte Gesamtentwurf wird mit gemeinsamen PageHead-/Dialog-/Kachelmustern
und Theme-Variablen umgesetzt. Werkzeuge verlinkt Aufträge, Wartung, Monitoring und
Speicherübernahme. Aufträge bündelt aktuelle Arbeit, Fehler, nächste Termine und
Wiederanlauf. Seit 0.18.0 liegt dieser Handlungsbereich ausschließlich auf der Auftragsseite.

Speicherübernahme trennt Erkennung, lesende Vorschau und ausdrücklich bestätigte
Registrierung. Die Volume-Seite verlinkt Verbrauch, Erweiterung und Snapshot-Dateien.
Erweiterung zeigt Platte/Seriennummer, Rettungs-Volume und Mitgliederzahl vor der
Löschbestätigung. Offene Vorgänge bleiben sichtbar. Snapshot-Dateien werden in
neue Ordner kopiert; keine Wiederherstellung über laufende Daten.

Wartung zeigt gemeinsame Fenster/Zeitzone, Zeitpläne und letzte Ergebnisse.
Unterbrochene Vorgänge bieten Prüfung mit Kennung. Monitoring zeigt abschaltbare
Volume-Speicherung und getrennte Kurven; Lücken verbinden sich nicht zu erfundenen
Messwerten. Mobile Ansichten stapeln; Tabellen bleiben seitlich scrollbar.
VM-Klonen und Backup-Anwendungsauswahl ergänzen die jeweiligen vorhandenen Dialoge.
`scripts/test-ui-operations.mjs` prüft diese Abläufe auf Desktop/Mobil, Graphit/Hell.

### Abschluss und Korrekturen (0.17.0)

Auftragsfilter/Ergebnisse und eigene Upload-Einträge, gemeinsame Zustände der
Fachbereiche, Wartegründe/Abhängigkeiten/Backup-Vorrang, historische
Wiederanlaufdetails, CSV/JSON, Kapazitäts-/Restore-Vorschau und grafische
Speicheranteile ergänzen den Entwurf. Sicherungsabdeckung und die geführte
Anwendungs-Rückkopie liegen unter Einstellungen › Sicherungen. Globale
Wartungseinstellungen und Dialogentwürfe sind getrennt. Genaue Regeln und Grenzen:
[WORKFLOWS.md](../WORKFLOWS.md), Prüfstand [0.17.0](../releases/v0.17.0.md).
VM-Gastagent-/Freeze-Sicherung bleibt ein eigener späterer Ausbau.

### Persönliche Startseite, Speicher und Einstellungen (0.20.0)

[Bestätigter Folgeentwurf](PersonalDashboard.md): eine anpassbare Startseite mit
visuellem Editor, persönlicher Hintergrundgalerie (seit 0.30.0 wie alle
Gestaltung unter Einstellungen › Darstellung),
kompakte Speicherlisten und thematisch gruppierte Einstellungen. Die zehn lokalen
Motive und ihre Herkunft stehen in [DashboardBackgrounds.md](DashboardBackgrounds.md).
Er ersetzt die älteren Dashboard-/Editor- und Übersichtsregeln einschließlich
der Aufmerksamkeit-/Auftragsfläche auf der Startseite.

## Netzwerk- und Workflow-Ergänzungen (0.23.0)

Der Projektinhaber hat die gerenderten Vorschauen für mehrere NAS-Netze,
Docker-LAN-Zuweisung, unabhängige NFS-Modi, VM-Medien, API-Zugänge und
Plugin-Aufräumen bestätigt und deren Umsetzung beauftragt. Die bisherige
Docker-Übersicht und VM-Erstellung behalten ihre Grundaufteilung; ergänzt werden
nur die zugehörigen Funktionen und Fachdialoge. Verbindlich bleiben alle globalen
Darstellungswerte (Theme, Akzent, Flächenstil, Schrift, Dichte, Radien) und das
bestehende SVG-Symbolsystem. Keine Piktogramm-Schrift oder Ersatz-Emoji-Icons.

Im Dateimanager zeigt die Smartphone-Ansicht oben nur eine Root-/Pfadzeile und
eine Zeile Upload, Neu, Suche und Mehr. Suchfeld/Auswahl erscheinen bei Bedarf.
Die Desktopleisten bleiben erhalten. Die leere Aufgabenanzeige entfällt nur
hier mobil; Fehler/laufende Aufgaben bleiben sichtbar. Bei 390×844 Pixeln und
normaler Schrift beginnt die erste Datei höchstens 260 Pixel unter dem oberen
Rand. `scripts/test-ui-023.mjs` misst diesen Wert, prüft Graphit/Hell, Deutsch/
Englisch, Flat/Glas/Outline und 120 % Schrift sowie die neuen Fachdialoge anhand
isolierter Daten. Diese Aufnahmen ersetzen keine QEMU-Funktionsprüfung.

Router und DHCP bleiben extern. Netzwerkzwecke und Verwaltungsmarkierung haben
keine Firewall-Bedeutung; welcher Zone der Firewall ein Netz angehört, ist eine
eigene Zuordnung unter Einstellungen › Firewall › Netze. Tailscale bleibt eine eigene
Integration; öffentliche Cloudflare-App-Links sind keine Tunnelverwaltung.
Ablaufende Download-Links sind ausdrücklich nicht Teil des Umfangs.
