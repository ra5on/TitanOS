# Persönliche Startseite, Speicher und Einstellungen (0.18.0)

Dieser bestätigte Folgeentwurf ersetzt die älteren Dashboard-, Speicherübersichts-
und Einstellungsübersichtsregeln. Es gibt **eine** Startseite pro Konto, keine
Ansichten, Profile oder wechselnden Dashboards. Shares, Dateien und Nutzer
behalten ihre bestehende Gestaltung. Keine LUKS-Verschlüsselung.

## Dashboard

Ruhige, zusammengehörige Kacheln: System, CPU, RAM und Netzwerk oben; erkannte
Grafik-/NPU-Geräte, Volumes, Docker, VMs, Temperaturen und Shares darunter.
Vorhandene gespeicherte Anordnungen bleiben erhalten. Neue Standardanordnungen
berücksichtigen die tatsächlich erkannten Beschleuniger. Keine Aufmerksamkeit-
oder Nächste-Schritte-Kachel und kein Aufgabenknopf auf der Startseite.
Aufträge und deren Steuerung bleiben unter Werkzeuge erreichbar.

Ein Bearbeiten-Modus mit Griffen, Rastergrößen und seitlichem Inspector. Dessen
Reiter **Inhalt**, **Darstellung**, **Größe** bearbeiten die markierte Kachel.
Überschrift, Hauptwert und Beschriftungen besitzen eigene Schriftgröße, Stärke,
Schriftart, Farbe und Ausrichtung. Hintergrund, Akzent, Rahmen, Innenabstand,
Elementabstand, Ecken, Deckkraft und Schatten lassen sich je Kachel einstellen.
Inhaltsabhängige Optionen: Details, Verlauf, CPU-Temperatur; Messwert als Zahl,
Balken, Ring oder Verlauf; Geräte-/Schnittstellenwahl; Liste/Raster und Auswahl
angezeigter Volumes, Laufwerke, Shares, VMs oder Docker-Gruppen. Reihenfolge der
Messwertbereiche, Einträgezahl und laufende Dienste lassen sich beeinflussen.

Duplizierte Instanzen behalten eine eigene Kennung und eigene Optionen. Sie
können andere Schnittstellen/Geräte beobachten. Namen sind Anzeigealiase, keine
Umbenennung von NAS-Objekten. Fehlende gewählte Quellen bleiben fehlend, ohne
still auf andere Geräte zu wechseln. Docker-Stacks sind zunächst geschlossen;
WebUI-, Tailscale- und öffentliche Links stammen vom gewählten Hauptdienst.

Rückgängig/Wiederholen, Stilübertragung und Rücksetzen einzelner Kacheln.
Pfeiltasten am Griff verschieben, Umschalt + Pfeiltasten skaliert. Vorschau bis
Speichern; Abbrechen und Navigationsschutz erhalten den gespeicherten Zustand.
Alle Werte werden geprüft und pro Konto atomar über bestehende Präferenzen
persistiert. Mobil wird gestapelt; Speichern bewahrt das Desktop-Raster.
Überbreite Inhalte umbrechen bzw. scrollen innerhalb ihrer Kachel.

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

`scripts/test-ui-personal-dashboard.mjs` prüft echte Browserbedienung gegen
isolierte API-Daten; Ergebnisse und Hardwaregrenzen stehen in den Release-Notizen.
Beispieldaten der Entwürfe und Tests gehören niemals ins Produkt.
