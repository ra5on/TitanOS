# Partitionseditor

Der Partitionseditor ergänzt „Laufwerk formatieren“ im Dateimanager
(Dateien › Laufwerk formatieren …, nur Besitzer und Administratoren). Neben
jedem Laufwerk der Liste steht „Partitionen …“. Er zeigt die Aufteilung einer
Platte, die zu keinem Volume des NAS gehört, und ändert einzelne Partitionen,
ohne die ganze Platte zu löschen.

Code: `backend/internal/usb/partitions.go`, Oberfläche
`web/src/routes/files/PartitionEditor.svelte` und `partitions.ts`.

## Welche Platten

Genau dieselben wie beim Formatieren (`GET /api/v1/usb/disks`): Der Boot-Stick,
Platten mit Dateisystem-, Array- oder LUKS-UUID eines Volumes (`storage.Owns`;
die LUKS-UUIDs halten auch gesperrte verschlüsselte Volumes fern, siehe
[ENCRYPTION.md](ENCRYPTION.md)), virtio-/Netzplatten und leere Kartenleser erscheinen nicht und haben keine ID.
Schreibgeschützte Platten, Platten einer VM oder eines Plattentests und eine
Platte, an der gerade ein Auftrag läuft, werden angezeigt, lassen sich aber
nicht ändern.

Die ID ist derselbe Hash über Gerät, Seriennummer, Größe, Tabelle und
Dateisysteme (einschließlich der Partitionsgrößen). Jede Änderung, auch ein
anderer Stick im selben Anschluss, ergibt eine neue ID; eine alte ID endet mit
`usb_not_found`. Die Oberfläche sucht die Platte danach über Gerät,
Seriennummer, Modell und Größe wieder.

## Anzeige

`GET /api/v1/usb/disks/{id}/partitions` liefert:

- `table`: `gpt`, `dos` (MBR) oder leer (keine Tabelle), `sectorSize`;
- `partitions`: Nummer, Gerät, Beginn und Größe in Bytes, Typ (GPT-GUID bzw.
  MBR-Typ), Dateisystem, Name, UUID, eingebunden, `reason` (warum nicht
  änderbar), `growBy` (freier Platz direkt dahinter, den die Partition nehmen
  kann) und `renamable`;
- `gaps`: freie Bereiche ab 1 MiB, Beginn und Ende auf 1-MiB-Grenzen, mit der
  davorliegenden Partition (`after`);
- `reason` für die ganze Platte und den letzten Auftrag (`job`).

Die Daten stammen aus `sfdisk --json` (Lage und Typ) und lsblk (Dateisystem,
Name, Einbindung, Halter). Bei GPT gilt der Bereich `firstlba`–`lastlba`, bei
MBR alles ab Sektor 1 bis höchstens 2³² Sektoren. Eine unbekannte oder nicht
lesbare Tabelle (`reason: table`) erlaubt nur eine neue Tabelle. Eine
MBR-Tabelle mit erweiterter Partition (`reason: extended`) erlaubt nur
Formatieren, Umbenennen und eine neue Tabelle.

Die Oberfläche zeigt einen waagrechten Balken (Partitionen in Akzentfarbe,
belegte gedämpft, freie Bereiche gestrichelt, jeder mindestens 2 % breit) und
darunter eine Tabelle mit den Aktionen je Zeile. Auf dem Smartphone scrollt
die Tabelle in sich.

## Aktionen

Alle Aktionen: nur Besitzer/Administratoren, nicht im Entwicklungsmodus
(`read_only`), nicht während Speicheraktionen oder RAID-Wachstum (`usb_busy`),
nur eine zugleich (gemeinsame Sperre mit Formatieren). Sie laufen als
Hintergrundauftrag mit Phase (`check`, `wipe`, `partition`, `format`, `label`,
`resize`, `done`, `failed`) und enden mit der Meldung
`usb_partitions_changed` bzw. `usb_partitions_failed`. **Im Hintergrund wird
die Platte direkt vor dem ersten Schreibzugriff erneut gelesen** und die Aktion
noch einmal gegen diesen Stand geprüft; hat sich die ID oder die Partition
geändert, endet der Auftrag ohne Schreibzugriff.

Die betroffene Partition darf weder eingebunden (von TitanOS: „Erst
auswerfen“, anderswo/Swap: „In Benutzung“) noch von einem zusammengesetzten
RAID, LVM oder einer geöffneten Verschlüsselung gehalten sein. Andere
Partitionen derselben Platte dürfen eingebunden bleiben: sfdisk schreibt mit
`--no-reread`, danach meldet `partx -a|-d|-u --nr <n>` nur die geänderte
Partition dem Kernel, anschließend `udevadm settle`.

