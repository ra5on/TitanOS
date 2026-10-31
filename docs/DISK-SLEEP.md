# Ruhemodus für Festplatten

Speicher › Weitere Optionen › Ruhemodus für Festplatten, nur Besitzer und Administratoren. Paket `storage` (`sleep.go`), Routen `GET`/`PUT /api/v1/storage/disk-sleep`.

## Einstellung

- **Standard:** eine Leerlaufzeit für alle unterstützten HDDs: Aus (ab Werk), 5, 10, 15 oder 30 Minuten, 1, 2 oder 3 Stunden.
- **Je Volume** (Entscheidung des Projektinhabers vom 29.10.2026): „Standard übernehmen“, „Aus“ (nie schlafen) oder eine der Zeiten. Alle Mitglieder des Volumes (mdadm, Btrfs, auch single) bekommen diesen Wert.
- **Je Platte:** dieselbe Auswahl für jede einzelne HDD, auch für Platten in keinem Volume (Ersatzplatte, USB-HDD).
- **Vorrang:** Platte vor Volume vor Standard. Die Seite zeigt für jedes Volume und jede Platte den wirksamen Wert und seine Herkunft („eigene“, „vom Volume“, „Standard“).

Eine Platte ohne eindeutige Seriennummer und ohne WWN (oder mit derselben Seriennummer wie eine andere Platte, wie es manche USB-Gehäuse melden) bekommt keinen eigenen Wert; sie übernimmt den ihres Volumes bzw. den Standard. So gilt ein Wert nie unbemerkt für eine andere Platte.

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

- `GET /api/v1/storage/disk-sleep`: Einstellung (`idleMinutes`, `volumes`, `disks`), `available`, `errors`, dazu `volumeStatus` und `diskStatus` (Name, Modell, Seriennummer, Größe, Volume, eigener Wert oder `null`, wirksamer Wert, Herkunft, Zustand `asleep`/`active` aus dem RAM, letzter Standby-Fehler) sowie `missingVolumes`/`missingDisks`. Die Platten kommen aus `lsblk` wie auf der Speicherseite; keine Platte wird gelesen oder geweckt. Dieselben Felder stehen in `diskSleep` von `GET /api/v1/storage`.
- `PUT /api/v1/storage/disk-sleep`: `idleMinutes` ist Pflicht. `volumes` und `disks` ersetzen, wenn angegeben, alle Ausnahmen dieser Art; fehlen sie, bleiben die gespeicherten (ältere Oberflächen). Werte wie beim Standard, sonst `invalid_disk_idle_time`. Ein neuer oder geänderter Eintrag für ein unbekanntes Volume oder eine nicht vorhandene bzw. nicht unterstützte Platte (SSD, NVMe …) sowie ein Schlüssel ohne `serial:`/`wwn:` ergibt `unknown_disk_sleep_target` (400); ein unverändert gespeicherter Eintrag einer gerade fehlenden Platte bleibt erlaubt.

## Verhalten

- `storage.RunDiskSleep` liest alle zehn Sekunden `/sys/block/*/stat`; jede Platte hat ihre eigene Zeitmessung mit ihrem wirksamen Wert. Welche Platte zu welchem Volume gehört, ergibt sich aus den UUIDs der Mitgliedspartitionen im `lsblk`-Baum (Array-UUID, Btrfs-UUID, LUKS-UUIDs), nicht aus `/config`. Platten mit dem wirksamen Wert „Aus“ werden nicht beobachtet; sind alle Werte aus, ruht die Zeitmessung ganz.
- Jede Änderung der Einstellung beginnt eine volle neue Ruhezeit für alle Platten. „Aus“ schickt keinen Befehl und weckt keine schlafende Platte.
- **RAID:** Ein Volume schläft erst wirklich, wenn alle seine Platten ruhen. Ein Zugriff auf ein RAID weckt die schlafenden Mitglieder ohnehin mit (mdadm und Btrfs lesen und schreiben verteilt); unterschiedliche Werte innerhalb eines Volumes sparen daher wenig. Eine Ausnahme je Platte ist vor allem für Platten außerhalb von Volumes gedacht.
- Ausnahmen wie bisher: SSDs, NVMe, Bootstick, Virtio-/Netzplatten, an VMs vergebene Platten, Platten mit laufendem Test und Mitglieder von Arrays bzw. Btrfs-Volumes in Prüfung oder Wiederaufbau. Standby einzeln über `hd-idle` (ATA bei SATA, SCSI bei SAS mit Power Condition 3, USB); `hd-idle.service` bleibt maskiert.

## Prüfstand

Go-Tests (`backend/internal/storage/sleep_test.go`, `sleep_override_test.go`, `backend/internal/api/storage_sleep_test.go`): Auflösung und Vorrang, Volumes mit „Aus“, Platten außerhalb von Volumes, Umbenennung `sdb` → `sdc`, doppelte Seriennummern, alte Datei hin und zurück, Prüfung unbekannter Volumes/Platten und Gerätenamen, Entfernen erst beim Speichern, Diagnosepaket ohne Seriennummern; Webtest `web/tests/disk-sleep.test.mjs`. Einschlafen und Aufwachen mit echten Platten, RAID-Mitgliedern und USB-Gehäusen sind noch nicht auf Hardware geprüft.
