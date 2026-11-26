# Konfiguration auf dem FAT-Bootmedium nach unterbrochenem Update

## Befund und Schreibpfad

Beim Abbruch eines A/B-Systemupdates in QEMU startete Slot A mit der vorigen Version. Die Freigabe war erreichbar. Beim nächsten Start meldete `fsck.fat` einen verwaisten 4096-Byte-Cluster. titand konnte `system/agent.json` nicht als JSON lesen. Ein lesender Zugriff auf das Testimage ergab eine leere Datei. Freigaben und Updatekonfiguration waren lesbar. Das erste serielle Protokoll hatte vor dem Abbruch einen Schreibzugriff auf die Agenteneinstellungen aufgezeichnet. Im damaligen Store stand der positive Journaleintrag **vor** dem Synchronisieren des Verzeichnisses. Aus diesem Eintrag allein lässt sich der dauerhafte Abschluss des Schreibens nicht ableiten.

Die automatische Wahl des ersten gemounteten Volumes in `api.agentVolume` speichert die Agenteneinstellungen über `config.Store.WriteJSON`. Der Store schreibt in eine temporäre Datei desselben Verzeichnisses, synchronisiert sie, schließt sie und benennt sie um. Danach synchronisierte er das Elternverzeichnis. Der Code dieses Pfads in v1.31.0 ist bytegleich mit PR #414.

## Gegenprobe mit frischem FAT32-Image

Die Probe startet den Kernel des Releases (Debian 6.12.118+deb13-amd64) mit einem kleinen separaten Initramfs. Jedes Szenario bekommt ein eigenes FAT32-Image zu 384 MiB mit 4-KiB-Clustern als emuliertes USB-Laufwerk. Im Gast kommt derselbe Mounttyp `vfat,flush,umask=0077` zum Einsatz. Ein Go-Test ruft den unveränderten öffentlichen `WriteJSON`-Pfad auf. Er liest die neuen 344 JSON-Bytes im Gast zurück. Erst nach erfolgreicher Rückgabe tötet der Host den QEMU-Prozess. Er liest das Image per mtools und prüft mit `fsck.fat -n`, bevor ein zweiter Start oder eine Reparatur das Ergebnis beeinflussen kann. Eine Freigabedatei und `update.json` werden mit ihren vor dem Boot gesicherten Originalbytes verglichen.

| Abbruchpunkt | Store von v1.31.0 | Geänderter Store |
|---|---|---|
| Neue Datei im bestehenden Systemordner, sofort nach Rückgabe | Dateiname vorhanden, Inhalt 0 Bytes, ein verwaister Cluster | Erwartete 344 Bytes, kein verwaister Cluster |
| Neue Datei, danach 8 MiB im Teilupdate geschrieben und bestätigt | Dateiname vorhanden, Inhalt 0 Bytes, ein verwaister Cluster | Erwartete 344 Bytes, kein verwaister Cluster |
| Vorhandene gültige Konfiguration ersetzt, sofort nach Rückgabe | Unter dem endgültigen Namen nicht lesbar, ein verwaister Cluster | Erwartete 344 Bytes, kein verwaister Cluster |
| Vorhandene Konfiguration ersetzt, danach 8 MiB Teilupdate bestätigt | Unter dem endgültigen Namen nicht lesbar, ein verwaister Cluster | Erwartete 344 Bytes, kein verwaister Cluster |
| Neue Datei in zwei zuvor fehlenden Elternordnern, sofort nach Rückgabe | Unter dem endgültigen Pfad nicht lesbar | Erwartete 344 Bytes, kein verwaister Cluster |

Der ursprüngliche Fehler tritt auch auf einem neu erzeugten Testmedium ohne vollständigen Updater und ohne `fsck`-Reparatur auf. Die beiden unbeteiligten JSON-Dateien blieben bytegleich.

## Korrektur und Geltungsbereich

Nach dem Umbenennen oder Entfernen eines Namens bestätigt TitanOS auf FAT das ganze betroffene Dateisystem per `syncfs`. Auf anderen Dateisystemen bleibt der vorhandene Verzeichnis-Sync erhalten. Identische Inhalte lösen weiterhin keine neue Schreiboperation aus. Nach einem fehlgeschlagenen Sync merkt sich der Store das betroffene Verzeichnis und versucht die Bestätigung beim Wiederholen auch für identische Bytes erneut. Der Journaltext über den erfolgreichen Schreibzugriff entsteht erst nach der Bestätigung. Fehler werden gemeldet. Die Anzahl der tatsächlichen Schreibzugriffe umfasst weiterhin auch fehlgeschlagene Bestätigungen.

Bei einer bereits unlesbaren oder leeren `agent.json` lassen die Automatik und die vorhandenen Einstellungswege die Datei bestehen. Eine gesonderte API-Probe mit einer bereits leeren Datei bestätigt, dass die automatische Wahl eines Volumes diese Datei weder löscht noch mit Standardwerten überschreibt. Für verlorenen früheren Inhalt wird eine intakte Konfigurationssicherung benötigt. Der zusätzliche Sync kann gleichzeitig ausstehende Schreibzugriffe auf demselben FAT-Dateisystem abwarten. Die Probe schützt den Fall, in dem `WriteJSON` bereits Erfolg zurückgegeben hat. FAT hat für einen Stromausfall **vor** dieser Rückgabe kein Transaktionsjournal. Physische USB-Controller, die Flush-Befehle vorzeitig bestätigen, wurden nicht erprobt.

## Wiederholen

Erforderlich sind QEMU, eine statisch gelinkte BusyBox, Go, cpio, Python, mtools, dosfstools sowie ein Kernel und seine passenden FAT-/USB-Module. Der zweite Parameter gibt das ausgepackte Verzeichnis `lib/modules/<Version>/kernel` an. Für den Vergleich ersetzt ein Go-Overlay lediglich `config/store.go` durch die Vergleichsversion. Beide Varianten durchlaufen denselben Test.

```sh
CONFIG_FAT_BASELINE=v1.31.0 backend/tests/config-fat-powercut.sh /pfad/zum/vmlinuz /pfad/zu/lib/modules/6.12.118+deb13-amd64/kernel
```
