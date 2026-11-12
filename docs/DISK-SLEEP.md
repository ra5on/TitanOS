# Ruhemodus für Festplatten

Speicher › Weitere Optionen › Ruhemodus für Festplatten, nur Besitzer und Administratoren. Paket `storage` (`sleep.go`), Routen `GET`/`PUT /api/v1/storage/disk-sleep`.

## Einstellung

- **Standard:** eine Leerlaufzeit für alle unterstützten HDDs: Aus (ab Werk), 5, 10, 15 oder 30 Minuten, 1, 2 oder 3 Stunden.
- **Je Volume** (Entscheidung des Projektinhabers vom 29.10.2026): „Standard übernehmen“, „Aus“ (nie schlafen) oder eine der Zeiten. Alle Mitglieder des Volumes (mdadm, Btrfs, auch single) bekommen diesen Wert.
- **Je Platte:** dieselbe Auswahl für jede einzelne HDD, auch für Platten in keinem Volume (Ersatzplatte, USB-HDD).
- **Vorrang:** Platte vor Volume vor Standard. Die Seite zeigt für jedes Volume und jede Platte den wirksamen Wert und seine Herkunft („eigene“, „vom Volume“, „Standard“).

Eine Platte ohne eindeutige Seriennummer und ohne WWN (oder mit derselben Seriennummer wie eine andere Platte, wie es manche USB-Gehäuse melden) bekommt keinen eigenen Wert; sie übernimmt den ihres Volumes bzw. den Standard. So gilt ein Wert nie unbemerkt für eine andere Platte.

## Wo man ihn sieht und einstellt (seit 0.46)

- **Speicher › Volumes** und **Laufwerke:** Spalte „Ruhemodus“ mit Wert, Zustand (schläft/aktiv) und Herkunft; bei einem Volume „2 von 4 Platten schlafen“. Ein Klick auf die Zelle öffnet den Dialog mit diesem Volume bzw. dieser Platte markiert. Volume- und Plattennamen sind unterstrichene Verweise mit `›`, die ganze Zeile ist anklickbar.
- **Zeitpunkte:** Wo der Zustand steht (Spalte, Dialog, Volume- und Plattendetails), steht „schläft seit hh:mm“ bzw. „zuletzt geweckt hh:mm“. Beides liegt nur im RAM und ist nach einem Neustart von titand unbekannt. Ist bekannt, wodurch die Platte geweckt wurde, steht statt „zuletzt geweckt“ „aufgeweckt heute hh:mm durch …“.
- **Aufgeweckt durch (seit 0.77, PLAN 79-6):** In den Plattendetails, den Volume-Details und im Dialog lässt sich unter dem Zustand „Letzte Weckvorgänge (n) · heute n×“ aufklappen: Zeitpunkt und Ursache der letzten bis zu 20 Weckvorgänge, darunter Hinweise – weckt TitanOS die Platte für eigene Daten (Protokolle, Meldungen, Verläufe, Konfigurationssicherung), führt ein Verweis zu Systemsteuerung › Daten und Sicherung › Systemdaten; „unbekannt“ und „vermutlich“ werden erklärt. Ohne aufgezeichneten Weckvorgang gibt es die Liste nicht.
- **Plattendetails (Laufwerke):** Abschnitt „Ruhemodus“ mit Wert, Herkunft, Zustand, Zeitpunkt oder dem Grund, warum die Platte nicht schlafen kann; der eigene Wert der Platte lässt sich dort ändern (dieselbe Anfrage wie der Dialog), „Ruhemodus einstellen“ öffnet den Dialog.
- **Einstellungen › Energie:** Kachel „Ruhemodus der Festplatten“ mit Standard und Zustand; sie öffnet denselben Dialog.
- **Volume-Details:** Abschnitt „Ruhemodus“ mit dem Wert, dem Zustand jeder Platte und einem kleinen Editor („Ändern …“) für das Volume und seine Platten; er sendet dieselbe Anfrage wie der Dialog. Hat ein Volume keine Platte, die schlafen kann, steht das mit dem Grund je Platte da.
- **Dialog** (Speicher › Weitere Optionen › Ruhemodus): unverändert die zentrale Stelle; neu der Abschnitt „Platten ohne Ruhemodus“ mit Grund (SSD, NVMe, Wechseldatenträger, Bootstick, kein SATA-/SAS-Laufwerk, Anschluss ohne Standby).
- Vorher blendete der Dialog die Volume- und Plattenlisten aus, wenn keine Platte schlafen konnte (leere Listen fehlten in der Antwort, `omitempty`). `listed: true` sagt jetzt, dass die Platten gelesen wurden.

## Datei

`/config/storage/disk-sleep.json`, nur bei einer Änderung durch die Person atomar geschrieben:

