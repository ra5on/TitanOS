# Persönliche Startseite, Speicher und Einstellungen (0.19.0)

Dieser bestätigte Folgeentwurf ersetzt die älteren Dashboard-, Speicherübersichts-
und Einstellungsübersichtsregeln. Es gibt **eine** Startseite pro Konto, keine
Ansichten, Profile oder wechselnden Dashboards. Shares, Dateien und Nutzer
behalten ihre bestehende Gestaltung. Keine LUKS-Verschlüsselung.

## Dashboard (bestätigter Folgeentwurf 0.19.0)

### Aufbau

Die Referenz des Besitzers bestimmt Informationsdichte und Hierarchie: ruhiger Hintergrund, schmale Abschnittsköpfe, kompakte Zeilen und Anwendungsraster. Keine dekorativen großen Messwerte, leeren Karten oder Hintergrundlichter. TitanOS behält Marke, Navigation, IBM Plex und eigene Symbole. Eine persönliche Startseite, keine Ansichten/Profile. Keine Aufmerksamkeit-/Auftragsfläche.

### Reihenfolge und Spalten

- Globale gewünschte Spaltenzahl (1–4), im Bearbeiten-Modus einstellbar.
- Eine persistierte Reihenfolge aller Kacheln. Befüllung zeilenweise von links nach rechts, danach nach unten. CSS-Spalten mit vertikalem Lesefluss oder Packen in die kürzeste Spalte sind dafür ungeeignet.
- Bei zu geringer Breite automatisch weniger Spalten; die gewünschte Anzahl bleibt gespeichert. Kein horizontales Abschneiden. Beispiel A B / C D wird schmal zu A / B / C / D.
- Einstellbare Mindestbreite und Mindesthöhe; Inhaltshöhe automatisch. Optionale bewusste feste Höhe nur mit sichtbarem Überlaufzugang. Breitere Kacheln können Spalten überspannen, werden mobil begrenzt. Keine absolute Desktopposition als einziges Ordnungsmodell.
- Vorgeschlagene Reihenfolge: Systeminformationen, Docker, CPU, VMs, RAM, Volumes, Beschleuniger, Shares, Netzwerk, Temperaturen. Jede Kachel verschiebbar; keine festen linken/rechten Kategorien.

### Inhalte und Klickmenüs

- Systeminformationen: auswählbare Felder, Reihenfolge, Beschriftungen/Werte separat; Hostname, Betriebssystem, Laufzeit, Kernel, CPU, Mainboard, BIOS, RAM, Geräte/Treiber, Netzwerkadressen, Slot und Updates. Vorhandene API-Felder wiederverwenden; zusätzliche Inventardaten nur ereignisbezogen/startseitig erfassen, kein zyklisches Sticklesen. Fehlende Werte als unbekannt darstellen.
- Docker: Symbol-/Namensraster mit verständlichem Status, kompakte Liste optional. Klick auf Symbol oder Namen öffnet ein Menü mit passenden Links, Konsole/Logs, Details, Starten bzw. Stoppen/Neustart, Updateprüfung, Bearbeiten und Anheften. Nur tatsächlich vorhandene WebUI/Tailscale/öffentliche Adressen anbieten. Stacks geschlossen. Ein separater Pfeil oder der Menüpunkt Container anzeigen klappt die Container auf. Einzelne Container/Anwendungen als eigene Kachel weiterhin möglich. Rollen und bestehende Bestätigungsregeln erhalten.
- VMs: gleich breite, kompakte Rasterzellen mit festem gleichmäßigem Abstand statt über die gesamte Fläche verteilten Einträgen. Entwurf: 200 px pro Eintrag, 16 px horizontaler Abstand, 12 px Zeilenabstand; bei weniger Platz umbrechen. Im Dashboard nur Symbol, Name und Status; lange Namen bleiben im eigenen Bereich. CPU/RAM entfallen dort. Der Button „Alle VM-Details“ öffnet die ausführlichen Informationen aller VMs, einschließlich Prozessor, RAM, Auslastung, Gast, Platten, Netzwerk und Start. Klick auf einzelne VM öffnet weiterhin Konsole, Details, zustandsabhängige Betriebsaktionen, unterstützte Snapshots, Bearbeiten und Anheften. Einzelne VM weiterhin separat platzierbar.
- CPU/RAM/GPU/iGPU/NPU: standardmäßig beschriftete Balken mit bündiger Zahl plus Einheit, keine riesigen Ringe. Ring als wählbare Alternative; Zahl und Prozentzeichen bleiben zusammen und mittig. Schrift muss bei 0/99/100 %, großer Benutzerschrift und schmaler Kachel passen. Unbekannte NPU-Werte sind kein 0 %.
- Volumes, Shares, Netzwerk, Temperaturen: kompakte Tabellen/Zeilen mit optionalen Zusatzspalten und direktem Link zu Details.
- Klickbare Volume-, Share- und Geräteeinträge bekommen eigene fachlich passende Menüs, keine identische globale Aktionsliste. Außenklick/Escape schließen, Tastatur und Touch funktionieren. Im Bearbeiten-Modus gilt die Auswahl zur Gestaltung; Betriebsaktionen dürfen nicht versehentlich beim Verschieben auslösen.

