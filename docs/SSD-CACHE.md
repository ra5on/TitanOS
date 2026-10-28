# SSD-Cache (ab 0.29.0)

Eine freie SSD kann ein mdadm-Volume (ext4/XFS) beschleunigen. Der Cache ist
**ab Werk aus** und wird je Volume auf der Speicher-Seite eingeschaltet
(„SSD-Cache › Einschalten …“). Btrfs-Volumes haben keinen SSD-Cache.

## Modi

| Modus | dm-cache | Was passiert, wenn die SSD ausfällt |
|---|---|---|
| **Lesecache** (Standard) | `writethrough` | nichts geht verloren; ohne SSD wird das Volume direkt eingebunden, es gibt eine Meldung |
| **Lese- und Schreibcache** | `writeback` | noch nicht zurückgeschriebene Daten gehen verloren, das Dateisystem kann beschädigt werden; ohne SSD bleibt das Volume **ausgehängt** |

Der Schreibcache ist eine bewusste Wahl mit Warnung im Dialog; empfohlen nur
mit zuverlässiger SSD und USV. Der Modus lässt sich im laufenden Betrieb
wechseln (`dmsetup reload` mit kurzem `suspend`/`resume`).

## Technik

- Die SSD wird vollständig gelöscht und bekommt eine GPT mit zwei
  Partitionen: Metadaten (mindestens 64 MiB, 32 Byte je Cache-Block) und
  Cache-Blöcke (256 KiB). `volumes.json` speichert im optionalen Feld `cache`
  Modus, Seriennummer und die PARTUUIDs; ältere Versionen ignorieren es und
  binden das Array direkt ein (bei Lese- und Schreibcache mit dem Risiko
  veralteter Daten: vor einem Rückweg auf 0.28.x den Cache ausschalten).
- Beim Einbinden legt titand `/dev/mapper/titanos-cache-<volume>` an (Ziel
  `cache` über `/dev/mdX`, Policy `smq`) und bindet dieses Gerät statt
  `UUID=` ein. Das Einschalten wird deshalb mit dem **nächsten Start**
  wirksam; Array und Dateisystem bleiben unverändert.
- Die Cache-SSD zählt als Mitglied des Volumes und wird nirgends sonst
  angeboten. Erweitern (RAID5/6) ist mit Cache gesperrt
  (`volume_cache_configured`).
- Ausschalten: Ein Lesecache wird sofort vergessen (er läuft bis zum nächsten
  Start harmlos weiter). Ein Schreibcache wird zuerst mit der Policy `cleaner`
  vollständig zurückgeschrieben (`removing`); sobald `dmsetup status` keine
  „dirty“ Blöcke mehr zeigt, wird er vergessen (Meldung
  `volume_cache_removed`).
- Die Speicher-Seite zeigt Belegung, Lesetreffer und offene Blöcke aus
  `dmsetup status`.
- `dmsetup` und das Kernelmodul `dm-cache` sind im Abbild; es ist kein neues
  Paket nötig.

Mit einem verschlüsselten Volume liegt der Cache **unter** der
Verschlüsselung (`md → titanos-cache-… → titanos-crypt-… → Dateisystem`): Die
SSD hält nur verschlüsselte Blöcke. Der Cache wird beim Entsperren eingerichtet
und wird damit erst nach dem nächsten Entsperren wirksam; siehe
[ENCRYPTION.md](ENCRYPTION.md).

Eine Sicherung des Sticks, die bei einem Stickwechsel von einem Volume mit
Schreibcache gelesen wird, kommt direkt vom Array und kann älter sein als der
letzte Stand auf der SSD.

Geprüft mit Unit-Tests (Vorbereitung der SSD, erzeugte Tabelle, Einbinden mit
und ohne SSD, Schreibcache ohne SSD, Zurückschreiben vor dem Entfernen,
Auswertung von `dmsetup status`). Echte SSDs und echtes dm-cache sind noch nicht
geprüft.
