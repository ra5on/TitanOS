# Titan 3.0.0 — Freigabeprüfungen

Die Angaben beziehen sich auf den eigenen Titan-Arbeitsstand dieses Commits.
Ergebnisse älterer Versionen ersetzen keine Prüfung dieses Images.

| Prüfung | Stand |
| --- | --- |
| Python/API/Sicherheitsregressionen | 1.596 Tests erfolgreich; ein plattformabhängiger Test übersprungen |
| JavaScript-Controller und Syntax | 48 UI-Suiten erfolgreich; JavaScript- und Shell-Syntax geprüft |
| Debian-Anwendungspaket | Lokal gebaut und vollständige neue Laufzeitdateien sowie öffentlicher Schlüssel geprüft; keine Installation auf dem Build-Rechner |
| Mobile Ansicht | Dateimanager/Docker/Fotos bei 390 Pixeln; Fotos, VM-Details/Netzwerk, Speicher, Systemsteuerung/Updates bei 320 Pixeln ohne Seitenüberlauf geprüft. Desktop-Raster und mobile Widgetüberlagerung korrigiert. Frisch mobil geöffnetes VM-Fenster wächst bei 320 → 768 → 903 Pixeln auf die verfügbare Breite; Browserkonsole ohne Warnungen oder Fehler |
| Docker-Vorlagen | 374 Konfigurationen normalisiert und durch die gezielten Adaptertests geprüft; keine pauschale Laufzeitfreigabe aller Apps |
| Eigene Fotos | Backend-, Berechtigungs-, Metadaten-Sicherungs- und UI-Tests geprüft. Browser: Bibliothek, Einlesen, Vorschau, Favorit, Papierkorb und Wiederherstellung erfolgreich. Browser-Dateiauswahl unterbrochen; echte Upload-/Download-HTTP-Tests bestanden. Image-Laufzeitprüfung folgt |
| UEFI-Boot, HTTPS, SMB, Docker, Speichergrenze | GitHub-Imageprüfung ausstehend |
| Signiertes Update, Rollback, fehlgeschlagener Boot | GitHub-A/B-Prüfung ausstehend |
| Installiertes Gastbetriebssystem, VNC-Eingabe und Wiederverbindung | Praxistest ausstehend; reine RFB-Verbindung genügt nicht |
| 8-GB-Lasttest, Installation parallel zum Dateimanager | Verpflichtender GitHub-Laufzeittest ergänzt; echte Messungen noch ausstehend |
| Vollständige Datenwiederherstellung auf frischem Image | Ausstehend |
| Speicher voll/offline/ersetzt, Vergrößerung | Unit- und Laufzeitprüfungen zusammen bewerten |
| Mehrtägiger Dauerlauf und reale Hardware | Ausstehend |

Ein Release-Entwurf kann zum Testen bereitgestellt werden. Ausstehende Prüfungen
dürfen nicht durch ein Stable-Etikett ersetzt werden. Für die erste Systemfamilie
prüft CI den echten A/B-Wechsel aus einem privaten Boot-Overlay mit einer
älteren signierten Identität. Dieses Bootstrap-Ergebnis belegt keine Migration
von einer fremden oder früheren Installation. Spätere Updates verwenden einen
signierten, veröffentlichten Installationsstand als Ausgangspunkt.

Rollback stellt das Betriebssystem wieder her. Persistente Benutzerdaten und
Container-Datenbanken werden dabei nicht rückwärts migriert. Die Kompatibilität
der gespeicherten Konfiguration und geeignete App-Sicherungen gehören deshalb
zu jeder Freigabeprüfung.