### Editor

Ein Bearbeiten-Modus: Auswahl direkt an der Kachel; Inspector Inhalt / Darstellung / Größe. Systemfelder per Häkchen und Reihenfolge. Darstellung mit unabhängiger Typografie für Titel/Werte/Beschriftungen, Listen-/Rastermodus, Symbole, Flächen, Rahmen und Abstand. Verschieben, Größenänderung, Duplizieren, Ausblenden, Wiederherstellen, Undo/Redo sowie Vorschau/Speichern/Abbrechen erhalten. Änderungen pro Konto, Tastaturbedienung und DE/EN.


Bestehende Layouts werden einmal nach Zeile und Spalte in die Lesereihenfolge übernommen.
Zusätzliche Felder der Layoutversion 5 sind optional; 12-Spalten-Koordinaten bleiben für
den App-Rückweg erhalten. Persönliche Inhalte und Gestaltungswerte bleiben bestehen;
„Standard wiederherstellen“ lädt die kompakte neue Standardgestaltung.

## Speicher

Reiter **Volumes** und **Laufwerke**, kompakte Zeilen mit Suche, Status-/Typ-/
Zuordnungsfiltern und Sortierung. Auswahl öffnet Details daneben, mobil darunter.
Die Volume-Details verwenden die bestehenden Aktionen und Dialoge einschließlich
Snapshots, Prüfen, Erweitern und Löschen. Die Hauptaktion ist Volume anlegen;
Übernahme, Wartung, Erkennung und Standby liegen unter Weitere Optionen.

Live-Aktualisierung behält Auswahl und Filter. Rückkehr innerhalb einer Sitzung
stellt die pro Konto gehaltene Navigation wieder her; Anker führen gezielt zu
Volume oder Laufwerk. Keine Änderung an Speicher- oder Bestätigungssemantik.

## Einstellungen

Kompakte Symbol-/Titelkacheln in **System**, **Netzwerk und Dienste**,
**Speicher und Sicherungen**, **Integrationen**, **Persönlich**. Alphabetische
Sortierung nach übersetztem Titel innerhalb der Gruppe. Gruppenübergreifende
Suche behält Abschnittsziele, Rollen und bestehende Weiterleitungen.

## Prüfungen

`scripts/test-ui-dashboard-flow.mjs` und `scripts/test-ui-personal-dashboard.mjs` prüft echte Browserbedienung gegen
isolierte API-Daten; Ergebnisse und Hardwaregrenzen stehen in den Release-Notizen.
Beispieldaten der Entwürfe und Tests gehören niemals ins Produkt.
