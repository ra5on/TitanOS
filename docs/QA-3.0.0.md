# Titan 3.0.0 — Freigabeprüfungen

Die Angaben beziehen sich auf den eigenen Titan-Arbeitsstand dieses Commits.
Ergebnisse älterer Versionen ersetzen keine Prüfung dieses Images.

| Prüfung | Stand |
| --- | --- |
| Python/API/Sicherheitsregressionen | 1.728 Tests im letzten Gesamtlauf erfolgreich; ein plattformabhängiger Test übersprungen. Einschließlich Identitätsbereinigung, schreibgeschütztem Systemslot, unterbrochener Persistenz-Erweiterung, verpflichtendem Mount-Nachweis, gemeinsamem Autostart-RAM-Budget, USB-Identitätsprüfung, echter Archiv-Wiederherstellung mit Zeitstempeln und abgesicherter Release-Versionsvergabe |
| JavaScript-Controller und Syntax | 48 UI-Suiten erfolgreich; JavaScript- und Shell-Syntax geprüft |
| Debian-Anwendungspaket | Lokal gebaut und vollständige neue Laufzeitdateien sowie öffentlicher Schlüssel geprüft; keine Installation auf dem Build-Rechner |
| Mobile Ansicht | Dateimanager/Docker/Fotos bei 390 Pixeln; Fotos, VM-Details/Netzwerk, Speicher, Systemsteuerung/Updates bei 320 Pixeln ohne Seitenüberlauf geprüft. Desktop-Raster und mobile Widgetüberlagerung korrigiert. Frisch mobil geöffnetes VM-Fenster wächst bei 320 → 768 → 903 Pixeln auf die verfügbare Breite; Browserkonsole ohne Warnungen oder Fehler |
| Docker-Vorlagen | 374 Konfigurationen normalisiert, durch die Adaptertests und mit Debian Compose 2.26.1-4 config --quiet geprüft; keine pauschale Laufzeitfreigabe aller Apps |
| Eigene Fotos | Backend-, Berechtigungs-, Metadaten-Sicherungs- und UI-Tests geprüft. Browser: Bibliothek, Einlesen, Vorschau, Favorit, Papierkorb und Wiederherstellung erfolgreich. Browser-Dateiauswahl unterbrochen; echte Upload-/Download-HTTP-Tests bestanden. Im ersten korrigierten Image auch die reale Foto-Laufzeitprüfung erfolgreich |
| UEFI-Boot, HTTPS, SMB, Docker, Speichergrenze | GitHub-Lauf 37378699615 erfolgreich: korrigierte Maschinenidentität, echte Ersteinrichtung, HTTPS, Mehrbenutzer-SMB, Docker-Lebenszyklus, eigene Netzwerke, Stacks und VM-Domain-Lebenszyklus. Der aktuelle Kandidat mit RO- und RAM-Erweiterungen wird erneut vollständig geprüft |
| Schreibgeschütztes Betriebssystem | Im GitHub-Lauf 37381938424 echter PID-1-Nachweis für RO-Systemslot, 17 DATA-Bindziele, slotlokale Paketdatenbanken und begrenztes /tmp erfolgreich; außerdem alle Laufzeit-, Update-, Rollback- und Fallback-Prüfungen erfolgreich. Veröffentlichung dieses Laufs wegen bereits belegter Versionsnummer sicher abgebrochen; Draft-Sichtbarkeit und frühe Kollisionsprüfung korrigiert. Aktueller Kandidat wird erneut geprüft |
| Signiertes Update, Rollback, fehlgeschlagener Boot | Im GitHub-Lauf 37378699615 echter A→B-Wechsel, B→A-Rollback und automatischer Fallback vom fehlerhaften B-Slot erfolgreich; Nutzerdaten und Rechte erhalten. Neuer Kandidat erfordert denselben Nachweis erneut |
| Installiertes Gastbetriebssystem, VNC-Eingabe und Wiederverbindung | Praxistest ausstehend; reine RFB-Verbindung genügt nicht |
| 8-GB-Lasttest, Installation parallel zum Dateimanager | Im GitHub-Lauf 37378699615 echte App-Installation bei 8 GiB mit gleichzeitig erreichbarem Dateimanager erfolgreich; keine Freigabe für jede beliebige App-Kombination |
| Autostart nach RAM-Verkleinerung | Laufende und automatisch startende Docker-/VM-Grenzen gemeinsam mit NAS-Reserve geprüft; unbekannte Metadaten blockieren. Separater Offline-Guard vor Docker/libvirt hält Weboberfläche und Dateimanager erreichbar. Unit-Prüfungen erfolgreich; verpflichtender realer 8→3→8-GiB-Kaltstart mit Wiederherstellung im neuen Image ausstehend |
| USB-Geräte für Container | Geeignete, eindeutig identifizierbare Geräte mit frischer Identitätsprüfung vor Start und Autostart; rohe Speicher-, Hub-, Netzwerk- und unbekannte USB-Geräte nicht auswählbar. Regressionstests erfolgreich; reale USB-Durchreichung auf Zielhardware ausstehend |
| Vollständige Datenwiederherstellung auf frischem Image | Nicht vollständig implementiert: Übernahme fremder Sicherungs-Namensräume, Rekonstruktion der Hostkonten/Speicher/Appinstallationen und integrierte App-Datenbank-Wiederherstellung fehlen |
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

