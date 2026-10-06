# Titan 3.0.0 — Freigabeprüfungen

Die Angaben beziehen sich auf den eigenen Titan-Arbeitsstand dieses Commits.
Ergebnisse älterer Versionen ersetzen keine Prüfung dieses Images.

| Prüfung | Stand |
| --- | --- |
| Python/API/Sicherheitsregressionen | 1.823 Tests im letzten Gesamtlauf erfolgreich; ein plattformabhängiger Test übersprungen. Einschließlich Identitätsbereinigung, schreibgeschütztem Systemslot, unterbrochener Persistenz-Erweiterung, verpflichtendem Mount-Nachweis, gemeinsamem Autostart-RAM-Budget, USB-Identitätsprüfung, echter Archiv-Wiederherstellung mit Zeitstempeln, abgesicherter Release-Versionsvergabe und frischer Speicher-/USB-/Netzwerk-/RAM-Prüfung vor dem Wiederstart nach App-Sicherungen sowie echter VNC-Readiness, begrenzter Konsolen-Gesamtfrist und dauerhaftem Main-Prozess-RAM-Nachweis mit aktuellen Bootmarkern |
| JavaScript-Controller und Syntax | 48 UI-Suiten erfolgreich; JavaScript- und Shell-Syntax geprüft |
| Debian-Anwendungspaket | Lokal gebaut und vollständige neue Laufzeitdateien sowie öffentlicher Schlüssel geprüft; keine Installation auf dem Build-Rechner |
| Mobile Ansicht | Dateimanager/Docker/Fotos bei 390 Pixeln; Fotos, VM-Details/Netzwerk, Speicher, Systemsteuerung/Updates bei 320 Pixeln ohne Seitenüberlauf geprüft. Desktop-Raster und mobile Widgetüberlagerung korrigiert. Frisch mobil geöffnetes VM-Fenster wächst bei 320 → 768 → 903 Pixeln auf die verfügbare Breite; Browserkonsole ohne Warnungen oder Fehler |
| Docker-Vorlagen | 374 Konfigurationen normalisiert, durch die Adaptertests und mit Debian Compose 2.26.1-4 config --quiet geprüft; keine pauschale Laufzeitfreigabe aller Apps |
| Eigene Fotos | Backend-, Berechtigungs-, Metadaten-Sicherungs- und UI-Tests geprüft. Browser: Bibliothek, Einlesen, Vorschau, Favorit, Papierkorb und Wiederherstellung erfolgreich. Browser-Dateiauswahl unterbrochen; echte Upload-/Download-HTTP-Tests bestanden. Im ersten korrigierten Image auch die reale Foto-Laufzeitprüfung erfolgreich |
| UEFI-Boot, HTTPS, SMB, Docker, Speichergrenze | GitHub-Lauf 37395985165 erfolgreich für echte Ersteinrichtung, HTTPS, Mehrbenutzer-SMB, Docker-Lebenszyklus, eigene Netzwerke, Stacks, native Containerverwaltung, Fotos und VM-Domain-Lebenszyklus einschließlich vollständiger UEFI→BIOS→UEFI-RFB-Konsolenverbindungen. Im Lauf 37400720990 sind diese Laufzeitprüfungen erneut erfolgreich; dessen RAM-Kaltstart fand noch einen Fehler im Runtime-Startpfad. Der nächste Kandidat muss alle Prüfungen erneut bestehen |
| Schreibgeschütztes Betriebssystem | Im GitHub-Lauf 37381938424 echter PID-1-Nachweis für RO-Systemslot, 17 DATA-Bindziele, slotlokale Paketdatenbanken und begrenztes /tmp erfolgreich; außerdem alle Laufzeit-, Update-, Rollback- und Fallback-Prüfungen erfolgreich. Veröffentlichung dieses Laufs wegen bereits belegter Versionsnummer sicher abgebrochen; Draft-Sichtbarkeit und frühe Kollisionsprüfung korrigiert. Aktueller Kandidat wird erneut geprüft |
| Signiertes Update, Rollback, fehlgeschlagener Boot | Im GitHub-Lauf 37378699615 echter A→B-Wechsel, B→A-Rollback und automatischer Fallback vom fehlerhaften B-Slot erfolgreich; Nutzerdaten und Rechte erhalten. Neuer Kandidat erfordert denselben Nachweis erneut |
| VM-Konsole und installiertes Gastbetriebssystem | Echte fragmentierte RFB-Readiness, frische Zielprüfung, begrenzte Sperr-/virsh-/Proxy-Laufzeit und Browser-Wiederverbindung durch Regressionen geprüft. Der vollständige UEFI→BIOS→UEFI-RFB-Roundtrip ist im GitHub-Lauf 37395985165 erfolgreich. Gastbetriebssystem, Tastatureingabe und reale Browser-Wiederverbindung benötigen weiter einen Praxistest |
| 8-GB-Lasttest, Installation parallel zum Dateimanager | Im GitHub-Lauf 37378699615 echte App-Installation bei 8 GiB mit gleichzeitig erreichbarem Dateimanager erfolgreich; keine Freigabe für jede beliebige App-Kombination |
| Autostart nach RAM-Verkleinerung | Der Main-Prozess-Guard verhindert automatische Wiederholungen mit Exit 78. Ein rootgeschützter Marker muss zum aktuellen Boot und exakt zur Main-Laufzeit gehören; eine frische vollständige RAM-Neuberechnung bestätigt zusätzlich den Mangel. Nur fest zugeordnete primäre Sockets mit belegtem Guard-Startlimit erhalten die eingeschränkte Ausnahme; alle Kernservices und VM-Hilfssockets bleiben zwingend. Die originalen Debian-13-Dienstdefinitionen wurden anhand der Paketdateien verifiziert: Docker bewahrt den Platzhalter $DOCKER_OPTS, libvirt $LIBVIRTD_ARGS. Lauf 37410714111 hat die zuvor fehlende Docker-Variante vor dem Boot sicher abgelehnt. Regressionen prüfen nun diese tatsächliche Paketdefinition sowie die Ablehnung fremder Variablen und Zusatzoptionen. Der vollständige echte 8→3→8-GiB-Durchlauf steht für diesen Kandidaten noch aus |
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