```json
{
  "idleMinutes": 30,
  "volumes": { "tank": 0 },
  "disks": { "serial:WD-WCC4N7XYZ123": 60, "wwn:0x5000c500a1b2c3d4": 0 }
}
```

- `volumes` nach dem Volume-Namen aus `volumes.json`, `disks` nach Seriennummer (`serial:…`) bzw. WWN (`wwn:…`, wenn die Platte keine Seriennummer meldet). **Nie Gerätenamen** wie `sda`: Eine Platte, die nach einem Neustart `sdc` statt `sdb` heißt, behält ihren Wert. `0` = nie schlafen, ein fehlender Eintrag übernimmt.
- Ohne Ausnahmen bleibt die Datei genau `{"idleMinutes": N}`. Ältere titand lesen nur `idleMinutes` und übergehen die beiden Felder; speichert ein älterer titand, schreibt er nur den Standard zurück und die Ausnahmen entfallen.
- Ausnahmen für Volumes oder Platten, die es nicht mehr gibt, werden nie von selbst entfernt. Die Seite nennt sie und entfernt sie beim nächsten Speichern. Das Diagnosepaket ersetzt die Platten-Schlüssel durch `disk-1`, `disk-2` …

## API

- `GET /api/v1/storage/disk-sleep`: Einstellung (`idleMinutes`, `volumes`, `disks`), `available`, `errors`, dazu `listed`, `unavailable` (Platten, die nicht schlafen können, mit Grund `ssd`/`nvme`/`removable`/`stick`/`name`/`transport`), `volumeStatus` und `diskStatus` (Name, Modell, Seriennummer, Größe, Volume, eigener Wert oder `null`, wirksamer Wert, Herkunft, Zustand `asleep`/`active` aus dem RAM, optional `since` (RFC 3339: bei `asleep` der Standby-Zeitpunkt, bei `active` die erste danach erkannte Aktivität; fehlt, wenn unbekannt), optional `wakes` (höchstens 20, neueste zuerst: `at`, `causes[]` mit `code` und optional `detail`; nie leer, ohne Wissen `unknown`), `wakesToday` und `wakeDays` (`day` JJJJ-MM-TT in Ortszeit des NAS, `count`, höchstens 7 Tage), letzter Standby-Fehler) sowie `missingVolumes`/`missingDisks`. Die Platten kommen aus `lsblk` wie auf der Speicherseite; keine Platte wird gelesen oder geweckt. Dieselben Felder stehen in `diskSleep` von `GET /api/v1/storage`.
- `PUT /api/v1/storage/disk-sleep`: `idleMinutes` ist Pflicht. `volumes` und `disks` ersetzen, wenn angegeben, alle Ausnahmen dieser Art; fehlen sie, bleiben die gespeicherten (ältere Oberflächen). Werte wie beim Standard, sonst `invalid_disk_idle_time`. Ein neuer oder geänderter Eintrag für ein unbekanntes Volume oder eine nicht vorhandene bzw. nicht unterstützte Platte (SSD, NVMe …) sowie ein Schlüssel ohne `serial:`/`wwn:` ergibt `unknown_disk_sleep_target` (400); ein unverändert gespeicherter Eintrag einer gerade fehlenden Platte bleibt erlaubt.

## Verhalten

- `storage.RunDiskSleep` liest alle zehn Sekunden `/sys/block/*/stat`; jede Platte hat ihre eigene Zeitmessung mit ihrem wirksamen Wert. Welche Platte zu welchem Volume gehört, ergibt sich aus den UUIDs der Mitgliedspartitionen im `lsblk`-Baum (Array-UUID, Btrfs-UUID, LUKS-UUIDs), nicht aus `/config`. Platten mit dem wirksamen Wert „Aus“ werden nicht beobachtet; sind alle Werte aus, ruht die Zeitmessung ganz.
- Jede Änderung der Einstellung beginnt eine volle neue Ruhezeit für alle Platten. „Aus“ schickt keinen Befehl und weckt keine schlafende Platte.
- **RAID:** Ein Volume schläft erst wirklich, wenn alle seine Platten ruhen. Ein Zugriff auf ein RAID weckt die schlafenden Mitglieder ohnehin mit (mdadm und Btrfs lesen und schreiben verteilt); unterschiedliche Werte innerhalb eines Volumes sparen daher wenig. Eine Ausnahme je Platte ist vor allem für Platten außerhalb von Volumes gedacht.
- Ausnahmen wie bisher: SSDs, NVMe, Bootstick, Virtio-/Netzplatten, an VMs vergebene Platten, Platten mit laufendem Test und Mitglieder von Arrays bzw. Btrfs-Volumes in Prüfung oder Wiederaufbau. Standby einzeln über `hd-idle` (ATA bei SATA, SCSI bei SAS mit Power Condition 3, USB); `hd-idle.service` bleibt maskiert.

## Aufgeweckt durch

