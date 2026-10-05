# Titan 3.0.0 — Freigabeprüfungen

Die Angaben beziehen sich auf den eigenen Titan-Arbeitsstand dieses Commits.
Ergebnisse älterer Versionen ersetzen keine Prüfung dieses Images.

| Prüfung | Stand |
| --- | --- |
| Python/API/Sicherheitsregressionen | 1.630 Tests im letzten Gesamtlauf erfolgreich; ein plattformabhängiger Test übersprungen. Einschließlich Identitätsbereinigung, schreibgeschütztem Systemslot, unterbrochener Persistenz-Erweiterung und verpflichtendem Mount-Nachweis |
| JavaScript-Controller und Syntax | 48 UI-Suiten erfolgreich; JavaScript- und Shell-Syntax geprüft |
| Debian-Anwendungspaket | Lokal gebaut und vollständige neue Laufzeitdateien sowie öffentlicher Schlüssel geprüft; keine Installation auf dem Build-Rechner |
| Mobile Ansicht | Dateimanager/Docker/Fotos bei 390 Pixeln; Fotos, VM-Details/Netzwerk, Speicher, Systemsteuerung/Updates bei 320 Pixeln ohne Seitenüberlauf geprüft. Desktop-Raster und mobile Widgetüberlagerung korrigiert. Frisch mobil geöffnetes VM-Fenster wächst bei 320 → 768 → 903 Pixeln auf die verfügbare Breite; Browserkonsole ohne Warnungen oder Fehler |
| Docker-Vorlagen | 374 Konfigurationen normalisiert, durch die Adaptertests und mit Debian Compose 2.26.1-4 config --quiet geprüft; keine pauschale Laufzeitfreigabe aller Apps |
| Eigene Fotos | Backend-, Berechtigungs-, Metadaten-Sicherungs- und UI-Tests geprüft. Browser: Bibliothek, Einlesen, Vorschau, Favorit, Papierkorb und Wiederherstellung erfolgreich. Browser-Dateiauswahl unterbrochen; echte Upload-/Download-HTTP-Tests bestanden. Image-Laufzeitprüfung folgt |
| UEFI-Boot, HTTPS, SMB, Docker, Speichergrenze | Image und RAUC-Bundle erzeugt und signiert. Vorabprüfung entdeckte eine erneut gesetzte Maschinen-ID; abschließende Offlinebereinigung ergänzt. Reale Prüfung der korrigierten Version läuft, neue RO-Systemversion anschließend |
| Schreibgeschütztes Betriebssystem | GRUB/fstab und Initramfs auf RO-Systemslot umgestellt; 17 gezielte DATA-Bindziele und begrenztes /tmp. Paketdatenbanken bleiben slotlokal. Unit- und Dateikopiertests erfolgreich; echte PID-1-Mountprüfung blockiert die Veröffentlichung bei fehlendem Nachweis |
| Signiertes Update, Rollback, fehlgeschlagener Boot | GitHub-A/B-Prüfung ausstehend |
| Installiertes Gastbetriebssystem, VNC-Eingabe und Wiederverbindung | Praxistest ausstehend; reine RFB-Verbindung genügt nicht |
| 8-GB-Lasttest, Installation parallel zum Dateimanager | Verpflichtender GitHub-Laufzeittest ergänzt; echte Messungen noch ausstehend |
| Vollständige Datenwiederherstellung auf frischem Image | Ausstehend |
| Speicher voll/offline/ersetzt, Vergrößerung | Unit-Prüfungen verhindern Containerstarts bei Volumeaustausch während create/commit; Laufzeitprüfungen zusammen bewerten |
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
