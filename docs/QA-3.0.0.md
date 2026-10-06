# Titan — Systemimage 3.0.1: Freigabeprüfungen

Systemimage **3.0.1**, Anwendung **3.0.0**. Der automatische Image-Lauf
[37418063214](https://github.com/ra5on/TitanOS/actions/runs/37418063214)
ist am 6. Oktober 2026 erfolgreich abgeschlossen. Das Image gehört zum
[Quellcommit 2cfd33a](https://github.com/ra5on/TitanOS/commit/2cfd33a3d9c7001ee4c6ef14f49d3fde22f9e3f8).
Das [Release 3.0.1](https://github.com/ra5on/TitanOS/releases/tag/titan-3.0.1)
wurde am 6. Oktober 2026 auf ausdrücklichen Wunsch öffentlich für den
Stable-Kanal freigegeben. Weitere Praxistests und der Dauerlauf sind offen.

| Prüfung | Ergebnis |
| --- | --- |
| Python/API/Sicherheitsregressionen | 1.826 lokale Tests erfolgreich; ein plattformabhängiger Skip. GitHub bestätigt 1.826 Tests mit 21 Skips wegen dort nicht verfügbarer Laufzeitumgebungen; verpflichtende echte Image-Tests folgen separat |
| JavaScript und Oberfläche | 48 UI-Suiten und Syntaxprüfungen erfolgreich. Nicht verfügbarer Office-Editor wird nicht angeboten; DOCX-/XLSX-Downloads und Textbearbeitung geprüft |
| Debian-Anwendungspaket | Lokal gebaut; sieben relevante Dateien einschließlich Guard, Runtime, Bootprüfung, NOTICE und geänderter Oberfläche bytegleich und rootgeschützt im Paket nachgewiesen |
| Mobile Ansicht | Dateimanager/Docker/Fotos bei 390 Pixeln; Fotos, VM-Details/Netzwerk, Speicher und Systemsteuerung/Updates bei 320 Pixeln ohne Seitenüberlauf geprüft. Mobil geöffnetes VM-Fenster wächst beim Wechsel auf einen größeren Bildschirm; Browserkonsole ohne Warnungen oder Fehler |
| Docker-Vorlagen | 374 LinuxServer.io-/Big-Bear-Konfigurationen normalisiert und mit Debian Compose 2.26.1-4 geprüft. Die API listet tatsächlich 374 Vorlagen; dies ist keine Laufzeitfreigabe jeder einzelnen App |
| Echte NAS-Laufzeit | Ersteinrichtung, HTTPS, Anmeldeschutz, Mehrbenutzer-SMB, Container Start/Stop/Entfernen, eigene Netze und statische IPs, mehrteilige Projekte, native Containerverwaltung, Protokolle und Kommandokonsole erfolgreich |
| Eigene Fotos | Echte PNG-Datei hochgeladen, Hintergrundindex, private Vorschau/Original, Authentifizierung, Favoriten/Album, Papierkorb und Wiederherstellung mit unverändertem Inhalt erfolgreich |
| Schreibgeschütztes Betriebssystem | Alle zehn echten Nachweise erfolgreich: PID-1-Mounts, RO-Systemslot ohne schreibbaren Alias, slotlokale Paketdatenbanken, 17 persistente DATA-Bindziele sowie begrenztes beschreibbares /tmp |
| Signiertes Update und Rückkehr | Echter A→B-Wechsel, B→A-Rollback und automatischer Fallback vom fehlerhaften B-Slot erfolgreich; Konten, ACLs und Daten erhalten. Die Bootstrap-Baseline ist gesondert unten erläutert |
| Autostart nach RAM-Verkleinerung | Alle elf Nachweise des echten 8→3→8-GiB-Kaltstarts erfolgreich: Docker und libvirt bei zu kleinem RAM gesperrt, Verwaltung und Dateimanager erreichbar, Mangel frisch bestätigt, echte RAM-Rückkehr und Daemon-Autostarts nachgewiesen, Testcontainer/-image entfernt. Exakte Main-Prozessmarker und feste Socket-Triggers/Listener sind zwingend |
| Installation parallel zum Dateimanager | Tatsächliche App-Installation bei 8 GiB: sieben aktive Installationsproben, Dateimanager und Status erreichbar; längste gemessene Anfrage 261 ms. Keine Freigabe für jede beliebige App-Kombination |
| VM-Lebenszyklus und Konsole | Start, Stop, Löschen, Wiederverwendung des gelöschten Namens, BIOS/UEFI-Wechsel, Bridgekonfiguration, direktes Image-Klonen mit unveränderter Quelle und authentifizierte RFB-Konsole erfolgreich. Ein installiertes Gastbetriebssystem, Tastatureingabe und reales Browser-Canvas benötigen noch einen Praxistest |
| Speicher und Vergrößerung | Echte DATA-Vergrößerung auf einer erweiterten virtuellen Systemplatte erfolgreich. ext4-/XFS-Werkzeuge und zum Kernel passendes geladenes ZFS-Modul nachgewiesen. Unit-Prüfungen verhindern Starts bei Volumeaustausch; reale Pool-/RAID-Recovery und weitere Offline-Szenarien bleiben offen |
| USB/GPU | Auswahl/Identität/Speichergrenzen vor Start und Autostart durch Regressionen geprüft. Reale Durchreichung und Beschleunigung auf geeigneter Hardware ausstehend |
| Vollständige Neu-NAS-Wiederherstellung | Nicht vollständig implementiert: fremde Sicherungs-Namensräume, Rekonstruktion von Hostkonten/Speicher/Appinstallationen und integriertes Rückspielen von App-Datenbanken fehlen |
| Signierter Download | Manifest und Prüfsummen signiert; Signaturprüfung im CI erfolgreich. Alle 13 Assets vollständig hochgeladen; Hashes der separat geladenen Laufzeit-/A/B-Berichte stimmen mit den Release-Dateien überein. Beide öffentlichen Vertrauensschlüssel stimmen mit den Repository-Schlüsseln überein |
| Mehrtägiger Dauerlauf, reale Hardware und Clients | Ausstehend; [Praxistest](PRAXISTEST-3.0.0.md) durchführen |

Das [öffentliche Release](https://github.com/ra5on/TitanOS/releases/tag/titan-3.0.1)
enthält `titan-3.0.1-amd64.img.xz` (1.192.493.740 Bytes, eine Datei), das
RAUC-Update, Manifest, Signaturen, Paketinventar und Prüfberichte.
Die Downloads sind ohne GitHub-Anmeldung erreichbar. Der
[Veröffentlichungslauf](https://github.com/ra5on/TitanOS/actions/runs/37425803597)
hat Quellcommit, Tag, alle 13 Asset-Digests, beide Signaturen und die
RAM-/Schreibschutz-Nachweise nochmals geprüft; die signierten Assets sind
unverändert geblieben.
Der GitHub-SHA-256-Digest des komprimierten Images lautet:

```text
687164133cffe0f48b2144da0a5b3942f8001b2808a4d4207c071e392e8ac1f9
```

Die Laufzeit- und A/B-Berichte wurden zusätzlich als GitHub-Artefakt geladen
und mit den Release-Digests verglichen. Das komprimierte Image wurde für diesen
zusätzlichen Abgleich nicht erneut lokal heruntergeladen.

Die öffentliche Freigabe lässt die oben aufgeführten praktischen Prüfungen und
fehlenden Wiederherstellungsfunktionen weiterhin offen. Für die erste Systemfamilie
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