TitanOS verfolgt keine Plattenzugriffe. Stattdessen merkt jeder Teil von TitanOS, der ein Volume anfasst, das unmittelbar davor im RAM vor (Paket `diskwake`, keine I/O); beim ersten I/O nach einem bestätigten Standby nimmt die Zeitmessung, was für das Volume der Platte im Fenster davor vorgemerkt wurde (Regel und Fenster: `docs/rules/STORAGE.md`, „Aufgeweckt durch“).

| Kennung (`code`) | Bedeutung, `detail` |
|---|---|
| `journal`, `activity_log`, `notifications`, `smart_history`, `monitoring`, `config_backup` | eigene Daten von TitanOS auf dem Volume (Abhilfe: Systemdaten-Volume wählen) |
| `backup_job` | Datensicherung, `detail` = Auftrag (während des ganzen Laufs) |
| `app_backup`, `replication`, `snapshot_plan`, `snapshot`, `shadow_copies`, `recycle_sweep` (`detail` = Freigabe), `file_index`, `thumbnails`, `usage_scan`, `quota_scan`, `file_transfer`, `agent`, `data_move`, `private_data` (`detail` = Ordner unter `.titanos`) | weitere Arbeit von TitanOS |
| `maintenance` | `detail` `raid`/`btrfs` (Prüfung oder Wiederaufbau übernimmt die Platte), `check` (geplante Volume-Prüfung), `plan` (Wartungsdaten) |
| `disk_test`, `vm_disk` (`detail` = VM) | Plattentest bzw. durchgereichte Platte übernimmt eine schlafende Platte |
| `vm_start`, `lxc_start`, `container_start`, `docker_data` | Start einer VM, eines Containers, einer App bzw. von Docker; bei Docker nur das Volume des Datenordners |
| `file_manager`, `webdav` | Anfrage über Dateimanager/Datei-API bzw. WebDAV, `detail` = Konto |
| `smb_session` (`detail` = Freigabe), `docker_running` | **vermutlich**: nichts von TitanOS war vorgemerkt, aber ein SMB-Client war verbunden bzw. Docker läuft auf dem Volume |
| `unknown` | nichts davon: nicht TitanOS selbst (NFS, App, Container, VM-Abbild, SMB ohne erkennbare Verbindung) |

Grenzen: Zugriffe über SMB und NFS selbst sieht TitanOS nicht (Samba protokolliert nur Löschen/Umbenennen geschützter Freigaben); eine SMB-Verbindung ist ein Indiz, kein Beleg. Laufende Container und VMs schreiben ohne TitanOS und erscheinen als „unbekannt“ bzw. „vermutlich Docker“. Platten außerhalb von Volumes haben außer Test/VM immer „unbekannt“. Ein vorgemerkter Zugriff, den der Seitencache bedient, weckt nichts und wird dann auch nie genannt; fällt aber zufällig ein fremder Zugriff ins selbe Fenster, wird der vorgemerkte genannt. Die Historie liegt im RAM und in `.titanos/disksleep/wakes.json` auf dem Systemdaten-Volume, sofern dessen Platten nie schlafen; dürfen alle Volumes schlafen, ist sie nach einem Neustart von titand leer. Im Entwicklungsmodus läuft die Zeitmessung nicht.

## Prüfstand

Go-Tests (`backend/internal/storage/sleep_test.go`, `sleep_override_test.go`, `backend/internal/api/storage_sleep_test.go`): Auflösung und Vorrang, Volumes mit „Aus“, Platten außerhalb von Volumes, Umbenennung `sdb` → `sdc`, doppelte Seriennummern, alte Datei hin und zurück, Prüfung unbekannter Volumes/Platten und Gerätenamen, Entfernen erst beim Speichern, Diagnosepaket ohne Seriennummern; Webtest `web/tests/disk-sleep.test.mjs`. Einschlafen und Aufwachen mit echten Platten, RAID-Mitgliedern und USB-Gehäusen sind noch nicht auf Hardware geprüft.

Aufgeweckt durch (0.77): Go-Tests `backend/internal/diskwake/diskwake_test.go` (Fenster, Reihenfolge, Grenzen, Aus behält nichts, Vormerken ohne I/O und ohne Allokation) und `backend/internal/storage/wake_test.go` (Ursache, mehrere Ursachen, Fenster 40 s/70 s, nichts von vor dem Einschlafen, unbekannt, Nachfrage nur ohne Ursache, Test/VM/Wartung, 20 Weckvorgänge/14 Tage, Datei nur auf einem Volume, das nie schläft, nie `/config`); Webtest `web/tests/lib/sleep-view.test.ts`; UI-Suite `scripts/test-ui-disk-sleep-times.mjs` mit Aufnahmen bei 1100 und 390 px. Nicht geprüft: echte Platten, ob das Fenster von 45 s zu den Schreibverzögerungen von ext4/XFS/Btrfs auf dem NAS passt, `smbstatus` mit echten Clients.
