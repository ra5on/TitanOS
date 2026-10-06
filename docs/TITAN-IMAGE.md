# Titan-Systemimage

Systemimage **3.0.1** enthält die eigene Titan-Anwendung **3.0.0** auf Debian 13
für x86-64. Der aktuelle Kandidat befindet sich in der Freigabeprüfung. Ein
Release-Entwurf ist ein Testbuild; Stable wird erst nach den abgeschlossenen
Laufzeit- und Praxistests freigegeben. Siehe [QA-3.0.0](QA-3.0.0.md).

[Releases und Downloads](https://github.com/ra5on/TitanOS/releases).
Ein Entwurf ist vor seiner Veröffentlichung nur für berechtigte
Repository-Nutzer sichtbar. Der öffentliche Download folgt nach
Release-Freigabe.

## Neuinstallation

Das Image ist für eine Neuinstallation auf einem separaten Testdatenträger
bestimmt. Es konvertiert kein anderes NAS-System und übernimmt dessen Daten
nicht automatisch. Beim Schreiben eines Images wird das ausgewählte Ziel
überschrieben; vorher vorhandene Daten unabhängig sichern.

Die Datei heißt `titan-3.0.1-amd64.img.xz` und wird vor dem Import oder Schreiben
entpackt. Der getestete Bootmodus ist **UEFI/OVMF mit deaktiviertem Secure Boot**.
Legacy-BIOS-Start, Secure Boot und ARM sind nicht freigegeben.

Für den Test sind **8 GiB RAM**, **64 GiB Datenträgerkapazität** und mindestens
zwei CPUs empfohlen. Das entpackte Image selbst ist 48 GiB groß. Für VMs
innerhalb von Titan muss der Host verschachtelte Virtualisierung verfügbar
machen; in einer VM außerdem die benötigten Geräte gezielt durchreichen.

Die virtuelle Systemplatte vor dem Start bei Bedarf vergrößern und als
Startlaufwerk auswählen. Netzwerk wird per DHCP eingerichtet. Die Konsole
zeigt `https://<NAS-IP>:5000`. Den Administrator beim ersten Aufruf im eigenen
Netz anlegen; es gibt kein vorgegebenes Kennwort. Das lokale TLS-Zertifikat
wird vom NAS selbst erzeugt und ist nicht automatisch öffentlich vertraut.

## System und Daten getrennt

Die Platte enthält EFI, Bootloader, zwei jeweils 16 GiB große ext4-Systemslots
und eine gemeinsame ext4-Datenpartition **DATA**. Auf der Neuinstallation ist
System A eingerichtet; System B wird beim ersten vollständigen Update
beschrieben. Der aktive Systemslot ist schreibgeschützt.

Persönliche Dateien, NAS-Konfiguration, Docker- und VM-Daten liegen im
persistenten Datenbereich. Die vorgesehenen veränderbaren Dienstverzeichnisse
werden von DATA eingebunden. System-Paketdatenbanken bleiben an ihren Slot
gebunden. Der Dateimanager bietet nur freigegebene Nutzer-Speicherbereiche an,
keinen schreibbaren Zugang zu Betriebssystemdateien.

Beim Start erweitert Titan die Datenpartition eines passenden Titan-Layouts
auf den freien Endbereich des Datenträgers. Eine größere SSD oder später
vergrößerte virtuelle Platte bringt damit mehr DATA-Kapazität; die beiden
Systemslots behalten ihre Größe. Es wird keine zusätzliche Systemplatte
automatisch formatiert. Zusätzliche Laufwerke können als benannte ext4-/XFS-
Volumes oder ZFS-Pools eingerichtet werden. Apps, VMs und Freigaben wählen
diese Speicherbereiche über ihre Namen.

## Updates und Rollback

**Systemsteuerung → Updates & Rollback** bietet den Kanal **Stable** und einen
gemeinsamen Ablauf für Titan, Debian und Sicherheitskorrekturen. Ein
Release-Entwurf wird nicht als verfügbares NAS-Update angeboten.

Ein geprüftes signiertes Systemupdate beschreibt den inaktiven Slot. Es wird
erst nach ausdrücklich bestätigtem Neustart aktiv. Die automatische Suche
oder Vorbereitung löst keinen automatischen Neustart aus. Das Dropdown zeigt
vorhandene bestätigte lokale Rollback-Stände; unmittelbar nach der ersten
Installation gibt es noch keinen Vorgänger.

Benutzer, Rechte, NAS-Einstellungen und App-/VM-/Nutzdaten bleiben auf DATA.
Unveränderte Systemkonfigurationen folgen dem gewählten Slot; lokale Änderungen
unter `/etc` bleiben in der persistenten Overlay-Schicht. System-UIDs und -GIDs
gehören zum signierten Kompatibilitätsvertrag. Unvereinbare Konten oder
Datenschemata werden vor dem Update abgewiesen.

Ein Rollback setzt Betriebssystem und Paketversionen zurück, aber keine
persönlichen Dateien oder Container-Datenbanken. Es ersetzt keine Sicherung.
Eine vollständige Wiederherstellung auf einem leeren Image ist noch nicht
implementiert; vorhandene Konfigurations- und App-Archive rekonstruieren nicht
automatisch alle Konten, Speicherzuordnungen und Dienste.

Der Bootloader versucht einen neuen Stand einmal. Erst die erfolgreiche
NAS-Dienstprüfung bestätigt ihn dauerhaft. Nach einem fehlgeschlagenen Start
wählt der nächste Neustart den vorherigen gesunden Stand. Ein hängender Gast
kann einen Reset durch den Host benötigen. EFI-/GRUB-Änderungen und ein anderes
Partitionslayout benötigen derzeit ein neues Installationsimage.

Mehr unter [Updates und Rollback](UPDATES.md).

## Prüfungen und Freigabegrenzen

Die Pipeline prüft echte Ersteinrichtung, HTTPS, SMB-Rechte, Docker-Lebenszyklus,
Speicher und VM-Domain-Verwaltung. Dazu kommen der PID-1-Nachweis des
schreibgeschützten Systemslots, signierter A→B-Wechsel, B→A-Rollback und
Fehler-Rückfall mit erhaltenen DATA-Daten. Ein realer 8→3→8-GiB-Kaltstart prüft,
dass der Autostart-RAM-Schutz beide Daemons anhalten kann, Web/Dateimanager
erreichbar bleiben und die Dienste nach RAM-Wiederherstellung zurückkehren.
Die Dateien `runtime-test.json` und `ab-test.json` dokumentieren diese Prüfungen.

Die erste Systemfamilie verwendet als Ausgangspunkt einen privaten
Boot-Overlay mit älterer signierter Identität. Dieser Bootstrap-Nachweis ist
keine Migration von einer vorhandenen fremden Installation. Spätere Updates
werden gegen einen signierten veröffentlichten Systemstand geprüft.

Die 374 mitgelieferten LinuxServer.io-/Big-Bear-Vorlagen sind normalisiert und
mit Compose geprüft; eine Laufzeitfreigabe jeder App ist damit nicht belegt.
Installiertes VM-Gastbetriebssystem mit VNC-Eingabe/Wiederverbindung, reale
USB-/GPU-/NPU-Durchreichung, Hardware-Dauerlauf und vollständige Wiederherstellung
bleiben konkrete Grenzen. Vollständige Neuinstallations-Wiederherstellung fehlt
als Funktion, nicht nur als Test. Ein grüner Build darf diese Punkte nicht als
geprüft ausgeben.

`SHA256SUMS` und `SHA256SUMS.sig` gehören zu den signierten Release-Dateien.
Den öffentlichen Schlüssel mit einer separat vertrauten Kopie von
`packaging/release-public.pem` vergleichen. Ein Schlüssel aus derselben
Downloadquelle allein ist kein unabhängiger Vertrauensnachweis.
