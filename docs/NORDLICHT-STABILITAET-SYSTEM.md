# Installationsabbruch und Verlust der Verwaltungsverbindung

## Prüfstand

QEMU mit OVMF (UEFI und systemd-boot), Softwareemulation und einem als USB-Massenspeicher präsentierten FAT32-Abbild. Die SMB-Daten lagen auf einem zweiten, frisch angelegten ext4-Datenträger. Der erste Netzwerkport hatte eine nur auf Loopback veröffentlichte HTTPS- und SMB-Weiterleitung. Für die Bridge stand ein zweiter, vom Verwaltungszugang isolierter Port bereit.

Grundlage waren Installations-ZIP und Manifest von v1.31.0. Beide Minisign-Signaturen und die SHA-256-Werte von Kernel, Initramfs und Root-Dateisystem wurden geprüft. Für den Versuch erhielt die Wegwerf-Kopie den Go-Daemon aus dem Arbeitsstand und einen zusätzlichen Wegwerf-Prüfschlüssel. Die Kennung 1.31.0 des ersten Slots wurde für den Versionsvergleich beibehalten. Der lokale Release-Server lieferte ein mit dem Testschlüssel signiertes Angebot. Damit wurden Backend, Bootpfad und Samba im Gast geprüft. Die gebaute Desktop-Oberfläche und WebKit wurden separat geprüft.

## Systemupdate und Freigaben

Vorhanden waren Signaturprüfung, getrennte A/B-Slots und Probestarts samt Rückfall. Der QEMU-Test `scripts/smoke-system-update.py` deckte bereits ein erfolgreiches Update, einen nicht startenden Daemon, eine Kernelpanik und einen im Notfallmodus stehenden Start ab.

Der zusätzliche Fall `--scenarios interrupted` erzeugt über die vorhandene API ein ext4-Volume und eine Freigabe. Er schreibt 32.768 Zufallsbytes über SMB und liest sie über eine neue Verbindung zurück. Der lokale Server hält die Lieferung des Systemabbildes nach 8 MiB an. Das Skript liest die unvollständige Datei auf dem FAT-Stick und vergleicht ihre Bytes mit der Quelldatei. Erst danach beendet es den eigenen QEMU-Prozess hart und startet das gleiche Abbild erneut. Dabei prüft es Boot-Eintrag, laufenden Slot, Version, ausstehende Umschaltung, die Auswahl des Agent-Volumes und die zuvor geschriebene SMB-Datei.

Im ersten vollständigen Start-Abbruch-Neustart-Versuch waren 8.388.608 korrekt geschriebene Bytes nachweisbar. Die Firmware startete den vorherigen Slot A. Er meldete die vorherige Version ohne ausstehende Umschaltung. Die SMB-Datei hatte wieder alle ursprünglichen Bytes. Unabhängig davon war die beim ersten Mount automatisch erzeugte `system/agent.json` im FAT-Dateisystem anschließend leer. Im Journal stand vor der Unterbrechung die zuvor gemeldete Schreibaktion und nach dem Neustart ein JSON-Lesefehler. Die Datei der Freigaben blieb lesbar. Der Hostzugriff mit mtools öffnete das Image ausschließlich lesend. Eine separate Prüfung mit demselben Release-Kernel und dem damals eingesetzten Config-Store konnte den Verlust beim Abbruch nach erfolgreichem Schreibaufruf unabhängig reproduzieren. Der Store synchronisiert auf FAT jetzt das Dateisystem nach der Namensänderung. Der gleiche isolierte Versuch bewahrte damit die vollständige JSON-Datei. Unveränderte Einstellungen lösen weiterhin keinen Schreibvorgang aus. Die fünf getrennten Vorher- und Nachher-Proben stehen im [Prüfbericht zum FAT-Konfigurationsspeicher](NORDLICHT-STABILITAET-KONFIGURATION.md).

Nach der Korrektur wurde der komplette Fall mit einem frischen Wegwerf-Stick wiederholt. Während des Schreibens gingen erneut 8.388.608 Bytes des Systemabbilds auf den Stick. Ihr SHA-256-Wert entsprach auch nach dem Abbruch dem gleich langen Anfang des signierten Quellabbilds. Nach dem zweiten Firmwarestart war die komplette 356-Byte-JSON mit dem Volumenamen `testdaten` vorhanden. Der Daemon lieferte denselben Wert. Slot A startete mit 1.31.0 ohne ausstehende Umschaltung. Eine neue SMB-Verbindung las die 32.768 ursprünglichen Bytes zurück. Der Gast beendete den Test geordnet, Exitstatus 0.

### Boot-Rückfall nach einer Kernelpanik

Der vorhandene Fall `--scenarios panic` wurde zusätzlich auf einem eigenen UEFI-Wegwerfgast mit 2304 MiB RAM und Softwareemulation ausgeführt. Im Versuch erhielt Slot B die synthetische Version 1.31.3 mit defektem Initramfs. Die serielle Konsole zeigte drei Kernelpaniken und drei selbsttätige Neustarts nach 22, 21 und 20 Sekunden. Der auf FAT gelesene Restzähler sank auf 2, 1 und 0. Anschließend startete Slot A wieder mit 1.31.0 und blieb als nächste Bootwahl gesetzt. Sowohl die Fehlermeldung als auch die Datei `config/system/update-failed.json` nannten den fehlgeschlagenen Versuch. Das Bootprotokoll und der Prüfablauf endeten erfolgreich.

## Netzwerk