Die vorhandene externe Sicherung archiviert ausgewählte SMB-Freigaben mit
Prüfsumme und optional die NAS-Konfiguration. Dateien werden in einen neuen
Ordner einer bestehenden Freigabe zurückgespielt; deren Zugriffsrechte gelten
weiter. Datei- und Ordner-Änderungszeiten werden aus dem Archiv übernommen.
Beliebige NAS-Ordner, Docker-Volumes und beschreibbare Container-Schichten
werden dadurch nicht automatisch gesichert. Foto-Metadaten wie Bibliotheken,
Alben, Favoriten und Papierkorbzuordnungen gehören zur Konfiguration;
Foto-Originale müssen zusätzlich über die entsprechenden Freigaben gesichert
werden. Vorschaubilder werden neu erzeugt.

Ein frisches Image erzeugt einen neuen Sicherungs-Namensraum. Die bestehende
Oberfläche bietet noch keine Übernahme oder Suche nach den Archiven eines
früheren Namensraums; diese bleiben auf dem Sicherungsmedium vorhanden.
Namensraum und Sicherungsziel-Einstellungen werden nicht mit der
NAS-Konfiguration exportiert. Die Konfigurations-Wiederherstellung ist für
denselben Host gebaut: verwaltete Linux-Konten mit passenden UIDs,
Freigabeordner, eingebundene Speicher und übereinstimmende Appinstallationen
müssen bereits vorhanden sein. Sie erstellt diese Voraussetzungen nicht auf
einem leeren Image. Foto-Bibliotheken verlangen außerdem ihre ursprüngliche
Speicherkennung; eine Zuordnung zu einem Ersatzvolume ist nicht implementiert.

App-Sicherungen stoppen die verwalteten Container und archivieren deren lokale
Konfigurationsordner einschließlich dort gespeicherter Datenbankdateien auf
dem DATA-Speicher. Ein integrierter Rückspielvorgang für diese App-Archive fehlt.
Sie ersetzen keine unabhängige externe Sicherung und decken zusätzliche
Datenpfade oder native Docker-Volumes nicht automatisch ab. Eine vollständige
Wiederherstellung von beispielsweise Nextcloud oder Immich auf einem frischen
Image benötigt daher derzeit weitere manuelle, zur App passende Schritte.

Die Regressionen prüfen echte TAR-/SQLite-Sicherungen, Dateiinhalte,
Änderungszeiten und sichere Ablehnung ungültiger beziehungsweise ausgetauschter
Pfade. Hostkonten, Dienste und App-Datenbanken werden dabei teilweise durch
Test-Adapter ersetzt. Der A/B-Lauf belegt die Erhaltung vorhandener DATA-Daten
beim Systemwechsel; er belegt keine vollständige Wiederherstellung aus
externen Sicherungen nach einer Neuinstallation. Diese vollständige Funktion
fehlt im aktuellen Stand und ist nicht bloß noch ungetestet.