| Aktion | Route | Befehle |
|---|---|---|
| Neue Tabelle | `POST …/table` `{scheme: gpt\|dos}` | `wipefs` aller Partitionen und der Platte, `sfdisk --wipe always --wipe-partitions always` mit `label: …`, `partx -u`. Nur wenn die ganze Platte formatierbar ist (nichts eingebunden); MBR nur bis 2 TiB. |
| Anlegen | `POST …/partitions` `{start, size, filesystem, label}` | `sfdisk --no-reread --wipe-partitions always -N <n>` mit `start=…, size=…, type=…`, `partx -a`, warten auf das Gerät, optional `mkfs` wie beim Formatieren. |
| Löschen | `DELETE …/partitions/{n}` | `sfdisk --no-reread --delete`, `partx -d`. Die Daten werden nicht zusätzlich überschrieben. |
| Formatieren | `POST …/partitions/{n}/format` `{filesystem, label}` | `wipefs`, bei abweichendem Typ `sfdisk --part-type` + `partx -u`, `mkfs`. |
| Umbenennen | `PUT …/partitions/{n}/label` `{label}` | `exfatlabel`, `fatlabel` (Großbuchstaben), `ntfslabel`, `e2label`, `xfs_admin -L`, `btrfs filesystem label`. Fehlt das Werkzeug oder kennt TitanOS keins: `usb_partition_unsupported`. |
| Vergrößern | `POST …/partitions/{n}/grow` `{size}` | `sfdisk --no-reread -N <n>` mit gleichem Beginn, `partx -u`, dann das Dateisystem (siehe unten). |

### Größen und Ausrichtung

- Ein neuer Beginn muss auf einer 1-MiB-Grenze in einem freien Bereich liegen.
- `size` 0 nimmt den Rest des Bereichs, sonst wird auf ganze MiB abgerundet.
- GPT: höchstens 128 Partitionen, MBR: höchstens vier primäre.
- Typ: exFAT, FAT32, NTFS auf GPT „Microsoft basic data“, sonst „Linux
  filesystem“; MBR `7` (exFAT/NTFS), `c` (FAT32 ab 512 MiB), `e` (FAT16
  darunter), `83` (Linux und ohne Dateisystem).
- Mindestgrößen und Namensregeln der Dateisysteme sind dieselben wie beim
  Formatieren der ganzen Platte (XFS 300 MiB, Btrfs 256 MiB, FAT32-Namen nur
  A–Z, Ziffern und einige Zeichen).

### Vergrößern

Nur in freien Platz **direkt hinter** der Partition; das neue Ende liegt auf
einer 1-MiB-Grenze. Verkleinern gibt es nicht.

- ext2/3/4: `e2fsck -f -p` (Ausgang 1 = automatisch behoben, gilt als in
  Ordnung), dann `resize2fs`.
- XFS: kurz mit `nosuid,nodev,noexec` unter `/run/titanos-partitions/grow-*`
  eingebunden, `xfs_growfs`, wieder ausgehängt.
- Btrfs: ebenso mit `btrfs filesystem resize max`. Liegt dasselbe Btrfs auf
  mehreren Geräten, wird abgelehnt.
- NTFS: `ntfsresize --no-progress-bar` ohne `--force`; ein NTFS mit
  Ruhezustand oder Windows-Schnellstart wird nicht verändert und der Auftrag
  schlägt fehl.
- FAT32, exFAT und fremde Inhalte (RAID-, LVM-, LUKS-Mitglieder): abgelehnt.
- Ohne Dateisystem wächst nur die Partition.

Schlägt das Vergrößern des Dateisystems fehl, ist die Partition bereits größer
als das Dateisystem. Das ist ungefährlich; der Auftrag meldet den Fehler.

## Bestätigung

Neue Tabelle, Löschen und Formatieren haben einen einfachen
Bestätigungsschritt im Dialog, der Platte (Modell, Gerät), Partition und den
Datenverlust nennt; nichts muss eingetippt werden. Der gefährliche Knopf steht
links von „Abbrechen“ und wird nie durch Enter ausgelöst. Anlegen, Vergrößern
und Umbenennen löschen keine Daten und starten nach dem Formular direkt; der
Vergrößern-Dialog nennt vorher alte und neue Größe.

## Fehlercodes

`usb_not_found` (Platte oder Partition nicht mehr so vorhanden),
`usb_not_formattable` (belegt, mit Grund), `usb_format_invalid` (Größe, Lage,
Name, Dateisystem passen nicht), `usb_partition_unsupported` (Tabelle oder
Dateisystem erlauben das nicht, Werkzeug fehlt), `usb_busy`, `read_only`.

## Prüfung und Grenzen

- Go-Tests (`partitions_test.go`) mit nachgebautem lsblk/sfdisk: Ausschlüsse,
  eingebundene Partitionen, ID-Wechsel vor dem Start und zwischen Klick und
  Hintergrundprüfung, Befehlsfolgen aller Aktionen, Ausrichtung auch für
  4-KiB-Sektoren, MBR mit logischen Partitionen, mehrteiliges Btrfs.
- Webtest `web/tests/partitions.test.mjs` und Browserprüfung
  `scripts/test-ui-drive-format.mjs` mit isolierten API-Daten.
- Nicht geprüft: echte Platten, USB-Adapter und Kartenleser, das Verhalten
  von `partx` bei eingebundenen Nachbarpartitionen auf echter Hardware sowie
  `ntfsresize`/`xfs_growfs`/`btrfs` gegen echte Dateisysteme. Keine Tests auf
  Host-Platten ausführen; nur Wegwerf-VMs mit neu erzeugten Abbildern.
- Nicht umgesetzt: Verkleinern, Verschieben, logische Partitionen anlegen,
  GPT-Partitionsnamen setzen, Kopieren von Partitionen.
