# Obergrenzen für Freigaben

Eine Freigabe kann eine Obergrenze bekommen: Der Kernel lässt dann keine
Schreibvorgänge mehr zu, sobald die Dateien der Freigabe die Grenze erreichen.
Das gilt für jeden Zugriff (SMB, NFS, Apps, VMs mit Ordnerfreigabe, Dateimanager)
und für alle Konten. Windows und macOS sehen die Grenze als Größe des
Netzlaufwerks.

## Voraussetzungen

- Nur auf **ext4- und XFS-Volumes**. Btrfs kennt Grenzen nur für Subvolumes; die
  Freigaben von TitanOS sind normale Ordner. Auf Btrfs-Volumes gibt es deshalb
  keine Obergrenze, das Formular nennt den Grund. Eine Belegungsanzeige ohne
  Kontingent würde regelmäßiges Durchsuchen aller Dateien verlangen und ruhende
  Platten wecken; dafür gibt es weiterhin **Speicher › Verbrauch** auf Anfrage.
- Unter **Speicher › Volume › Freigabe-Kontingente** einmal pro Volume
  einschalten. Wirksam wird das erst beim nächsten Einbinden, also nach dem
  nächsten Neustart des NAS:
  - **XFS** wird dann mit `prjquota` eingebunden und zählt beim Einbinden selbst
    nach; das kann bei vielen Dateien einige Zeit dauern.
  - **ext4** erhält vor dem Einbinden einmalig die Funktionen `quota` und
    `project` (`tune2fs -O quota,project -Q prjquota`) und wird ebenfalls mit
    `prjquota` eingebunden; ohne diese Option zählt ext4 nur mit, stoppt aber
    keine Schreibzugriffe. tune2fs verweigert das bei einem nicht sauber
    ausgehängten Dateisystem; das Volume wird dann trotzdem eingebunden, die
    Kontingente bleiben ausstehend und der Grund steht beim Volume.
- Fehler beim Vorbereiten verhindern **nie** das Einbinden eines Volumes:
  Scheitert das Einbinden mit `prjquota`, bindet TitanOS ohne ein.
- „Aktiv“ zeigt die Seite nur, wenn der Kernel die Grenzen tatsächlich
  durchsetzt (`Q_XGETQSTATV`), nicht schon, wenn er nur mitzählt.

## Grenze setzen

Im Freigabe-Dialog unter **Allgemein › Obergrenze** in GB (0 = keine, mindestens
16 MiB). Beim ersten Setzen erhält die Freigabe eine eigene Projekt-ID; titand
schreibt sie im Hintergrund an alle vorhandenen Dateien und Ordner, neue erben
sie. Währenddessen steht „Vorhandene Dateien werden gezählt“. Symlinks,
Gerätedateien und eingehängte fremde Dateisysteme werden übersprungen.
Projekt-IDs beginnen bei 10000 und werden nie zweimal vergeben, auch nicht nach
dem Löschen einer Freigabe (Zähler in `/config/shares/quota-ids.json`).

Die Details einer Freigabe zeigen Belegung und Grenze; ab 90 % wird der Balken
gelb. Die Belegung wird nur gelesen, wenn jemand die Seite öffnet (höchstens
alle 30 Sekunden), damit ruhende Platten nicht geweckt werden.

Grenze entfernen (0), Freigabe löschen oder Kontingente am Volume ausschalten
hebt die Grenzen auf. Die Dateien behalten ihre Projekt-ID; das schadet nicht.
Bei ext4 bleibt die Zählung im Dateisystem eingeschaltet.

Dateien, die innerhalb desselben Volumes in eine begrenzte Freigabe verschoben
werden, kopiert der Kernel (EXDEV), damit sie richtig zählen. Das kann bei großen
Dateien länger dauern als ein Umbenennen.

## Grenzen der Umsetzung

- Kontingente pro Konto gibt es nicht; die Grenze gilt für die Freigabe.
- Auf ext4 ignoriert der Kernel die Grenze für Prozesse mit Root-Rechten
  (`CAP_SYS_RESOURCE`), auf XFS nicht. Betroffen sind die NAS-Konsole und titand
  selbst. titand prüft vor Uploads, Kopien, Entpacken und Downloads von einem
  Link den freien Platz der Freigabe; innerhalb einer Freigabe mit Grenze meldet
  das Dateisystem dabei den Rest bis zur Grenze. Mehrere gleichzeitig laufende
  Vorgänge können die Grenze auf ext4 deshalb knapp überschreiten. SMB, NFS und
  Docker-Container (ohne `CAP_SYS_RESOURCE`) werden auf beiden Dateisystemen
  gestoppt.
- Der Kernel zählt belegte Blöcke: kleine Dateien belegen mindestens einen Block.

## Prüfung

`go test ./internal/quota ./internal/shares ./internal/storage`, die Browserprüfung
`scripts/test-ui-share-quotas.mjs` mit isolierten Daten und in der CI der
QEMU-Storage-Test (`scripts/quota_smoke.py`): je ein XFS- und ein ext4-Volume auf
eigenen virtuellen Platten, Kontingente einschalten, Neustart, ein normales Konto
schreibt 100 MiB in eine Freigabe mit 64 MiB Grenze und wird vom Kernel gestoppt;
Belegung und Laufwerksgröße der Freigabe werden geprüft.