Vorhanden waren eine zweiminütige Bestätigungsfrist, getrennte gespeicherte und probeweise Konfigurationen sowie eine erneute Rücknahme nach Fehlern. Die erste unbestätigte Änderung auf eine unerreichbare IP zeigte ein Problem. Der Helfer für die Firewall-Weiterleitung lief bei der Rücknahme dreimal ins Zeitlimit. In dieser Zeit kam der alte Netzwerkpfad nicht zurück. Die serielle Konsole erlaubte diese Diagnose trotz fehlendem HTTPS. Im selben ersten Lauf meldete der Treiber für den unverbundenen virtuellen Zweitport TX-Zeitüberschreitungen. Ob sie den Helfer-Timeout auslösten, ist offen. Der korrigierte Prüfstand verwendet auf dem Zweitport ein isoliertes QEMU-Netz ohne IPv4 und ohne externe Weiterleitung. Der Go-Test erzwingt den Helferfehler unabhängig vom Netztreiber.

Die Rücknahme stellt nun die bisherigen Links mit eigenem Zeitbudget wieder her, einschließlich eines erneuten networkd-Reloads nach einem zuvor fehlgeschlagenen Versuch. Danach wird der vorhandene Weiterleitungshelfer ausgeführt. Schlägt er fehl, bleibt die Rücknahme als ausstehend markiert und wird wiederholt. Beim Anwenden einer neuen Topologie bleibt die bestehende Vorbedingung für die Weiterleitung erhalten. Zwei Go-Regressionstests decken die Reihenfolge und die erneute Anwendung bereits geschriebener Konfigurationsdateien ab.

Der Fall `--scenarios network` nutzt denselben authentifizierten PUT wie die Netzwerkseite. Nach jeder Änderung fordert er einen fehlgeschlagenen HTTPS-Zugriff, die im Gast protokollierte Rücknahme, dieselbe Schnittstelle mit der alten IPv4-Adresse, dieselbe gespeicherte Netzkonfiguration und eine erneute vollständige Leseprobe der SMB-Datei. Es gibt drei aufeinanderfolgende Versuche mit unterschiedlicher Ursache.

| Unbestätigte Änderung | Beobachteter Ausfall und Rückkehr | Erneute Leseprobe |
| --- | --- | --- |
| Statische IPv4 `192.0.2.17/24` mit unpassendem Gateway | Ursprüngliches HTTPS unerreichbar. Nach 132 Sekunden ursprüngliche Konfiguration und `10.0.2.15/24` auf `enp0s3`. | 32.768 von 32.768 ursprünglichen SMB-Bytes identisch. |
| Verwaltungsnetz in VLAN 37 ohne Trunk | Ursprüngliches HTTPS unerreichbar. Nach 131 Sekunden ursprüngliche Konfiguration, Schnittstelle und IPv4. | 32.768 von 32.768 ursprünglichen SMB-Bytes identisch. |
| Verwaltungs-Bridge `br0` auf zweitem, isoliertem Port | Ursprüngliches HTTPS unerreichbar. Nach 131 Sekunden ursprüngliche Konfiguration, Schnittstelle und IPv4. | 32.768 von 32.768 ursprünglichen SMB-Bytes identisch. |

Alle drei Messungen liefen nacheinander auf demselben Datenvolume. Die Zeitangabe umfasst Antrag, Bestätigungsfrist und erneute erfolgreiche SMB-Leseprobe. Nach jeder Frist protokollierte der Daemon die ausgelöste Rücknahme, systemd-networkd bezog die vorige DHCP-Adresse. Der zweite Durchgang endete ohne Rücknahmefehler. Exitstatus des Wegwerfgastes 0.

## Ergänzende automatisierte Prüfungen

Die Go-Pakettests `internal/network` und `internal/update` liefen mit frischem Testcache erfolgreich. Vier Python-Tests prüfen Bootzähler, Versionsableitung und den Ablauf bei verlorener Antwort auf die Volume-Erstellung. 17 Release-Klassifizierungstests bestätigen die bestehende Behandlung der QEMU-Prüfskripte. Die Fehlersimulation formatiert ihren virtuellen Datenträger bei einer verlorenen Antwort genau einmal und wartet anschließend auf die sichtbare Mount- und SMB-Bereitschaft.

## Grenzen

Der untersuchte Installationszeitpunkt ist das Schreiben des inaktiven Systemabbilds. Firmware- und Bootloaderwechsel, eine Unterbrechung beim Ändern des Boot-Eintrags, Legacy-BIOS, reale USB-Sticks mit eigenem Cache, verwaltete Switches und das Verhalten konkreter Netzwerkkarten wurden in diesem Durchgang nicht praktisch abgenommen. Die SMB-Leseproben wurden über die veröffentlichte Adresse durchgeführt. Die Namenssuche über mDNS war kein Teil der SMB-Abnahme. Die bestehenden Neustart- und Rückfall-Szenarien bleiben in der CI-Matrix. Die älteren Fälle mit erfolgreichem neuem Slot, ausbleibendem Daemonstart und Stillstand im Notfallmodus wurden für diesen Bericht lokal nicht erneut vollständig ausgeführt.


## Protokolle der lokalen Abnahme

Zum Vergleichsordner `TitanOS-Nordlicht-Desktop/Stabilitaet` gehören die Läufe `Installationsabbruch.log`, `Netzwerk-Ruecknahmen.log` und `Panic-Rollback.log` mit den zugehörigen seriellen Konsolenausgaben. `Netzwerk-Erstbefund.log` und sein serielles Protokoll enthalten den fehlgeschlagenen Durchgang vor der Netzkorrektur.
